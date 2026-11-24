// Package config defines the workspace/service schema and validates it.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service types are informational labels that drive diagnostics (which runtime
// to look for) and icons; execution is always command + args without a shell.
var ServiceTypes = []string{"node", "nextjs", "php", "laravel", "python", "django", "flask", "java", "worker", "custom"}

// RestartSpec controls what happens when a service exits.
type RestartSpec struct {
	Policy       string `yaml:"policy,omitempty" json:"policy"`               // never | on-failure | always
	MaxRetries   int    `yaml:"max_retries,omitempty" json:"maxRetries"`      // consecutive restarts before giving up
	BackoffMs    int    `yaml:"backoff_ms,omitempty" json:"backoffMs"`        // initial delay
	MaxBackoffMs int    `yaml:"max_backoff_ms,omitempty" json:"maxBackoffMs"` // delay cap
}

// HealthSpec describes a health check.
type HealthSpec struct {
	Type             string   `yaml:"type" json:"type"` // http | tcp | process | command
	URL              string   `yaml:"url,omitempty" json:"url,omitempty"`
	ExpectStatus     int      `yaml:"expect_status,omitempty" json:"expectStatus,omitempty"` // 0 = any 2xx/3xx
	Host             string   `yaml:"host,omitempty" json:"host,omitempty"`
	Port             int      `yaml:"port,omitempty" json:"port,omitempty"`
	Command          string   `yaml:"command,omitempty" json:"command,omitempty"`
	Args             []string `yaml:"args,omitempty" json:"args,omitempty"`
	IntervalSec      int      `yaml:"interval_sec,omitempty" json:"intervalSec"`
	TimeoutSec       int      `yaml:"timeout_sec,omitempty" json:"timeoutSec"`
	StartPeriodSec   int      `yaml:"start_period_sec,omitempty" json:"startPeriodSec"` // grace before failures count
	FailureThreshold int      `yaml:"failure_threshold,omitempty" json:"failureThreshold"`
}

// ServiceSpec is a process RUNTIDE supervises.
type ServiceSpec struct {
	Name             string            `yaml:"name" json:"name"`
	Type             string            `yaml:"type,omitempty" json:"type"`
	Command          string            `yaml:"command" json:"command"`
	Args             []string          `yaml:"args,omitempty" json:"args"`
	WorkingDirectory string            `yaml:"working_directory,omitempty" json:"workingDirectory"`
	Env              map[string]string `yaml:"env,omitempty" json:"env"`
	EnvFile          string            `yaml:"env_file,omitempty" json:"envFile,omitempty"`
	CleanEnv         bool              `yaml:"clean_env,omitempty" json:"cleanEnv,omitempty"` // start with a filtered environment instead of the whole parent environment
	DependsOn        []string          `yaml:"depends_on,omitempty" json:"dependsOn"`
	Health           *HealthSpec       `yaml:"healthcheck,omitempty" json:"health,omitempty"`
	Restart          RestartSpec       `yaml:"restart,omitempty" json:"restart"`
	StopTimeoutSec   int               `yaml:"stop_timeout_sec,omitempty" json:"stopTimeoutSec"`
	StartTimeoutSec  int               `yaml:"start_timeout_sec,omitempty" json:"startTimeoutSec"`
	Autostart        bool              `yaml:"autostart,omitempty" json:"autostart"`
}

// Workspace groups services that belong to one project.
type Workspace struct {
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description,omitempty" json:"description"`
	Root        string        `yaml:"root,omitempty" json:"root"` // project root; defaults to the YAML file's directory
	Services    []ServiceSpec `yaml:"services" json:"services"`
}

var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ValidName reports whether s is an acceptable workspace or service name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ValidEnvKey reports whether s is a valid environment variable name.
func ValidEnvKey(s string) bool { return envKeyRe.MatchString(s) }

// Parse decodes workspace YAML strictly (unknown fields are errors).
func Parse(data []byte) (*Workspace, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w Workspace
	if err := dec.Decode(&w); err != nil {
		return nil, fmt.Errorf("invalid workspace YAML: %w", err)
	}
	return &w, nil
}

// Marshal encodes a workspace to YAML.
func Marshal(w *Workspace) ([]byte, error) { return yaml.Marshal(w) }

