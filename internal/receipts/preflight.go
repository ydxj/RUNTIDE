package receipts

import (
	"fmt"
	"path"
	"strings"

	"github.com/runtide/runtide/internal/execution"
)

// Outcomes of a reproduction attempt.
const (
	OutcomeReproduced = "reproduced"           // same final state and verification verdict, and every known input was available
	OutcomePartial    = "partially_reproduced" // same verdict, but known inputs were missing or secondary evidence differs
	OutcomeCouldNot   = "could_not_reproduce"  // the replay did not run, or the result differs from the receipt
	OutcomeMetadata   = "metadata_only"        // the receipt holds evidence only: there was nothing to run
)

// Dependency is one thing a replay needs, and whether this machine has it.
type Dependency struct {
	Kind      string `json:"kind"` // tool | env | source | runtime
	Name      string `json:"name"`
	Required  string `json:"required"`
	Available string `json:"available"`
	Status    string `json:"status"` // ok | different | missing
}

var destructiveExecutables = map[string]string{
	"rm": "deletes files", "del": "deletes files", "erase": "deletes files", "rmdir": "deletes directories", "rd": "deletes directories",
	"remove-item": "deletes files", "rimraf": "deletes files", "format": "formats a volume", "shutdown": "shuts the machine down",
	"taskkill": "terminates processes", "kill": "terminates processes", "stop-process": "terminates processes", "diskpart": "changes disk layout",
	"reg": "changes the registry", "setx": "changes persistent environment variables", "netsh": "changes network configuration",
	"sc": "changes Windows services",
}

// destructiveReasons inspects one command line. It is a heuristic: it flags
// common destructive operations, it cannot prove a command is safe.
func destructiveReasons(exe string, args []string) []string {
	base := strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	var out []string
	if why, ok := destructiveExecutables[base]; ok {
		out = append(out, why)
	}
	joined := " " + strings.ToLower(strings.Join(args, " ")) + " "
	switch base {
	case "git":
		for _, p := range []string{" clean ", " reset --hard", " push ", " checkout -- ", " branch -d", " branch -D", " gc --prune"} {
			if strings.Contains(joined, strings.ToLower(p)) {
				out = append(out, "git"+strings.TrimRight(p, " ")+" changes or discards repository state")
				break
			}
		}
	case "docker":
		for _, p := range []string{" rm ", " rmi ", " prune", " system prune", " volume rm", " push "} {
			if strings.Contains(joined, p) {
				out = append(out, "docker"+strings.TrimRight(p, " ")+" removes or publishes container resources")
				break
			}
		}
	case "npm", "pnpm", "yarn":
		if strings.Contains(joined, " publish ") || strings.Contains(joined, " unpublish ") {
			out = append(out, "publishes or unpublishes a package")
		}
	case "powershell", "pwsh", "cmd", "bash", "sh", "wsl":
		out = append(out, "runs an arbitrary shell script whose effects RUNTIDE cannot inspect")
	}
	for _, p := range []string{" -rf ", " -fr ", " /s /q", " --force ", " drop table", " drop database"} {
		if strings.Contains(joined, p) {
			out = append(out, fmt.Sprintf("argument %q suggests forced or bulk deletion", strings.TrimSpace(p)))
			break
		}
	}
	return out
}

