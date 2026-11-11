// Package tools detects installed developer tools by locating real
// executables and running them to obtain a version. A directory merely
// existing is never treated as proof of installation.
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Install is one concrete executable of a tool.
type Install struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	OnPath  bool   `json:"onPath"`
	Method  string `json:"method"` // PATH | known-location
	Valid   bool   `json:"valid"`
	Note    string `json:"note,omitempty"`
}

// Tool is the detection result for one tool.
type Tool struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Installed bool      `json:"installed"`
	Version   string    `json:"version"`
	Path      string    `json:"path"`
	OnPath    bool      `json:"onPath"`
	Method    string    `json:"method"`
	Others    []Install `json:"others"` // additional versions found
	Note      string    `json:"note,omitempty"`
}

type def struct {
	id, name, category string
	exes               []string // executable base names without extension
	args               []string
	re                 *regexp.Regexp
	locations          []string // directories (may contain env vars) to try if not on PATH
}

var verRe = regexp.MustCompile(`(\d+(?:\.\d+){1,3}(?:[-+._a-zA-Z0-9]*)?)`)

func pf(sub string) []string {
	var out []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if b := os.Getenv(env); b != "" {
			out = append(out, filepath.Join(b, sub))
		}
	}
	return out
}

func defs() []def {
	return []def{
		{id: "node", name: "Node.js", category: "runtime", exes: []string{"node"}, args: []string{"--version"}, locations: pf("nodejs")},
		{id: "npm", name: "npm", category: "package-manager", exes: []string{"npm"}, args: []string{"--version"}, locations: pf("nodejs")},
		{id: "pnpm", name: "pnpm", category: "package-manager", exes: []string{"pnpm"}, args: []string{"--version"}, locations: append(pf("pnpm"), os.ExpandEnv(`$APPDATA\npm`))},
		{id: "yarn", name: "Yarn", category: "package-manager", exes: []string{"yarn"}, args: []string{"--version"}, locations: append(pf("Yarn\\bin"), os.ExpandEnv(`$APPDATA\npm`))},
		{id: "php", name: "PHP", category: "runtime", exes: []string{"php"}, args: []string{"--version"}, re: regexp.MustCompile(`PHP (\d+\.\d+\.\d+\S*)`), locations: []string{`C:\php`, `C:\xampp\php`, `C:\laragon\bin\php`, `C:\tools\php`}},
		{id: "composer", name: "Composer", category: "package-manager", exes: []string{"composer"}, args: []string{"--version"}, re: regexp.MustCompile(`Composer (?:version )?(\d+\.\d+\.\d+\S*)`), locations: []string{os.ExpandEnv(`$APPDATA\Composer\vendor\bin`), os.ExpandEnv(`$ProgramData\ComposerSetup\bin`)}},
		{id: "python", name: "Python", category: "runtime", exes: []string{"python", "python3", "py"}, args: []string{"--version"}, re: regexp.MustCompile(`Python (\d+\.\d+\.\d+\S*)`), locations: pf("Programs\\Python")},
		{id: "java", name: "Java", category: "runtime", exes: []string{"java"}, args: []string{"-version"}, re: regexp.MustCompile(`version "([^"]+)"`), locations: append(pf("Java"), pf("Eclipse Adoptium")...)},
		{id: "go", name: "Go", category: "runtime", exes: []string{"go"}, args: []string{"version"}, re: regexp.MustCompile(`go version go(\S+)`), locations: []string{`C:\Program Files\Go\bin`, `C:\Go\bin`}},
		{id: "git", name: "Git", category: "vcs", exes: []string{"git"}, args: []string{"--version"}, re: regexp.MustCompile(`git version (\S+)`), locations: pf("Git\\cmd")},
		{id: "docker", name: "Docker CLI", category: "containers", exes: []string{"docker"}, args: []string{"--version"}, re: regexp.MustCompile(`Docker version ([^,\s]+)`), locations: []string{`C:\Program Files\Docker\Docker\resources\bin`}},
		{id: "mysql", name: "MySQL client", category: "database", exes: []string{"mysql"}, args: []string{"--version"}, re: regexp.MustCompile(`Ver (\S+)`), locations: pf("MySQL\\MySQL Server 8.0\\bin")},
		{id: "psql", name: "PostgreSQL client", category: "database", exes: []string{"psql"}, args: []string{"--version"}, re: regexp.MustCompile(`psql \(PostgreSQL\) (\S+)`), locations: nil},
	}
}