// ApplyDefaults fills zero values with documented defaults.
func (s *ServiceSpec) ApplyDefaults() {
	if s.Type == "" {
		s.Type = "custom"
	}
	if s.Restart.Policy == "" {
		s.Restart.Policy = "never"
	}
	if s.Restart.Policy != "never" {
		if s.Restart.MaxRetries == 0 {
			s.Restart.MaxRetries = 3
		}
		if s.Restart.BackoffMs == 0 {
			s.Restart.BackoffMs = 1000
		}
		if s.Restart.MaxBackoffMs == 0 {
			s.Restart.MaxBackoffMs = 30000
		}
	}
	if s.StopTimeoutSec == 0 {
		s.StopTimeoutSec = 10
	}
	if s.StartTimeoutSec == 0 {
		s.StartTimeoutSec = 60
	}
	if h := s.Health; h != nil {
		if h.IntervalSec == 0 {
			h.IntervalSec = 10
		}
		if h.TimeoutSec == 0 {
			h.TimeoutSec = 3
		}
		if h.FailureThreshold == 0 {
			h.FailureThreshold = 3
		}
		if h.Type == "tcp" && h.Host == "" {
			h.Host = "127.0.0.1"
		}
	}
}

// ValidationError aggregates problems so users can fix them in one pass.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "validation failed:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// ValidateService checks one service. root may be empty for standalone
// services, in which case relative working directories are rejected.
func ValidateService(s *ServiceSpec, root string) []string {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf("service %q: "+f, append([]any{s.Name}, a...)...)) }
	if !ValidName(s.Name) {
		add("invalid name (use letters, digits, '.', '_', '-'; max 63 chars)")
	}
	if s.Type != "" && !contains(ServiceTypes, s.Type) {
		add("unknown type %q (allowed: %s)", s.Type, strings.Join(ServiceTypes, ", "))
	}
	if strings.TrimSpace(s.Command) == "" {
		add("command is required")
	}
	if strings.ContainsAny(s.Command, "\r\n\x00") {
		add("command contains control characters")
	}
	for _, a := range s.Args {
		if strings.ContainsAny(a, "\x00") {
			add("argument contains NUL byte")
		}
	}
	for k := range s.Env {
		if !ValidEnvKey(k) {
			add("invalid environment variable name %q", k)
		}
	}
	if s.WorkingDirectory != "" {
		if _, err := ResolveDir(root, s.WorkingDirectory); err != nil {
			add("%v", err)
		}
	}
	if s.EnvFile != "" {
		if _, err := ResolveDir(root, s.EnvFile); err != nil {
			add("env_file: %v", err)
		}
	}
	switch s.Restart.Policy {
	case "", "never", "on-failure", "always":
	default:
		add("restart.policy must be never, on-failure or always")
	}
	if s.Restart.MaxRetries < 0 || s.Restart.MaxRetries > 100 {
		add("restart.max_retries must be between 0 and 100")
	}
	if s.StopTimeoutSec < 0 || s.StopTimeoutSec > 600 {
		add("stop_timeout_sec must be between 0 and 600")
	}
	if h := s.Health; h != nil {
		p = append(p, validateHealth(s.Name, h)...)
	}
	return p
}

func validateHealth(svc string, h *HealthSpec) []string {
	var p []string
	add := func(f string, a ...any) {
		p = append(p, fmt.Sprintf("service %q healthcheck: "+f, append([]any{svc}, a...)...))
	}
	switch h.Type {
	case "http":
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("url must be an absolute http(s) URL")
		} else if !IsLoopbackHost(u.Hostname()) {
			add("url host %q is not loopback; health checks only target this machine", u.Hostname())
		}
	case "tcp":
		if h.Port < 1 || h.Port > 65535 {
			add("port must be 1-65535")
		}
		if h.Host != "" && !IsLoopbackHost(h.Host) {
			add("host %q is not loopback; health checks only target this machine", h.Host)
		}
	case "process":
	case "command":
		if strings.TrimSpace(h.Command) == "" {
			add("command is required for type command")
		}
	default:
		add("type must be http, tcp, process or command")
	}
	if h.IntervalSec < 0 || (h.IntervalSec > 0 && h.IntervalSec < 1) || h.IntervalSec > 3600 {
		add("interval_sec must be 1-3600")
	}
	if h.TimeoutSec < 0 || h.TimeoutSec > 120 {
		add("timeout_sec must be 0-120")
	}
	return p
}

