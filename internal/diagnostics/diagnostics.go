// Package diagnostics implements RUNTIDE Doctor: rule-based checks over real
// machine and workspace state. Rules never change anything; fixes are
// separate, opt-in, previewable operations.
package diagnostics

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/docker"
	"github.com/runtide/runtide/internal/envfile"
	"github.com/runtide/runtide/internal/health"
	"github.com/runtide/runtide/internal/ports"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/runtide/runtide/internal/wsl"
)

// Severity levels.
const (
	Error   = "error"
	Warning = "warning"
	Info    = "info"
)

// Finding is one diagnostic result.
type Finding struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Severity     string   `json:"severity"`
	Description  string   `json:"description"`
	Evidence     []string `json:"evidence"`
	Component    string   `json:"component"`
	Resolution   string   `json:"resolution"`
	FixAvailable bool     `json:"fixAvailable"`
	FixID        string   `json:"fixId,omitempty"`
}

// Target is a service plus the context needed to check it.
type Target struct {
	Workspace string
	Root      string // workspace root; empty for standalone services
	Spec      config.ServiceSpec
	Key       string
}

// Inputs is a snapshot of everything the rules inspect.
type Inputs struct {
	Targets   []Target
	Runtime   map[string]supervisor.Info // by key
	Listeners []ports.Listener
	Tools     []tools.Tool
	Docker    docker.Status
	WSL       wsl.Status
	WSLUsed   bool
	DataDir   string
	LastLogs  func(key string, n int) []string
	Roots     []string // workspace roots (also the only places fixes may write)
}

// Report is a complete run.
type Report struct {
	Time     time.Time `json:"time"`
	Findings []Finding `json:"findings"`
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Infos    int       `json:"infos"`
}

// Run evaluates every rule.
func Run(ctx context.Context, in Inputs) Report {
	fs := []Finding{}
	for _, rule := range []func(context.Context, Inputs) []Finding{
		ruleDefinitions, rulePaths, ruleExecutables, rulePorts, ruleEnv, ruleRuntimes,
		ruleDatabases, ruleServiceState, ruleHealth, rulePermissions, ruleDocker, ruleWSL,
	} {
		if ctx.Err() != nil {
			break
		}
		fs = append(fs, rule(ctx, in)...)
	}
	rank := map[string]int{Error: 0, Warning: 1, Info: 2}
	sort.SliceStable(fs, func(i, j int) bool { return rank[fs[i].Severity] < rank[fs[j].Severity] })
	r := Report{Time: time.Now(), Findings: fs}
	for _, f := range fs {
		switch f.Severity {
		case Error:
			r.Errors++
		case Warning:
			r.Warnings++
		default:
			r.Infos++
		}
	}
	return r
}

func label(t Target) string {
	if t.Workspace != "" {
		return t.Workspace + "/" + t.Spec.Name
	}
	return t.Spec.Name
}

func workdir(t Target) string {
	if t.Spec.WorkingDirectory == "" {
		return t.Root
	}
	d, err := config.ResolveDir(t.Root, t.Spec.WorkingDirectory)
	if err != nil {
		return ""
	}
	return d
}

func ruleDefinitions(_ context.Context, in Inputs) []Finding {
	var out []Finding
	for _, t := range in.Targets {
		spec := t.Spec
		spec.ApplyDefaults()
		if probs := config.ValidateService(&spec, t.Root); len(probs) > 0 {
			out = append(out, Finding{
				ID: "invalid-definition:" + t.Key, Title: "Invalid service definition", Severity: Error,
				Description: "The service definition does not pass validation and cannot be started.",
				Evidence:    probs, Component: label(t),
				Resolution: "Edit the service and correct the listed problems.",
			})
		}
	}
	return out
}

func rulePaths(_ context.Context, in Inputs) []Finding {
	var out []Finding
	seenRoot := map[string]bool{}
	for _, t := range in.Targets {
		if t.Root != "" && !seenRoot[t.Root] {
			seenRoot[t.Root] = true
			if st, err := os.Stat(t.Root); err != nil || !st.IsDir() {
				out = append(out, Finding{ID: "root-missing:" + t.Workspace, Title: "Workspace root does not exist", Severity: Error,
					Description: "The folder the workspace is anchored to is missing, so none of its services can start.",
					Evidence:    []string{t.Root}, Component: "workspace " + t.Workspace,
					Resolution: "Restore the folder or edit the workspace to point at the new location."})
				continue
			}
		}
		wd := workdir(t)
		if wd == "" {
			continue
		}
		if st, err := os.Stat(wd); err != nil {
			f := Finding{ID: "workdir-missing:" + t.Key, Title: "Working directory does not exist", Severity: Error,
				Description: fmt.Sprintf("Service %s is configured to run in a folder that is missing.", label(t)),
				Evidence:    []string{wd}, Component: label(t),
				Resolution: "Create the folder, or fix working_directory in the service definition."}
			if t.Root != "" && underAny(wd, in.Roots) {
				f.FixAvailable, f.FixID = true, "mkdir:"+wd
			}
			out = append(out, f)
		} else if !st.IsDir() {
			out = append(out, Finding{ID: "workdir-notdir:" + t.Key, Title: "Working directory is a file", Severity: Error,
				Description: "working_directory points at a file, not a folder.", Evidence: []string{wd}, Component: label(t),
				Resolution: "Point working_directory at the project folder."})
		}
	}
	return out
}

