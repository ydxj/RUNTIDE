package receiptcore

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
)

// Comparison is a structured difference between two receipts. It never
// concludes that two executions are "equivalent": it reports what differs,
// what matches in the recorded evidence, and what could not be compared.
type Comparison struct {
	A            CompareMeta   `json:"a"`
	B            CompareMeta   `json:"b"`
	SameProject  bool          `json:"sameProject"`
	Durations    DurationDiff  `json:"durations"`
	Environment  EnvDiff       `json:"environment"`
	Commands     CommandsDiff  `json:"commands"`
	Verification VerifDiff     `json:"verification"`
	Files        FilesDiff     `json:"files"`
	Services     []ServiceDiff `json:"services"`
	Failures     FailureDiff   `json:"failureEvents"`
	Missing      []string      `json:"missingEvidence"`
	Notes        []string      `json:"notes"`
}

// CompareMeta identifies one side.
type CompareMeta struct {
	ExecutionID  string    `json:"executionId"`
	Project      string    `json:"project"`
	Task         string    `json:"task"`
	Status       string    `json:"status"`
	Verification string    `json:"verification"`
	CreatedAt    time.Time `json:"createdAt"`
	SHA256       string    `json:"sha256"`
}

// DurationDiff compares wall-clock durations.
type DurationDiff struct {
	AMs     int64 `json:"aMs"`
	BMs     int64 `json:"bMs"`
	DeltaMs int64 `json:"deltaMs"`
}

// EnvDiff compares recorded environments.
type EnvDiff struct {
	Changed []string   `json:"changed"` // e.g. "os: windows -> linux"
	Tools   []ToolDiff `json:"tools"`
}

// ToolDiff is a tool whose presence or version differs.
type ToolDiff struct {
	ID     string `json:"id"`
	A      string `json:"a"`
	B      string `json:"b"`
	Change string `json:"change"` // added | removed | changed
}

// CommandsDiff compares commands matched by (kind, name, executable, arguments).
type CommandsDiff struct {
	OnlyInA   []string        `json:"removed"`
	OnlyInB   []string        `json:"new"`
	Changed   []CommandChange `json:"changed"`
	Unchanged int             `json:"unchanged"`
}

// CommandChange describes a matched command whose outcome differs.
type CommandChange struct {
	Key           string `json:"command"`
	ExitA         string `json:"exitA"`
	ExitB         string `json:"exitB"`
	DurationAMs   int64  `json:"durationAMs"`
	DurationBMs   int64  `json:"durationBMs"`
	OutputChanged string `json:"output"` // changed | same | unavailable
}

// VerifDiff compares verification results by check name.
type VerifDiff struct {
	Overall       [2]string      `json:"overall"`
	NewFailures   []string       `json:"newFailures"`
	Resolved      []string       `json:"resolvedFailures"`
	StatusChanges []StatusChange `json:"statusChanges"`
	OnlyInA       []string       `json:"onlyInA"`
	OnlyInB       []string       `json:"onlyInB"`
}