// preflight fills the dependency, destructive-operation and permission lists.
// It reads configuration only; nothing is executed.
func (p *ReplayPlan) preflight(cfg *execution.Config, r *Receipt, have map[string]string) {
	// dependencies: tools, withheld environment, source
	for _, t := range r.Environment.Tools {
		d := Dependency{Kind: "tool", Name: t.Name, Required: t.Version}
		if v, ok := have[t.ID]; !ok {
			d.Status = "missing"
		} else if v != t.Version {
			d.Status, d.Available = "different", v
		} else {
			d.Status, d.Available = "ok", v
		}
		p.Dependencies = append(p.Dependencies, d)
	}
	for _, k := range p.NeedsEnv {
		p.Dependencies = append(p.Dependencies, Dependency{Kind: "env", Name: k, Required: "a value (withheld from the receipt)", Status: "missing"})
	}
	src := Dependency{Kind: "source", Name: "git checkout", Required: "commit " + short10(p.Commit)}
	if p.Commit == "" {
		src.Required = "the project at HEAD (no commit recorded)"
	}
	src.Status = "ok"
	for _, b := range p.Blockers {
		if strings.Contains(b, "project directory") || strings.Contains(b, "not a git repository") || strings.Contains(b, "is not present in") {
			src.Status = "missing"
		}
	}
	p.Dependencies = append(p.Dependencies, src)
	if iso, err := cfg.IsolationLevel(); err == nil {
		switch iso.Name() {
		case "docker":
			p.Dependencies = append(p.Dependencies, Dependency{Kind: "runtime", Name: "Docker engine", Required: "running, image " + imageOf(cfg), Status: "unknown until started"})
		case "wsl":
			d := "default"
			if cfg.WSL != nil {
				d = cfg.WSL.Distro
			}
			p.Dependencies = append(p.Dependencies, Dependency{Kind: "runtime", Name: "WSL2 distribution", Required: d, Status: "unknown until started"})
		}
	}

	// potentially destructive operations (heuristic)
	check := func(phase, name, exe string, args []string) {
		for _, why := range destructiveReasons(exe, args) {
			p.Destructive = append(p.Destructive, fmt.Sprintf("%s %s: %s %s: %s", phase, name, exe, strings.Join(args, " "), why))
		}
	}
	for _, c := range cfg.Setup {
		check("setup", c.Name, c.Command, c.Args)
	}
	for _, c := range cfg.Start {
		check("start", c.Name, c.Command, c.Args)
	}
	for _, s := range cfg.Services {
		check("service", s.Name, s.Command, s.Args)
	}
	if cfg.Verify != nil {
		for _, c := range cfg.Verify.Checks {
			if c.Command != "" {
				check("verify", c.Name, c.Command, c.Args)
			}
		}
	}

	// permissions the replay needs
	n := len(cfg.Setup) + len(cfg.Start) + len(cfg.Services)
	if cfg.Verify != nil {
		n += len(cfg.Verify.Checks)
	}
	p.Permissions = append(p.Permissions,
		"create a new git worktree (detached HEAD) of your checkout; your branch and working tree are not modified",
		fmt.Sprintf("run %d configured step(s) as your Windows user, without elevation", n))
	if len(cfg.Services) > 0 {
		p.Permissions = append(p.Permissions, fmt.Sprintf("start %d long-running service(s) that may listen on local ports", len(cfg.Services)))
	}
	p.Permissions = append(p.Permissions, "network and file access are not restricted by managed or worktree isolation")
	if cfg.InheritEnv {
		p.Permissions = append(p.Permissions, "the original run inherited your whole environment; the replay uses a clean allow-listed environment instead")
	}
}

func imageOf(cfg *execution.Config) string {
	if cfg.Docker != nil {
		return cfg.Docker.Image
	}
	return "(none)"
}

func short10(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// classifyOutcome decides how well a replay reproduced the receipt. It only
// uses recorded evidence and never claims more than the comparison shows.
func classifyOutcome(plan *ReplayPlan, state string, cmp *Comparison, runErr error) (string, []string) {
	var why []string
	if cmp == nil {
		return OutcomeCouldNot, []string{"the replay's evidence could not be compared with the receipt"}
	}
	if runErr != nil {
		return OutcomeCouldNot, []string{"the replay did not finish cleanly: " + runErr.Error()}
	}
	if cmp.A.Status != cmp.B.Status {
		return OutcomeCouldNot, []string{fmt.Sprintf("final state differs: receipt %s, replay %s", cmp.A.Status, cmp.B.Status)}
	}
	if cmp.Verification.Overall[0] != cmp.Verification.Overall[1] {
		return OutcomeCouldNot, []string{fmt.Sprintf("verification verdict differs: receipt %s, replay %s", cmp.Verification.Overall[0], cmp.Verification.Overall[1])}
	}
	if len(cmp.Verification.NewFailures) > 0 || len(cmp.Verification.Resolved) > 0 || len(cmp.Verification.StatusChanges) > 0 {
		return OutcomeCouldNot, []string{"individual verification checks differ from the receipt"}
	}
	partial := false
	if plan != nil && plan.Class != ReproFull {
		partial = true
		why = append(why, "the receipt could only be partially reconstructed (class "+plan.Class+"): some inputs were unknown, so equal results do not show the run was identical")
	}
	if len(cmp.Commands.Changed) > 0 || len(cmp.Commands.OnlyInA) > 0 || len(cmp.Commands.OnlyInB) > 0 {
		partial = true
		why = append(why, "the executed commands or their exit codes differ from the receipt")
	}
	if len(cmp.Files.OnlyInA) > 0 || len(cmp.Files.OnlyInB) > 0 || len(cmp.Files.Changed) > 0 {
		partial = true
		why = append(why, "the recorded file changes differ from the receipt")
	}
	if partial {
		return OutcomePartial, why
	}
	return OutcomeReproduced, []string{"same final state and verification verdict, matching commands and file changes"}
}