func lookup(t Target) (string, error) {
	cmd := t.Spec.Command
	if strings.ContainsAny(cmd, `\/`) && !filepath.IsAbs(cmd) {
		cmd = filepath.Join(workdir(t), cmd)
	}
	p, err := exec.LookPath(cmd)
	if err != nil && !isErrDot(err) {
		return "", err
	}
	return p, nil
}

func isErrDot(err error) bool {
	return err != nil && strings.Contains(err.Error(), "current directory")
}

func ruleExecutables(_ context.Context, in Inputs) []Finding {
	var out []Finding
	for _, t := range in.Targets {
		if t.Spec.Command == "" {
			continue
		}
		if _, err := lookup(t); err != nil {
			out = append(out, Finding{ID: "exe-missing:" + t.Key, Title: "Executable not found", Severity: Error,
				Description: fmt.Sprintf("%q could not be resolved from PATH or the working directory.", t.Spec.Command),
				Evidence:    []string{err.Error()}, Component: label(t),
				Resolution: "Install the tool, add it to PATH (RUNTIDE never edits PATH for you), or use the full path in command."})
		}
	}
	return out
}

func portsOf(t Target) []int {
	var ps []int
	if h := t.Spec.Health; h != nil {
		switch h.Type {
		case "http":
			if u, err := url.Parse(h.URL); err == nil {
				p := u.Port()
				if p == "" {
					if u.Scheme == "https" {
						p = "443"
					} else {
						p = "80"
					}
				}
				if n, err := strconv.Atoi(p); err == nil {
					ps = append(ps, n)
				}
			}
		case "tcp":
			ps = append(ps, h.Port)
		}
	}
	if v, ok := t.Spec.Env["PORT"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			ps = append(ps, n)
		}
	}
	return ps
}

func rulePorts(_ context.Context, in Inputs) []Finding {
	var out []Finding
	byPort := map[uint16][]ports.Listener{}
	for _, l := range in.Listeners {
		byPort[l.Port] = append(byPort[l.Port], l)
	}
	for _, t := range in.Targets {

		for _, p := range portsOf(t) {
			ls := byPort[uint16(p)]
			if len(ls) == 0 {
				continue
			}
			foreign := false
			var ev []string
			for _, l := range ls {
				if l.Managed == t.Key {
					continue // the service itself (or one of its child processes)
				}
				if l.Managed != "" {
					ev = append(ev, fmt.Sprintf("%s:%d held by %s (pid %d), managed service %s", l.Address, l.Port, l.ProcessName, l.PID, l.Managed))
				} else {
					ev = append(ev, fmt.Sprintf("%s:%d held by %s (pid %d)", l.Address, l.Port, l.ProcessName, l.PID))
				}
				foreign = true
			}
			if foreign {
				out = append(out, Finding{ID: fmt.Sprintf("port-conflict:%s:%d", t.Key, p), Title: fmt.Sprintf("Port %d is already in use", p), Severity: Error,
					Description: fmt.Sprintf("%s expects port %d but another process is listening on it.", label(t), p),
					Evidence:    ev, Component: label(t),
					Resolution: "Stop the owning process (Ports page) or change the port in the service configuration."})
			}
		}
	}
	return out
}

