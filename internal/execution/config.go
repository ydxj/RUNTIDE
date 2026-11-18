// Package execution is the heart of RUNTIDE: it prepares task workspaces,
// runs recorded commands and services inside an isolation level, verifies the
// result, tracks file changes and finalises an execution with evidence.
package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/runtide/runtide/internal/capsule"
	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/verification"
	"gopkg.in/yaml.v3"
)

// ConfigVersion is the workspace configuration schema version.
const ConfigVersion = 1

// CommandSpec is a one-shot command of a workspace (setup or start phase).
type CommandSpec struct {
	Name         string            `yaml:"name" json:"name"`
	Command      string            `yaml:"command" json:"command"`
	Args         []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Dir          string            `yaml:"dir,omitempty" json:"dir,omitempty"` // relative to the workspace directory
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	TimeoutSec   int               `yaml:"timeout_sec,omitempty" json:"timeoutSec,omitempty"`
	AllowFailure bool              `yaml:"allow_failure,omitempty" json:"allowFailure,omitempty"`
}

// CleanupSpec controls what happens when an execution ends.
type CleanupSpec struct {
	StopServices   *bool `yaml:"stop_services,omitempty" json:"stopServices,omitempty"` // default true
	RemoveWorktree bool  `yaml:"remove_worktree,omitempty" json:"removeWorktree,omitempty"`
}

