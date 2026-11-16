// Package envfile inspects, compares and safely rewrites .env files. Values
// are redacted by default and files are only written after a backup.
package envfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/redact"
)

// Entry is one variable definition.
type Entry struct {
	Key       string `json:"key"`
	Value     string `json:"-"` // never serialised; use Display
	Display   string `json:"value"`
	Line      int    `json:"line"`
	Sensitive bool   `json:"sensitive"`
	Empty     bool   `json:"empty"`
}

// File is a parsed env file.
type File struct {
	Path       string        `json:"path"`
	Entries    []Entry       `json:"entries"`
	Duplicates []string      `json:"duplicates"` // keys defined more than once
	Invalid    []InvalidLine `json:"invalid"`
	Secrets    []SecretIssue `json:"secrets"`
}

// InvalidLine is a line that is neither blank, comment nor KEY=VALUE.
type InvalidLine struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// SecretIssue flags a suspicious exposed credential (value never included).
type SecretIssue struct {
	Key    string `json:"key"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

var knownNames = []string{".env", ".env.local", ".env.development", ".env.development.local", ".env.production", ".env.test", ".env.staging", ".env.example", ".env.sample"}

// Discover lists env files present in dir (non-recursive).
func Discover(dir string) []string {
	var out []string
	for _, n := range knownNames {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

var (
	placeholderRe = regexp.MustCompile(`(?i)^(changeme|change_me|your[-_ ].*|xxx+|<.*>|\*+|todo|example|secret|password|null|none)$`)
	bigSecretRe   = regexp.MustCompile(`^(AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|sk-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})$`)
)

// Parse reads and analyses an env file. Parsing is lenient but reports
// problems instead of hiding them.
func Parse(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseReader(path, f)
}

// ParseReader parses env data from r.
func ParseReader(path string, r io.Reader) (*File, error) {
	out := &File{Path: path}
	seen := map[string]int{}
	dupSet := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		raw := strings.TrimSuffix(sc.Text(), "\r")
		if n == 1 {
			raw = strings.TrimPrefix(raw, "\xEF\xBB\xBF")
		}
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok {
			out.Invalid = append(out.Invalid, InvalidLine{n, "missing '='"})
			continue
		}
		if !config.ValidEnvKey(k) {
			out.Invalid = append(out.Invalid, InvalidLine{n, fmt.Sprintf("invalid variable name %q", k)})
			continue
		}
		v = unquote(strings.TrimSpace(v))
		e := Entry{Key: k, Value: v, Line: n, Sensitive: redact.IsSensitiveKey(k), Empty: v == ""}
		e.Display = redact.Value(k, v)
		if prev, dup := seen[k]; dup {
			dupSet[k] = true
			_ = prev
		}
		seen[k] = n
		if e.Sensitive && v != "" && !placeholderRe.MatchString(v) && !strings.HasSuffix(path, ".example") && !strings.HasSuffix(path, ".sample") {
			out.Secrets = append(out.Secrets, SecretIssue{k, n, "sensitive-looking variable holds a real-looking value; make sure this file is git-ignored"})
		} else if bigSecretRe.MatchString(v) {
			out.Secrets = append(out.Secrets, SecretIssue{k, n, "value matches a known credential format"})
		}
		out.Entries = append(out.Entries, e)
	}
	for k := range dupSet {
		out.Duplicates = append(out.Duplicates, k)
	}
	sort.Strings(out.Duplicates)
	return out, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 { // inline comment
		return strings.TrimSpace(v[:i])
	}
	return v
}

// Keys returns the set of defined keys.
func (f *File) Keys() map[string]bool {
	m := map[string]bool{}
	for _, e := range f.Entries {
		m[e.Key] = true
	}
	return m
}

func (f *File) value(k string) (string, bool) {
	v, ok := "", false
	for _, e := range f.Entries { // last definition wins
		if e.Key == k {
			v, ok = e.Value, true
		}
	}
	return v, ok
}

// Diff compares two files. Differing values are reported by key only (values
// are included as redacted display strings).
type Diff struct {
	OnlyInA []string   `json:"onlyInA"`
	OnlyInB []string   `json:"onlyInB"`
	Changed []DiffItem `json:"changed"`
}

// DiffItem is a key whose value differs.
type DiffItem struct {
	Key string `json:"key"`
	A   string `json:"a"`
	B   string `json:"b"`
}

// Compare diffs a against b.
func Compare(a, b *File) Diff {
	var d Diff
	ka, kb := a.Keys(), b.Keys()
	for k := range ka {
		if !kb[k] {
			d.OnlyInA = append(d.OnlyInA, k)
			continue
		}
		va, _ := a.value(k)
		vb, _ := b.value(k)
		if va != vb {
			d.Changed = append(d.Changed, DiffItem{k, redact.Value(k, va), redact.Value(k, vb)})
		}
	}
	for k := range kb {
		if !ka[k] {
			d.OnlyInB = append(d.OnlyInB, k)
		}
	}
	sort.Strings(d.OnlyInA)
	sort.Strings(d.OnlyInB)
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].Key < d.Changed[j].Key })
	return d
}

// Missing returns keys required by example but absent from actual.
func Missing(example, actual *File) []string {
	have := actual.Keys()
	var out []string
	seen := map[string]bool{}
	for _, e := range example.Entries {
		if !have[e.Key] && !seen[e.Key] {
			out = append(out, e.Key)
			seen[e.Key] = true
		}
	}
	return out
}

// GenerateExample builds .env.example content from a real env file: keys are
// preserved in order, sensitive values are blanked, other values kept only if
// keepSafeValues. Comments directly above a key are preserved.
func GenerateExample(path string, keepSafeValues bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	b.WriteString("# Generated by RUNTIDE from " + filepath.Base(path) + ". Fill in real values locally; never commit secrets.\n")
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := strings.TrimSuffix(sc.Text(), "\r")
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\xEF\xBB\xBF"))
		if line == "" || strings.HasPrefix(line, "#") {
			b.WriteString(line + "\n")
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !config.ValidEnvKey(k) || seen[k] {
			continue
		}
		seen[k] = true
		v = unquote(strings.TrimSpace(v))
		if keepSafeValues && !redact.IsSensitiveKey(k) && !bigSecretRe.MatchString(v) {
			b.WriteString(k + "=" + v + "\n")
		} else {
			b.WriteString(k + "=\n")
		}
	}
	return b.String(), sc.Err()
}

// WriteWithBackup writes content to path. If the file exists it is first
// copied to <name>.runtide-backup-<timestamp> next to it. The caller must have
// obtained user approval. It returns the backup path ("" when none).
func WriteWithBackup(path, content string) (string, error) {
	var backup string
	if old, err := os.ReadFile(path); err == nil {
		backup = fmt.Sprintf("%s.runtide-backup-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", fmt.Errorf("could not create backup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp := path + ".runtide-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return backup, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return backup, err
	}
	return backup, nil
}

// Report is a full inspection of a directory.
type Report struct {
	Dir     string   `json:"dir"`
	Files   []*File  `json:"files"`
	Missing []Missed `json:"missing"`
	Hints   []string `json:"hints"`
}

// Missed lists variables an example requires but a real file lacks.
type Missed struct {
	File string   `json:"file"`
	Keys []string `json:"keys"`
}

// Inspect analyses every env file in dir and cross-checks against the example.
func Inspect(dir string) (*Report, error) {
	r := &Report{Dir: dir}
	var example *File
	for _, p := range Discover(dir) {
		f, err := Parse(p)
		if err != nil {
			return nil, err
		}
		r.Files = append(r.Files, f)
		if strings.HasSuffix(p, ".env.example") || strings.HasSuffix(p, ".env.sample") {
			if example == nil {
				example = f
			}
		}
	}
	if example != nil {
		for _, f := range r.Files {
			if f == example || strings.HasSuffix(f.Path, ".example") || strings.HasSuffix(f.Path, ".sample") {
				continue
			}
			if m := Missing(example, f); len(m) > 0 {
				r.Missing = append(r.Missing, Missed{File: f.Path, Keys: m})
			}
		}
	} else if len(r.Files) > 0 {
		r.Hints = append(r.Hints, "No .env.example found; generate one so teammates know which variables are required.")
	}
	return r, nil
}