func ruleEnv(_ context.Context, in Inputs) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, t := range in.Targets {
		wd := workdir(t)
		if wd == "" || seen[wd] {
			continue
		}
		seen[wd] = true
		if _, err := os.Stat(wd); err != nil {
			continue
		}
		rep, err := envfile.Inspect(wd)
		if err != nil {
			continue
		}
		var example, real *envfile.File
		for _, f := range rep.Files {
			switch filepath.Base(f.Path) {
			case ".env.example", ".env.sample":
				example = f
			case ".env":
				real = f
			}
		}
		if example != nil && real == nil {
			out = append(out, Finding{ID: "env-missing:" + wd, Title: ".env file is missing", Severity: Warning,
				Description: "The project ships an example environment file but no .env exists, so required variables are unset.",
				Evidence:    []string{filepath.Join(wd, filepath.Base(example.Path))}, Component: label(t),
				Resolution:   "Copy the example to .env and fill in the values. RUNTIDE can create the copy for you (secrets are never invented).",
				FixAvailable: underAny(wd, in.Roots), FixID: "env-from-example:" + wd})
		}
		for _, m := range rep.Missing {
			out = append(out, Finding{ID: "env-vars-missing:" + m.File, Title: "Environment variables missing", Severity: Warning,
				Description: "Variables present in the example file are not defined in your env file.",
				Evidence:    m.Keys, Component: label(t),
				Resolution: "Add the listed variables to " + filepath.Base(m.File) + "."})
		}
		for _, f := range rep.Files {
			if len(f.Duplicates) > 0 {
				out = append(out, Finding{ID: "env-dupes:" + f.Path, Title: "Duplicate environment variables", Severity: Info,
					Description: "A variable is defined more than once; the last definition wins.", Evidence: f.Duplicates, Component: label(t),
					Resolution: "Remove the duplicate definitions."})
			}
		}
	}
	return out
}

var typeRuntime = map[string][]string{
	"node": {"node"}, "nextjs": {"node"}, "php": {"php"}, "laravel": {"php", "composer"},
	"python": {"python"}, "django": {"python"}, "flask": {"python"}, "java": {"java"},
}

func toolByID(ts []tools.Tool, id string) *tools.Tool {
	for i := range ts {
		if ts[i].ID == id {
			return &ts[i]
		}
	}
	return nil
}

var majorRe = regexp.MustCompile(`(\d+)`)

func ruleRuntimes(_ context.Context, in Inputs) []Finding {
	var out []Finding
	for _, t := range in.Targets {
		for _, id := range typeRuntime[t.Spec.Type] {
			tl := toolByID(in.Tools, id)
			if tl == nil || !tl.Installed {
				out = append(out, Finding{ID: "runtime-missing:" + t.Key + ":" + id, Title: tools.Name(id) + " is not installed", Severity: Error,
					Description: fmt.Sprintf("Service %s is a %s service and needs %s, which was not detected.", label(t), t.Spec.Type, tools.Name(id)),
					Evidence:    []string{"detection result: not installed"}, Component: label(t),
					Resolution: "Install " + tools.Name(id) + " and make sure it is on PATH."})
			}
		}
		if t.Spec.Type == "node" || t.Spec.Type == "nextjs" {
			wd := workdir(t)
			if wd == "" {
				continue
			}
			for _, f := range []string{".nvmrc", ".node-version"} {
				b, err := os.ReadFile(filepath.Join(wd, f))
				if err != nil {
					continue
				}
				want := majorRe.FindString(strings.TrimSpace(string(b)))
				tl := toolByID(in.Tools, "node")
				if want != "" && tl != nil && tl.Installed {
					have := majorRe.FindString(tl.Version)
					if have != want {
						out = append(out, Finding{ID: "node-version:" + t.Key, Title: "Node.js version differs from project", Severity: Warning,
							Description: fmt.Sprintf("%s requests Node %s but Node %s is first on PATH.", f, want, tl.Version),
							Evidence:    []string{filepath.Join(wd, f) + " = " + strings.TrimSpace(string(b)), tl.Path}, Component: label(t),
							Resolution: "Switch Node versions (nvm-windows, fnm, Volta) or update the project's version file."})
					}
				}
				break
			}
		}
	}
	return out
}

func ruleDatabases(ctx context.Context, in Inputs) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, t := range in.Targets {
		wd := workdir(t)
		if wd == "" {
			continue
		}
		for _, name := range []string{".env", ".env.local"} {
			f, err := envfile.Parse(filepath.Join(wd, name))
			if err != nil {
				continue
			}
			host, port := "", ""
			var urlv string
			for _, e := range f.Entries {
				switch strings.ToUpper(e.Key) {
				case "DB_HOST", "DATABASE_HOST", "MYSQL_HOST", "POSTGRES_HOST", "PGHOST":
					host = e.Value
				case "DB_PORT", "DATABASE_PORT", "MYSQL_PORT", "POSTGRES_PORT", "PGPORT":
					port = e.Value
				case "DATABASE_URL":
					urlv = e.Value
				}
			}
			if urlv != "" && host == "" {
				if u, err := url.Parse(urlv); err == nil && u.Host != "" {
					host, port = u.Hostname(), u.Port()
				}
			}
			if host == "" || !config.IsLoopbackHost(host) {
				continue // only local databases are probed; remote hosts are never contacted
			}
			if port == "" {
				port = "3306"
			}
			addr := net.JoinHostPort(host, port)
			if seen[addr] {
				continue
			}
			seen[addr] = true
			dctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			var d net.Dialer
			c, err := d.DialContext(dctx, "tcp", addr)
			cancel()
			if err != nil {
				out = append(out, Finding{ID: "db-unreachable:" + addr, Title: "Database is not reachable", Severity: Warning,
					Description: fmt.Sprintf("%s configures a local database at %s but nothing accepts connections there.", filepath.Join(wd, name), addr),
					Evidence:    []string{err.Error()}, Component: label(t),
					Resolution: "Start the database (or its Docker container / workspace service) and verify DB_HOST and DB_PORT."})
			} else {
				c.Close()
			}
		}
	}
	return out
}