func pathExts() []string {
	pe := os.Getenv("PATHEXT")
	if pe == "" {
		pe = ".COM;.EXE;.BAT;.CMD"
	}
	var out []string
	for _, e := range strings.Split(strings.ToLower(pe), ";") {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// findAll returns every matching executable in dirs (order preserved, deduped).
func findAll(dirs []string, names []string) []string {
	seen := map[string]bool{}
	var out []string
	exts := pathExts()
	for _, d := range dirs {
		d = strings.Trim(d, `"`)
		if d == "" {
			continue
		}
		for _, n := range names {
			for _, e := range exts {
				p := filepath.Join(d, n+e)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					key := strings.ToLower(p)
					if !seen[key] {
						seen[key] = true
						out = append(out, p)
					}
				}
			}
		}
	}
	return out
}

func runVersion(ctx context.Context, exe string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "COMPOSER_NO_INTERACTION=1")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func parseVersion(d def, out string) string {
	if d.re != nil {
		if m := d.re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	}
	if m := verRe.FindStringSubmatch(out); len(m) > 1 {
		return strings.TrimPrefix(m[1], "v")
	}
	return ""
}

// isStoreStub identifies the Microsoft Store "python.exe" app-execution alias,
// which exists on PATH but is not a Python installation.
func isStoreStub(p string) bool {
	return strings.Contains(strings.ToLower(p), `\microsoft\windowsapps\`)
}

func detectOne(ctx context.Context, d def) Tool {
	t := Tool{ID: d.id, Name: d.name, Category: d.category}
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	onPath := findAll(pathDirs, d.exes)
	onPathSet := map[string]bool{}
	for _, p := range onPath {
		onPathSet[strings.ToLower(p)] = true
	}
	var cands []Install
	for _, p := range onPath {
		cands = append(cands, Install{Path: p, OnPath: true, Method: "PATH"})
	}
	var locDirs []string
	for _, l := range d.locations {
		locDirs = append(locDirs, expandGlob(l)...)
	}
	for _, p := range findAll(locDirs, d.exes) {
		if !onPathSet[strings.ToLower(p)] {
			cands = append(cands, Install{Path: p, Method: "known-location"})
		}
	}
	for i := range cands {
		c := &cands[i]
		if isStoreStub(c.Path) {
			c.Note = "Microsoft Store alias, not a real installation"
			continue
		}
		args := d.args
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(c.Path), filepath.Ext(c.Path)), "py") {
			args = []string{"--version"}
		}
		out, err := runVersion(ctx, c.Path, args)
		v := parseVersion(d, out)
		if err != nil && v == "" {
			c.Note = "failed to run: " + firstLine(out, err.Error())
			continue
		}
		if v == "" {
			c.Note = "ran but printed no recognizable version"
			continue
		}
		c.Valid, c.Version = true, v
	}
	var valid []Install
	var rejected []string
	for _, c := range cands {
		if c.Valid {
			valid = append(valid, c)
		} else if c.Note != "" {
			rejected = append(rejected, c.Path+": "+c.Note)
		}
	}
	if len(valid) == 0 {
		if len(rejected) > 0 {
			t.Note = "Found but not usable: " + strings.Join(rejected, "; ")
		}
		return t
	}
	t.Installed = true
	t.Version, t.Path, t.OnPath, t.Method = valid[0].Version, valid[0].Path, valid[0].OnPath, valid[0].Method
	if !t.OnPath {
		t.Note = "Installed but not on PATH. RUNTIDE does not modify PATH automatically."
	}
	t.Others = valid[1:]
	return t
}

func firstLine(s, fallback string) string {
	if s == "" {
		return fallback
	}
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// expandGlob expands a directory pattern (e.g. Python3*) into real dirs.
func expandGlob(p string) []string {
	if !strings.ContainsAny(p, "*?[") {
		return []string{p}
	}
	m, _ := filepath.Glob(p)
	return m
}

// Detect runs detection for every known tool concurrently.
func Detect(ctx context.Context) []Tool {
	ds := defs()
	// Versioned install dirs are discovered with globs.
	for i := range ds {
		switch ds[i].id {
		case "python":
			ds[i].locations = append(ds[i].locations, filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Python", "Python3*"), `C:\Python3*`)
		case "java":
			ds[i].locations = append(expandAll(ds[i].locations, "*\\bin"), ds[i].locations...)
		case "mysql":
			ds[i].locations = append(ds[i].locations, filepath.Join(os.Getenv("ProgramFiles"), "MySQL", "*", "bin"))
		case "psql":
			ds[i].locations = append(ds[i].locations, filepath.Join(os.Getenv("ProgramFiles"), "PostgreSQL", "*", "bin"))
		}
	}
	out := make([]Tool, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func(i int, d def) {
			defer wg.Done()
			out[i] = detectOne(ctx, d)
		}(i, d)
	}
	wg.Wait()
	return out
}

func expandAll(bases []string, sub string) []string {
	var out []string
	for _, b := range bases {
		out = append(out, filepath.Join(b, sub))
	}
	return out
}

// Find returns the detection result for one tool id.
func Find(ctx context.Context, id string) (Tool, bool) {
	for _, t := range Detect(ctx) {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// Name returns the display name for a tool id.
func Name(id string) string {
	for _, d := range defs() {
		if d.id == id {
			return d.name
		}
	}
	return id
}