// StatusChange is a check whose status changed.
type StatusChange struct {
	Name string `json:"name"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// FilesDiff compares file change manifests.
type FilesDiff struct {
	OnlyInA []string `json:"onlyInA"`
	OnlyInB []string `json:"onlyInB"`
	Changed []string `json:"changedContent"` // same path changed in both, different resulting hash
	Common  int      `json:"identical"`
}

// ServiceDiff is a service whose final recorded health differs.
type ServiceDiff struct {
	Name string `json:"name"`
	A    string `json:"a"`
	B    string `json:"b"`
}

// FailureDiff compares error-severity events.
type FailureDiff struct {
	New      []string `json:"new"`
	Resolved []string `json:"resolved"`
}

func cmdKey(c domain.Command) string {
	return fmt.Sprintf("%s:%s: %s %s", c.Kind, c.Name, c.Executable, strings.Join(c.Args, " "))
}

func exitStr(c domain.Command) string {
	switch {
	case c.Error != "":
		return "error"
	case c.TimedOut:
		return "timeout"
	case c.Cancelled:
		return "cancelled"
	case c.ExitCode == nil:
		return "unknown"
	}
	return fmt.Sprint(*c.ExitCode)
}

func durMs(c domain.Command) int64 {
	if c.EndedAt == nil {
		return -1
	}
	return c.EndedAt.Sub(c.StartedAt).Milliseconds()
}

func hashText(s string) string { h := sha256.Sum256([]byte(s)); return fmt.Sprintf("%x", h[:8]) }

// Compare diffs two receipts. pathA/pathB are the archive files (for reading logs).
func Compare(a, b *Receipt, pathA, pathB string) *Comparison {
	c := &Comparison{
		A: meta(a), B: meta(b),
		SameProject: a.Manifest.ProjectName == b.Manifest.ProjectName,
		Durations:   DurationDiff{AMs: a.Execution.DurationMs, BMs: b.Execution.DurationMs, DeltaMs: b.Execution.DurationMs - a.Execution.DurationMs},
	}
	if !c.SameProject {
		c.Notes = append(c.Notes, fmt.Sprintf("the receipts belong to different projects (%q vs %q); differences may be unrelated to the code under test", a.Manifest.ProjectName, b.Manifest.ProjectName))
	}
	c.Notes = append(c.Notes, "Matching evidence does not make two executions equivalent: only what each receipt recorded can be compared.")

	// environment
	ea, eb := a.Environment, b.Environment
	for _, d := range [][3]string{{"os", ea.OS, eb.OS}, {"arch", ea.Arch, eb.Arch}, {"runtide version", ea.RuntideVersion, eb.RuntideVersion}, {"isolation", ea.Isolation, eb.Isolation}} {
		if d[1] != d[2] {
			c.Environment.Changed = append(c.Environment.Changed, fmt.Sprintf("%s: %s -> %s", d[0], d[1], d[2]))
		}
	}
	if ea.Git.Head != eb.Git.Head {
		c.Environment.Changed = append(c.Environment.Changed, fmt.Sprintf("git commit: %.10s -> %.10s", ea.Git.Head, eb.Git.Head))
	}
	ta, tb := map[string]string{}, map[string]string{}
	for _, t := range ea.Tools {
		ta[t.ID] = t.Version
	}
	for _, t := range eb.Tools {
		tb[t.ID] = t.Version
	}
	for _, id := range union(keys(ta), keys(tb)) {
		va, oka := ta[id]
		vb, okb := tb[id]
		switch {
		case oka && !okb:
			c.Environment.Tools = append(c.Environment.Tools, ToolDiff{ID: id, A: va, Change: "removed"})
		case !oka && okb:
			c.Environment.Tools = append(c.Environment.Tools, ToolDiff{ID: id, B: vb, Change: "added"})
		case va != vb:
			c.Environment.Tools = append(c.Environment.Tools, ToolDiff{ID: id, A: va, B: vb, Change: "changed"})
		}
	}
	if len(ea.Tools) == 0 || len(eb.Tools) == 0 {
		c.Missing = append(c.Missing, "tool versions were not recorded in one or both receipts")
	}

	// commands
	ma, mb := map[string]domain.Command{}, map[string]domain.Command{}
	for _, x := range a.Commands {
		ma[cmdKey(x)] = x
	}
	for _, x := range b.Commands {
		mb[cmdKey(x)] = x
	}
	for _, k := range union(keys(ma), keys(mb)) {
		xa, oka := ma[k]
		xb, okb := mb[k]
		switch {
		case oka && !okb:
			c.Commands.OnlyInA = append(c.Commands.OnlyInA, k)
		case !oka && okb:
			c.Commands.OnlyInB = append(c.Commands.OnlyInB, k)
		default:
			oc := outputCompare(a, b, pathA, pathB, xa, xb)
			if exitStr(xa) != exitStr(xb) || oc != "same" {
				c.Commands.Changed = append(c.Commands.Changed, CommandChange{Key: k, ExitA: exitStr(xa), ExitB: exitStr(xb), DurationAMs: durMs(xa), DurationBMs: durMs(xb), OutputChanged: oc})
			} else {
				c.Commands.Unchanged++
			}
			if oc == "unavailable" {
				c.Missing = append(c.Missing, "output of "+k+" could not be compared (log missing or truncated)")
			}
		}
	}

	// verification
	c.Verification.Overall = [2]string{a.Verification.Summary.Overall, b.Verification.Summary.Overall}
	va, vb := map[string]string{}, map[string]string{}
	for _, r := range a.Verification.Results {
		va[r.Name] = r.Status
	}
	for _, r := range b.Verification.Results {
		vb[r.Name] = r.Status
	}
	bad := func(s string) bool { return s == domain.VFailed || s == domain.VTimedOut || s == domain.VSkipped }
	for _, n := range union(keys(va), keys(vb)) {
		sa, oka := va[n]
		sb, okb := vb[n]
		switch {
		case oka && !okb:
			c.Verification.OnlyInA = append(c.Verification.OnlyInA, n)
		case !oka && okb:
			c.Verification.OnlyInB = append(c.Verification.OnlyInB, n)
			if bad(sb) {
				c.Verification.NewFailures = append(c.Verification.NewFailures, n)
			}
		case sa != sb:
			c.Verification.StatusChanges = append(c.Verification.StatusChanges, StatusChange{Name: n, A: sa, B: sb})
			if bad(sb) && !bad(sa) {
				c.Verification.NewFailures = append(c.Verification.NewFailures, n)
			}
			if bad(sa) && !bad(sb) {
				c.Verification.Resolved = append(c.Verification.Resolved, n)
			}
		}
	}
	if len(a.Verification.Results) == 0 || len(b.Verification.Results) == 0 {
		c.Missing = append(c.Missing, "verification results are missing in one or both receipts")
	}

	// files
	fa, fb := map[string]domain.FileChange{}, map[string]domain.FileChange{}
	for _, f := range a.FileChanges {
		fa[f.Path] = f
	}
	for _, f := range b.FileChanges {
		fb[f.Path] = f
	}
	for _, p := range union(keys(fa), keys(fb)) {
		xa, oka := fa[p]
		xb, okb := fb[p]
		switch {
		case oka && !okb:
			c.Files.OnlyInA = append(c.Files.OnlyInA, xa.Change+" "+p)
		case !oka && okb:
			c.Files.OnlyInB = append(c.Files.OnlyInB, xb.Change+" "+p)
		case xa.HashAfter != xb.HashAfter || xa.Change != xb.Change:
			if xa.HashAfter == "" || xb.HashAfter == "" {
				c.Missing = append(c.Missing, "content hash of "+p+" is unavailable on one side")
			}
			c.Files.Changed = append(c.Files.Changed, p)
		default:
			c.Files.Common++
		}
	}

	// services
	sa, sb := map[string]string{}, map[string]string{}
	for _, s := range a.Services {
		sa[s.Name] = s.Health + "/" + s.LastState
	}
	for _, s := range b.Services {
		sb[s.Name] = s.Health + "/" + s.LastState
	}
	for _, n := range union(keys(sa), keys(sb)) {
		if sa[n] != sb[n] {
			c.Services = append(c.Services, ServiceDiff{Name: n, A: orNone(sa, n), B: orNone(sb, n)})
		}
	}

	// failure events
	fe := func(r *Receipt) map[string]bool {
		m := map[string]bool{}
		for _, e := range r.Events {
			if e.Severity == domain.SevError {
				msg := fmt.Sprint(firstOf(e.Data, "reason", "error", "detail", "message"))
				m[fmt.Sprintf("%s [%s] %s", e.Type, e.Subject, msg)] = true
			}
		}
		return m
	}
	ea2, eb2 := fe(a), fe(b)
	for k := range eb2 {
		if !ea2[k] {
			c.Failures.New = append(c.Failures.New, k)
		}
	}
	for k := range ea2 {
		if !eb2[k] {
			c.Failures.Resolved = append(c.Failures.Resolved, k)
		}
	}
	sort.Strings(c.Failures.New)
	sort.Strings(c.Failures.Resolved)
	c.Missing = dedupe(c.Missing)
	return c
}

func meta(r *Receipt) CompareMeta {
	return CompareMeta{ExecutionID: r.Manifest.ExecutionID, Project: r.Manifest.ProjectName, Task: r.Manifest.Task, Status: r.Manifest.Status, Verification: r.Manifest.Verification, CreatedAt: r.Manifest.CreatedAt, SHA256: r.SHA256}
}

func outputCompare(a, b *Receipt, pathA, pathB string, xa, xb domain.Command) string {
	if xa.Truncated || xb.Truncated || pathA == "" || pathB == "" {
		return "unavailable"
	}
	la, lb := a.LogFiles[xa.ID], b.LogFiles[xb.ID]
	if la == "" || lb == "" {
		return "unavailable"
	}
	ta, err1 := ReadLog(pathA, la)
	tb, err2 := ReadLog(pathB, lb)
	if err1 != nil || err2 != nil {
		return "unavailable"
	}
	if hashText(ta) == hashText(tb) {
		return "same"
	}
	return "changed"
}

func firstOf(m map[string]any, ks ...string) any {
	for _, k := range ks {
		if v, ok := m[k]; ok && v != nil && fmt.Sprint(v) != "" {
			return v
		}
	}
	return ""
}

func orNone(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return "(absent)"
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