func ruleServiceState(_ context.Context, in Inputs) []Finding {
	var out []Finding
	for _, t := range in.Targets {
		rt, ok := in.Runtime[t.Key]
		if !ok {
			continue
		}
		if rt.State == supervisor.StateFailed {
			ev := []string{rt.LastError}
			if rt.ExitCode != nil {
				ev = append(ev, fmt.Sprintf("exit code %d", *rt.ExitCode))
			}
			if in.LastLogs != nil {
				ev = append(ev, in.LastLogs(t.Key, 5)...)
			}
			out = append(out, Finding{ID: "service-failed:" + t.Key, Title: "Service failed", Severity: Error,
				Description: fmt.Sprintf("%s is in a failed state.", label(t)),
				Evidence:    ev, Component: label(t),
				Resolution: "Read the logs for the cause, fix it, then start the service again."})
		}
		if rt.TotalRestart >= 3 {
			out = append(out, Finding{ID: "restart-loop:" + t.Key, Title: "Service keeps restarting", Severity: Warning,
				Description: fmt.Sprintf("%s has been restarted %d times automatically.", label(t), rt.TotalRestart),
				Evidence:    []string{rt.LastError}, Component: label(t),
				Resolution: "Investigate the crash in the logs; consider lowering max_retries."})
		}
	}
	return out
}

func ruleHealth(_ context.Context, in Inputs) []Finding {
	var out []Finding
	for _, t := range in.Targets {
		h := t.Spec.Health
		if h == nil {
			continue
		}
		hs := *h
		if probs := validateHealth(t, hs); len(probs) > 0 {
			out = append(out, Finding{ID: "health-invalid:" + t.Key, Title: "Health check is misconfigured", Severity: Error,
				Description: "The health check definition is invalid and can never succeed.", Evidence: probs, Component: label(t),
				Resolution: "Fix the healthcheck block of the service."})
		}
		if rt, ok := in.Runtime[t.Key]; ok && rt.State == supervisor.StateRunning && rt.Health == health.Unhealthy {
			out = append(out, Finding{ID: "unhealthy:" + t.Key, Title: "Service is running but unhealthy", Severity: Warning,
				Description: "The process is alive but its health check fails.", Evidence: []string{rt.HealthDetail}, Component: label(t),
				Resolution: "Check that the service listens on the configured URL/port and review its logs."})
		}
	}
	return out
}

func validateHealth(t Target, h config.HealthSpec) []string {
	s := t.Spec
	s.Health = &h
	s.ApplyDefaults()
	var out []string
	for _, p := range config.ValidateService(&s, t.Root) {
		if strings.Contains(p, "healthcheck") {
			out = append(out, p)
		}
	}
	return out
}

func rulePermissions(_ context.Context, in Inputs) []Finding {
	var out []Finding
	if in.DataDir != "" {
		probe := filepath.Join(in.DataDir, ".write-test")
		if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
			out = append(out, Finding{ID: "datadir-readonly", Title: "RUNTIDE data folder is not writable", Severity: Error,
				Description: "Settings, logs and workspaces cannot be saved.", Evidence: []string{in.DataDir, err.Error()}, Component: "RUNTIDE",
				Resolution: "Fix the folder permissions or set RUNTIDE_HOME to a writable location."})
		} else {
			os.Remove(probe)
		}
	}
	for _, t := range in.Targets {
		if wd := workdir(t); wd != "" {
			if _, err := os.ReadDir(wd); err != nil && os.IsPermission(err) {
				out = append(out, Finding{ID: "perm-denied:" + t.Key, Title: "Working directory is not accessible", Severity: Error,
					Description: "Windows denied access to the service's folder.", Evidence: []string{wd, err.Error()}, Component: label(t),
					Resolution: "Adjust folder permissions; RUNTIDE does not run elevated."})
			}
		}
	}
	return out
}