// IsLoopbackHost reports whether host is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ResolveDir resolves rel against root and guarantees the result stays inside
// root. Absolute paths and ".." escapes are rejected. When root is empty only
// an absolute path is accepted (standalone services).
func ResolveDir(root, rel string) (string, error) {
	if root == "" {
		if filepath.IsAbs(rel) {
			return filepath.Clean(rel), nil
		}
		return "", fmt.Errorf("path %q must be absolute for a standalone service", rel)
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.HasPrefix(rel, `\`) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("path %q must be relative to the workspace root", rel)
	}
	abs := filepath.Join(root, rel)
	r, err := filepath.Rel(root, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace root", rel)
	}
	return abs, nil
}

// Validate checks a whole workspace including dependencies.
func (w *Workspace) Validate() error {
	var p []string
	if !ValidName(w.Name) {
		p = append(p, "workspace name is invalid (letters, digits, '.', '_', '-'; max 63 chars)")
	}
	if len(w.Services) == 0 {
		p = append(p, "workspace defines no services")
	}
	if len(w.Services) > 100 {
		p = append(p, "workspace defines more than 100 services")
	}
	seen := map[string]bool{}
	for i := range w.Services {
		s := &w.Services[i]
		s.ApplyDefaults()
		if seen[s.Name] {
			p = append(p, fmt.Sprintf("duplicate service name %q", s.Name))
		}
		seen[s.Name] = true
		p = append(p, ValidateService(s, w.rootForValidation())...)
	}
	for _, s := range w.Services {
		for _, d := range s.DependsOn {
			if !seen[d] {
				p = append(p, fmt.Sprintf("service %q depends on unknown service %q", s.Name, d))
			}
			if d == s.Name {
				p = append(p, fmt.Sprintf("service %q depends on itself", s.Name))
			}
		}
	}
	if len(p) == 0 {
		if _, err := w.StartOrder(); err != nil {
			p = append(p, err.Error())
		}
	}
	if len(p) > 0 {
		return &ValidationError{Problems: p}
	}
	return nil
}

func (w *Workspace) rootForValidation() string {
	if w.Root == "" {
		return "." // symbolic root: only containment is checked here
	}
	return w.Root
}

// CycleError names the services forming a dependency cycle.
type CycleError struct{ Path []string }

func (e *CycleError) Error() string {
	return "dependency cycle detected: " + strings.Join(e.Path, " -> ")
}

// StartOrder returns services grouped in layers; every service in a layer
// depends only on earlier layers, so a layer may start in parallel.
func (w *Workspace) StartOrder() ([][]string, error) {
	deps := map[string][]string{}
	for _, s := range w.Services {
		deps[s.Name] = s.DependsOn
	}
	// Cycle detection with explicit path (DFS colouring).
	const white, grey, black = 0, 1, 2
	color := map[string]int{}
	var stack []string
	var cyc []string
	var visit func(n string) bool
	visit = func(n string) bool {
		color[n] = grey
		stack = append(stack, n)
		for _, d := range deps[n] {
			if _, ok := deps[d]; !ok {
				continue
			}
			if color[d] == grey {
				i := 0
				for ; i < len(stack) && stack[i] != d; i++ {
				}
				cyc = append(append([]string{}, stack[i:]...), d)
				return true
			}
			if color[d] == white && visit(d) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if color[n] == white && visit(n) {
			return nil, &CycleError{Path: cyc}
		}
	}
	// Kahn layering.
	indeg := map[string]int{}
	rev := map[string][]string{}
	for n, ds := range deps {
		for _, d := range ds {
			if _, ok := deps[d]; ok {
				indeg[n]++
				rev[d] = append(rev[d], n)
			}
		}
	}
	var layers [][]string
	remaining := len(deps)
	var cur []string
	for _, n := range names {
		if indeg[n] == 0 {
			cur = append(cur, n)
		}
	}
	for len(cur) > 0 {
		sort.Strings(cur)
		layers = append(layers, cur)
		remaining -= len(cur)
		var next []string
		for _, n := range cur {
			for _, m := range rev[n] {
				indeg[m]--
				if indeg[m] == 0 {
					next = append(next, m)
				}
			}
		}
		cur = next
	}
	if remaining != 0 {
		return nil, errors.New("dependency graph could not be ordered")
	}
	return layers, nil
}

// StopOrder is StartOrder reversed: dependents stop before their dependencies.
func (w *Workspace) StopOrder() ([][]string, error) {
	l, err := w.StartOrder()
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
	return l, nil
}

// Service returns the named service or nil.
func (w *Workspace) Service(name string) *ServiceSpec {
	for i := range w.Services {
		if w.Services[i].Name == name {
			return &w.Services[i]
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ResolveInside is ResolveDir plus containment of the *real* path: symbolic
// links and junctions inside the root may not lead outside it. A target that
// does not exist yet is checked through its nearest existing ancestor.
// Use it wherever RUNTIDE reads or writes a path that came from configuration
// or from a project's contents.
func ResolveInside(root, rel string) (string, error) {
	abs, err := ResolveDir(root, rel)
	if err != nil {
		return "", err
	}
	if root == "" {
		return abs, nil
	}
	realRoot, err := realPath(root)
	if err != nil {
		return "", fmt.Errorf("workspace root %q is not accessible: %w", root, err)
	}
	for p := abs; ; p = filepath.Dir(p) {
		if real, err := realPath(p); err == nil {
			r, rerr := filepath.Rel(realRoot, real)
			if rerr != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				return "", fmt.Errorf("path %q leads outside the workspace through a symbolic link or junction", rel)
			}
			return abs, nil
		}
		if next := filepath.Dir(p); next == p || len(p) <= len(root) {
			return abs, nil
		}
	}
}