// Policy restricts what may be executed in the workspace.
type Policy struct {
	AllowAdHoc bool     `yaml:"allow_adhoc,omitempty" json:"allowAdhoc,omitempty"` // arbitrary commands through `run exec` / agents
	Allow      []string `yaml:"allow,omitempty" json:"allow,omitempty"`            // executable name globs; empty = any (subject to deny)
	Deny       []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// Config is a task workspace's definition.
type Config struct {
	Version    int                    `yaml:"version" json:"version"`
	Name       string                 `yaml:"name,omitempty" json:"name,omitempty"`
	Task       string                 `yaml:"task,omitempty" json:"task,omitempty"`
	Isolation  string                 `yaml:"isolation,omitempty" json:"isolation,omitempty"`
	InheritEnv bool                   `yaml:"inherit_env,omitempty" json:"inheritEnv,omitempty"` // pass the whole parent environment (default: safe allow-list)
	Env        map[string]string      `yaml:"env,omitempty" json:"env,omitempty"`
	Setup      []CommandSpec          `yaml:"setup,omitempty" json:"setup,omitempty"`
	Start      []CommandSpec          `yaml:"start,omitempty" json:"start,omitempty"`
	Services   []config.ServiceSpec   `yaml:"services,omitempty" json:"services,omitempty"`
	Verify     *verification.Pipeline `yaml:"verify,omitempty" json:"verify,omitempty"`
	Cleanup    CleanupSpec            `yaml:"cleanup,omitempty" json:"cleanup,omitempty"`
	Exclude    []string               `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Docker     *capsule.DockerConfig  `yaml:"docker,omitempty" json:"docker,omitempty"`
	WSL        *capsule.WSLConfig     `yaml:"wsl,omitempty" json:"wsl,omitempty"`
	Policy     Policy                 `yaml:"policy,omitempty" json:"policy,omitempty"`
}

// ParseConfig reads configuration strictly (unknown fields are errors).
func ParseConfig(data string) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid workspace configuration: %w", err)
	}
	if c.Version == 0 {
		c.Version = ConfigVersion
	}
	return &c, nil
}

// StopServices reports whether services are stopped at the end (default true).
func (c *Config) StopServices() bool { return c.Cleanup.StopServices == nil || *c.Cleanup.StopServices }

// IsolationLevel resolves the configured isolation.
func (c *Config) IsolationLevel() (domain.Isolation, error) {
	return domain.ParseIsolation(c.Isolation)
}

// Validate checks the configuration before anything runs. wsDir is the
// workspace directory (used for container mount checks).
func (c *Config) Validate(wsDir string) error {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf(f, a...)) }
	if c.Version != ConfigVersion {
		add("unsupported configuration version %d (this RUNTIDE supports %d)", c.Version, ConfigVersion)
	}
	iso, err := c.IsolationLevel()
	if err != nil {
		add("%v", err)
	}
	for k := range c.Env {
		if !config.ValidEnvKey(k) {
			add("invalid environment variable name %q", k)
		}
	}
	checkCmds := func(phase string, cmds []CommandSpec) {
		seen := map[string]bool{}
		for i, cs := range cmds {
			if !config.ValidName(cs.Name) {
				add("%s[%d]: invalid name %q", phase, i, cs.Name)
			}
			if seen[cs.Name] {
				add("%s: duplicate name %q", phase, cs.Name)
			}
			seen[cs.Name] = true
			if strings.TrimSpace(cs.Command) == "" || strings.ContainsAny(cs.Command, "\r\n\x00") {
				add("%s %q: command is required and must not contain control characters", phase, cs.Name)
			}
			if cs.Dir != "" {
				if _, err := config.ResolveDir(".", cs.Dir); err != nil {
					add("%s %q: %v", phase, cs.Name, err)
				}
			}
			for k := range cs.Env {
				if !config.ValidEnvKey(k) {
					add("%s %q: invalid environment variable name %q", phase, cs.Name, k)
				}
			}
			if cs.TimeoutSec < 0 || cs.TimeoutSec > 24*3600 {
				add("%s %q: timeout_sec out of range", phase, cs.Name)
			}
			if err := c.checkPolicy(cs.Command, false); err != nil {
				add("%s %q: %v", phase, cs.Name, err)
			}
		}
	}
	checkCmds("setup", c.Setup)
	checkCmds("start", c.Start)
	names := map[string]bool{}
	for i := range c.Services {
		s := &c.Services[i]
		s.ApplyDefaults()
		if names[s.Name] {
			add("duplicate service %q", s.Name)
		}
		names[s.Name] = true
		for _, pr := range config.ValidateService(s, ".") {
			add("%s", pr)
		}
		if err := c.checkPolicy(s.Command, false); err != nil {
			add("service %q: %v", s.Name, err)
		}
	}
	for _, s := range c.Services {
		for _, d := range s.DependsOn {
			if !names[d] {
				add("service %q depends on unknown service %q", s.Name, d)
			}
		}
	}
	if len(p) == 0 && len(c.Services) > 0 {
		ws := &config.Workspace{Name: "x", Services: c.Services}
		if _, err := ws.StartOrder(); err != nil {
			add("%v", err)
		}
	}
	if c.Verify != nil {
		if err := c.Verify.Validate(); err != nil {
			add("verify: %v", err)
		}
	}
	for _, pat := range append(append([]string{}, c.Policy.Allow...), c.Policy.Deny...) {
		if _, err := path.Match(pat, "x"); err != nil {
			add("policy pattern %q is invalid: %v", pat, err)
		}
	}
	switch iso {
	case domain.IsolationDocker:
		if c.Docker == nil {
			add("isolation docker requires a docker section")
		} else if err := capsule.ValidateDocker(*c.Docker, wsDir); err != nil {
			add("%v", err)
		}
	case domain.IsolationWSL:
		if c.WSL == nil {
			add("isolation wsl requires a wsl section")
		} else if err := capsule.ValidateWSL(*c.WSL); err != nil {
			add("%v", err)
		}
	}
	if len(p) > 0 {
		return &config.ValidationError{Problems: p}
	}
	return nil
}

func (c *Config) checkPolicy(exe string, adhoc bool) error {
	base := strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	for _, d := range c.Policy.Deny {
		if ok, _ := path.Match(strings.ToLower(d), base); ok {
			return fmt.Errorf("policy denies executing %q", exe)
		}
	}
	if adhoc && !c.Policy.AllowAdHoc {
		return fmt.Errorf("policy does not allow ad-hoc commands in this workspace (set policy.allow_adhoc: true to permit them)")
	}
	if len(c.Policy.Allow) > 0 {
		for _, a := range c.Policy.Allow {
			if ok, _ := path.Match(strings.ToLower(a), base); ok {
				return nil
			}
		}
		return fmt.Errorf("policy does not allow executing %q", exe)
	}
	return nil
}

// Hash identifies the exact configuration + directory an approval applies to.
func (c *Config) Hash(dir string) (string, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(append(b, []byte("\x00"+strings.ToLower(dir))...))
	return hex.EncodeToString(h[:]), nil
}

// HasExecutables reports whether running the workspace would execute anything
// configured by the user (and therefore needs approval).
func (c *Config) HasExecutables() bool {
	return len(c.Setup)+len(c.Start)+len(c.Services) > 0 || (c.Verify != nil && len(c.Verify.Checks) > 0)
}

// Commands lists human-readable command lines for the approval prompt.
func (c *Config) Commands() []string {
	var out []string
	line := func(phase, name, cmd string, args []string) {
		out = append(out, fmt.Sprintf("%s %s: %s %s", phase, name, cmd, strings.Join(args, " ")))
	}
	for _, s := range c.Setup {
		line("setup", s.Name, s.Command, s.Args)
	}
	for _, s := range c.Start {
		line("start", s.Name, s.Command, s.Args)
	}
	for _, s := range c.Services {
		line("service", s.Name, s.Command, s.Args)
	}
	if c.Verify != nil {
		for _, k := range c.Verify.Checks {
			if k.Type == "command" || k.Command != "" {
				line("verify", k.Name, k.Command, k.Args)
			}
		}
	}
	return out
}

// DefaultConfigYAML is the starting configuration for a new workspace.
func DefaultConfigYAML(task string) string {
	return fmt.Sprintf(`version: 1
task: %q
isolation: managed        # managed | worktree | docker | wsl
# env:
#   NODE_ENV: test
# setup:
#   - {name: install, command: npm, args: [ci]}
# start:
#   - {name: build, command: npm, args: [run, build]}
# services:
#   - {name: api, command: npm, args: [start], healthcheck: {type: http, url: "http://127.0.0.1:3000/health"}}
# verify:
#   name: acceptance
#   checks:
#     - {name: unit-tests, command: npm, args: [test]}
policy:
  allow_adhoc: false
`, task)
}