func ruleDocker(_ context.Context, in Inputs) []Finding {
	d := in.Docker
	composeUsed := false
	for _, r := range in.Roots {
		if docker.ComposeFile(r) != "" {
			composeUsed = true
		}
	}
	switch {
	case !d.CLIInstalled && composeUsed:
		return []Finding{{ID: "docker-missing", Title: "Docker is required by a workspace but not installed", Severity: Warning,
			Description: "A workspace folder contains a Docker Compose file, but the Docker CLI is not available.",
			Evidence:    []string{d.Message}, Component: "Docker", Resolution: "Install Docker Desktop, or ignore if you do not use Compose."}}
	case !d.CLIInstalled:
		return []Finding{{ID: "docker-absent", Title: "Docker is not installed", Severity: Info,
			Description: "Docker is optional; container features are disabled.", Evidence: []string{d.Message}, Component: "Docker",
			Resolution: "Install Docker Desktop if you want container management."}}
	case !d.DaemonAvailable:
		sev := Info
		if composeUsed {
			sev = Warning
		}
		return []Finding{{ID: "docker-daemon", Title: "Docker daemon is not running", Severity: sev,
			Description: "The Docker CLI is installed but cannot reach the daemon.", Evidence: []string{d.Message}, Component: "Docker",
			Resolution: "Start Docker Desktop."}}
	}
	return nil
}

func ruleWSL(_ context.Context, in Inputs) []Finding {
	if !in.WSLUsed {
		return nil
	}
	if !in.WSL.Installed || len(in.WSL.Distros) == 0 {
		return []Finding{{ID: "wsl-missing", Title: "WSL is referenced but not available", Severity: Warning,
			Description: "A service command uses wsl.exe but WSL has no usable distribution.", Evidence: []string{in.WSL.Message}, Component: "WSL",
			Resolution: "Install WSL and a distribution (wsl --install) or change the service command."}}
	}
	return nil
}

func underAny(p string, roots []string) bool {
	p = strings.ToLower(filepath.Clean(p))
	for _, r := range roots {
		r = strings.ToLower(filepath.Clean(r))
		if p == r || strings.HasPrefix(p, r+`\`) {
			return true
		}
	}
	return false
}

// FixPlan describes what applying a fix will do, shown before consent.
type FixPlan struct {
	FixID       string   `json:"fixId"`
	Description string   `json:"description"`
	Changes     []string `json:"changes"`
	Backups     []string `json:"backups"`
}

// PlanFix previews a fix without changing anything.
func PlanFix(fixID string, roots []string) (*FixPlan, error) {
	op, arg, ok := strings.Cut(fixID, ":")
	if !ok || !underAny(arg, roots) {
		return nil, fmt.Errorf("fix %q is not allowed: target is outside every workspace root", fixID)
	}
	switch op {
	case "mkdir":
		return &FixPlan{FixID: fixID, Description: "Create the missing working directory.", Changes: []string{"create directory " + arg}}, nil
	case "env-from-example":
		src := exampleIn(arg)
		if src == "" {
			return nil, fmt.Errorf("no .env.example found in %s", arg)
		}
		dst := filepath.Join(arg, ".env")
		if _, err := os.Stat(dst); err == nil {
			return nil, fmt.Errorf("%s already exists; refusing to overwrite", dst)
		}
		return &FixPlan{FixID: fixID, Description: "Create .env from the example file (values are copied as-is; fill in real secrets afterwards).",
			Changes: []string{"create " + dst + " from " + src}}, nil
	}
	return nil, fmt.Errorf("unknown fix %q", fixID)
}

func exampleIn(dir string) string {
	for _, n := range []string{".env.example", ".env.sample"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return filepath.Join(dir, n)
		}
	}
	return ""
}

// ApplyFix executes a previously previewed fix. The caller is responsible for
// having obtained the user's consent.
func ApplyFix(fixID string, roots []string) (*FixPlan, error) {
	plan, err := PlanFix(fixID, roots)
	if err != nil {
		return nil, err
	}
	op, arg, _ := strings.Cut(fixID, ":")
	switch op {
	case "mkdir":
		if err := os.MkdirAll(arg, 0o755); err != nil {
			return plan, err
		}
	case "env-from-example":
		b, err := os.ReadFile(exampleIn(arg))
		if err != nil {
			return plan, err
		}
		f, err := os.OpenFile(filepath.Join(arg, ".env"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return plan, err
		}
		defer f.Close()
		if _, err := f.Write(b); err != nil {
			return plan, err
		}
	}
	return plan, nil
}
