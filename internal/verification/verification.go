// Package verification runs acceptance checks against a task's result. A task
// is only "verified" when every required check actually executed and passed:
// a failed prerequisite makes dependents *skipped*, and skipped required
// checks count against the overall result so nothing can look green by omission.
package verification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"gopkg.in/yaml.v3"
)

// Check is one acceptance criterion.
type Check struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string   `yaml:"type,omitempty" json:"type"` // command|http|tcp|file|services_healthy|no_running_services
	Required    *bool    `yaml:"required,omitempty" json:"required,omitempty"`
	Needs       []string `yaml:"needs,omitempty" json:"needs,omitempty"`
	TimeoutSec  int      `yaml:"timeout_sec,omitempty" json:"timeoutSec,omitempty"`
	WhenFile    string   `yaml:"when_file,omitempty" json:"whenFile,omitempty"` // not applicable unless this workspace file exists

	// command
	Command      string   `yaml:"command,omitempty" json:"command,omitempty"`
	Args         []string `yaml:"args,omitempty" json:"args,omitempty"`
	Dir          string   `yaml:"dir,omitempty" json:"dir,omitempty"`
	ExpectedExit *int     `yaml:"expected_exit,omitempty" json:"expectedExit,omitempty"`
	ExpectOutput string   `yaml:"expect_output,omitempty" json:"expectOutput,omitempty"` // regexp the combined output must match
	// http
	URL            string `yaml:"url,omitempty" json:"url,omitempty"`
	ExpectedStatus int    `yaml:"expected_status,omitempty" json:"expectedStatus,omitempty"`
	ExpectBody     string `yaml:"expect_body,omitempty" json:"expectBody,omitempty"`
	// tcp
	Host string `yaml:"host,omitempty" json:"host,omitempty"`
	Port int    `yaml:"port,omitempty" json:"port,omitempty"`
	// file
	Path     string `yaml:"path,omitempty" json:"path,omitempty"`
	Exists   *bool  `yaml:"exists,omitempty" json:"exists,omitempty"`
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	MinSize  int64  `yaml:"min_size,omitempty" json:"minSize,omitempty"`
	// services
	Services []string `yaml:"services,omitempty" json:"services,omitempty"` // services_healthy: these; no_running_services: allowed to be running
}

// Pipeline is a named list of checks.
type Pipeline struct {
	Name     string  `yaml:"name" json:"name"`
	FailFast bool    `yaml:"fail_fast,omitempty" json:"failFast,omitempty"`
	Checks   []Check `yaml:"checks" json:"checks"`
}

// IsRequired reports whether the check must pass for the pipeline to pass (default true).
func (c Check) IsRequired() bool { return c.Required == nil || *c.Required }

// Parse reads pipeline YAML strictly.
func Parse(data []byte) (*Pipeline, error) {
	var p Pipeline
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid verification pipeline: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

// Validate checks the pipeline before anything runs.
func (p *Pipeline) Validate() error {
	var probs []string
	add := func(f string, a ...any) { probs = append(probs, fmt.Sprintf(f, a...)) }
	if len(p.Checks) == 0 {
		add("pipeline has no checks")
	}
	seen := map[string]bool{}
	for i := range p.Checks {
		c := &p.Checks[i]
		if c.Type == "" {
			switch {
			case c.Command != "":
				c.Type = "command"
			case c.URL != "":
				c.Type = "http"
			case c.Port != 0:
				c.Type = "tcp"
			case c.Path != "":
				c.Type = "file"
			}
		}
		if !config.ValidName(c.Name) {
			add("check #%d: invalid name %q", i+1, c.Name)
		}
		if seen[c.Name] {
			add("duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
		if c.TimeoutSec < 0 || c.TimeoutSec > 3600 {
			add("check %q: timeout_sec must be 0-3600", c.Name)
		}
		switch c.Type {
		case "command":
			if strings.TrimSpace(c.Command) == "" {
				add("check %q: command is required", c.Name)
			}
			if c.ExpectOutput != "" {
				if _, err := regexp.Compile(c.ExpectOutput); err != nil {
					add("check %q: expect_output is not a valid regexp: %v", c.Name, err)
				}
			}
			if c.Dir != "" {
				if _, err := config.ResolveDir(".", c.Dir); err != nil {
					add("check %q: %v", c.Name, err)
				}
			}
		case "http":
			u, err := url.Parse(c.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				add("check %q: url must be an absolute http(s) URL", c.Name)
			} else if !config.IsLoopbackHost(u.Hostname()) {
				add("check %q: url host %q is not loopback; checks only target this machine", c.Name, u.Hostname())
			}
			if c.ExpectBody != "" {
				if _, err := regexp.Compile(c.ExpectBody); err != nil {
					add("check %q: expect_body is not a valid regexp: %v", c.Name, err)
				}
			}
		case "tcp":
			if c.Port < 1 || c.Port > 65535 {
				add("check %q: port must be 1-65535", c.Name)
			}
			if c.Host != "" && !config.IsLoopbackHost(c.Host) {
				add("check %q: host %q is not loopback", c.Name, c.Host)
			}
		case "file":
			if c.Path == "" {
				add("check %q: path is required", c.Name)
			} else if _, err := config.ResolveDir(".", c.Path); err != nil {
				add("check %q: %v", c.Name, err)
			}
		case "services_healthy", "no_running_services":
		default:
			add("check %q: unknown type %q (command, http, tcp, file, services_healthy, no_running_services)", c.Name, c.Type)
		}
		if c.WhenFile != "" {
			if _, err := config.ResolveDir(".", c.WhenFile); err != nil {
				add("check %q: when_file: %v", c.Name, err)
			}
		}
	}
	for _, c := range p.Checks {
		for _, n := range c.Needs {
			if !seen[n] {
				add("check %q needs unknown check %q", c.Name, n)
			}
			if n == c.Name {
				add("check %q needs itself", c.Name)
			}
		}
	}
	if len(probs) == 0 {
		if _, err := p.order(); err != nil {
			probs = append(probs, err.Error())
		}
	}
	if len(probs) > 0 {
		return &config.ValidationError{Problems: probs}
	}
	return nil
}

// order returns check indexes in dependency order (stable: declaration order among equals).
func (p *Pipeline) order() ([]int, error) {
	idx := map[string]int{}
	for i, c := range p.Checks {
		idx[c.Name] = i
	}
	state := make([]int, len(p.Checks))
	var out []int
	var path []string
	var visit func(i int) error
	visit = func(i int) error {
		if state[i] == 2 {
			return nil
		}
		if state[i] == 1 {
			return fmt.Errorf("dependency cycle detected: %s -> %s", strings.Join(path, " -> "), p.Checks[i].Name)
		}
		state[i] = 1
		path = append(path, p.Checks[i].Name)
		for _, n := range p.Checks[i].Needs {
			if j, ok := idx[n]; ok {
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		state[i] = 2
		out = append(out, i)
		return nil
	}
	for i := range p.Checks {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CommandOutcome is what the environment reports for a run command.
type CommandOutcome struct {
	ExitCode  int
	Output    string // combined, bounded tail
	CommandID string
	TimedOut  bool
	Cancelled bool
}

// Env is everything a pipeline needs from the execution it verifies. The
// execution engine implements it so every command is recorded.
type Env interface {
	Dir() string
	RunCommand(ctx context.Context, name, exe string, args []string, dir string, timeout time.Duration) (CommandOutcome, error)
	ServiceHealth(name string) (running bool, health string, known bool)
	RunningServices() []string
}

// Summary is the overall verdict.
type Summary struct {
	Overall string         `json:"overall"` // passed | failed | none
	Counts  map[string]int `json:"counts"`
	Reasons []string       `json:"reasons,omitempty"`
}

// Run executes the pipeline and returns per-check results plus the verdict.
func (p *Pipeline) Run(ctx context.Context, env Env) ([]domain.VerificationResult, Summary) {
	order, err := p.order()
	results := make([]domain.VerificationResult, len(p.Checks))
	if err != nil { // already rejected by Validate; defensive
		return nil, Summary{Overall: "failed", Reasons: []string{err.Error()}}
	}
	status := map[string]string{}
	failedStop := false
	for _, i := range order {
		c := p.Checks[i]
		r := domain.VerificationResult{Seq: i + 1, Name: c.Name, Description: c.Description, Type: c.Type, Required: c.IsRequired(), Needs: c.Needs, StartedAt: time.Now()}
		switch {
		case ctx.Err() != nil:
			r.Status, r.Error = domain.VSkipped, "verification was cancelled"
		case failedStop:
			r.Status, r.Error = domain.VSkipped, "fail_fast: an earlier required check failed"
		default:
			if bad := unmetNeed(c.Needs, status); bad != "" {
				r.Status, r.Error = domain.VSkipped, fmt.Sprintf("prerequisite %q did not pass", bad)
			} else if c.WhenFile != "" && !fileExists(filepath.Join(env.Dir(), c.WhenFile)) {
				r.Status, r.Error = domain.VNotApplicable, fmt.Sprintf("%s does not exist in the workspace", c.WhenFile)
			} else {
				execute(ctx, env, c, &r)
			}
		}
		r.EndedAt = time.Now()
		r.DurationMs = r.EndedAt.Sub(r.StartedAt).Milliseconds()
		status[c.Name] = r.Status
		results[i] = r
		if p.FailFast && c.IsRequired() && (r.Status == domain.VFailed || r.Status == domain.VTimedOut) {
			failedStop = true
		}
	}
	return results, Summarize(results)
}

func unmetNeed(needs []string, status map[string]string) string {
	for _, n := range needs {
		if s := status[n]; s != domain.VPassed && s != domain.VNotApplicable {
			return n
		}
	}
	return ""
}

// Summarize derives the verdict. Required checks that failed, timed out or
// were skipped all make the pipeline fail; optional ones only add reasons.
func Summarize(results []domain.VerificationResult) Summary {
	s := Summary{Counts: map[string]int{}}
	ran := 0
	for _, r := range results {
		s.Counts[r.Status]++
		if r.Status == domain.VPassed || r.Status == domain.VFailed || r.Status == domain.VTimedOut {
			ran++
		}
		bad := r.Status == domain.VFailed || r.Status == domain.VTimedOut || r.Status == domain.VSkipped
		if bad && r.Required {
			s.Reasons = append(s.Reasons, fmt.Sprintf("%s: %s", r.Name, firstNonEmpty(r.Error, r.Status)))
		}
	}
	switch {
	case len(results) == 0 || ran == 0 && len(s.Reasons) == 0:
		s.Overall = "none"
	case len(s.Reasons) > 0:
		s.Overall = "failed"
	default:
		s.Overall = "passed"
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func timeoutOf(c Check, def time.Duration) time.Duration {
	if c.TimeoutSec > 0 {
		return time.Duration(c.TimeoutSec) * time.Second
	}
	return def
}

func fail(r *domain.VerificationResult, format string, a ...any) {
	r.Status, r.Error = domain.VFailed, fmt.Sprintf(format, a...)
}

func execute(ctx context.Context, env Env, c Check, r *domain.VerificationResult) {
	switch c.Type {
	case "command":
		dir := env.Dir()
		if c.Dir != "" {
			d, err := config.ResolveInside(env.Dir(), c.Dir)
			if err != nil {
				fail(r, "%v", err)
				return
			}
			dir = d
		}
		out, err := env.RunCommand(ctx, c.Name, c.Command, c.Args, dir, timeoutOf(c, 10*time.Minute))
		r.Output, r.Evidence = out.Output, []string{"command:" + out.CommandID}
		ec := out.ExitCode
		r.ExitCode = &ec
		want := 0
		if c.ExpectedExit != nil {
			want = *c.ExpectedExit
		}
		switch {
		case err != nil:
			fail(r, "could not run %s: %v", c.Command, err)
			r.ExitCode = nil
		case out.TimedOut:
			r.Status, r.Error = domain.VTimedOut, fmt.Sprintf("exceeded %s", timeoutOf(c, 10*time.Minute))
		case out.Cancelled:
			fail(r, "cancelled")
		case out.ExitCode != want:
			fail(r, "exit code %d, expected %d", out.ExitCode, want)
		case c.ExpectOutput != "" && !regexp.MustCompile(c.ExpectOutput).MatchString(out.Output):
			fail(r, "output did not match %q", c.ExpectOutput)
		default:
			r.Status = domain.VPassed
		}
	case "http":
		pollHTTP(ctx, c, r)
	case "tcp":
		host := c.Host
		if host == "" {
			host = "127.0.0.1"
		}
		addr := net.JoinHostPort(host, strconv.Itoa(c.Port))
		r.Evidence = []string{"tcp:" + addr}
		poll(ctx, timeoutOf(c, 15*time.Second), r, func() error {
			cn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", addr)
			if err != nil {
				return err
			}
			return cn.Close()
		})
	case "file":
		checkFile(env, c, r)
	case "services_healthy":
		names := c.Services
		if len(names) == 0 {
			names = env.RunningServices()
		}
		if len(names) == 0 {
			r.Status, r.Error = domain.VNotApplicable, "the execution has no services"
			return
		}
		poll(ctx, timeoutOf(c, 30*time.Second), r, func() error {
			for _, n := range names {
				running, health, known := env.ServiceHealth(n)
				if !known {
					return fmt.Errorf("service %q does not exist", n)
				}
				if !running || (health != "healthy" && health != "not-monitored") {
					return fmt.Errorf("service %q is not healthy (running=%v, health=%s)", n, running, health)
				}
			}
			return nil
		})
		r.Evidence = names
	case "no_running_services":
		allowed := map[string]bool{}
		for _, a := range c.Services {
			allowed[a] = true
		}
		var left []string
		for _, n := range env.RunningServices() {
			if !allowed[n] {
				left = append(left, n)
			}
		}
		sort.Strings(left)
		if len(left) > 0 {
			fail(r, "unexpected services still running: %s", strings.Join(left, ", "))
		} else {
			r.Status = domain.VPassed
		}
	}
}

// poll retries fn once per interval until it succeeds or the timeout elapses.
func poll(ctx context.Context, timeout time.Duration, r *domain.VerificationResult, fn func() error) {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		last = fn()
		if last == nil {
			r.Status = domain.VPassed
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if errors.Is(last, context.DeadlineExceeded) || time.Now().After(deadline) {
				r.Status, r.Error = domain.VTimedOut, fmt.Sprintf("not satisfied within %s: %v", timeout, last)
			} else {
				fail(r, "%v", last)
			}
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func pollHTTP(ctx context.Context, c Check, r *domain.VerificationResult) {
	r.Evidence = []string{"http:" + c.URL}
	want := c.ExpectedStatus
	if want == 0 {
		want = 200
	}
	var re *regexp.Regexp
	if c.ExpectBody != "" {
		re = regexp.MustCompile(c.ExpectBody)
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	poll(ctx, timeoutOf(c, 30*time.Second), r, func() error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		r.Output = fmt.Sprintf("HTTP %d", resp.StatusCode)
		if resp.StatusCode != want {
			return fmt.Errorf("HTTP %d, expected %d", resp.StatusCode, want)
		}
		if re != nil && !re.Match(body) {
			return fmt.Errorf("response body did not match %q", c.ExpectBody)
		}
		return nil
	})
}

func checkFile(env Env, c Check, r *domain.VerificationResult) {
	p, err := config.ResolveInside(env.Dir(), c.Path)
	if err != nil {
		fail(r, "%v", err)
		return
	}
	r.Evidence = []string{"file:" + c.Path}
	wantExists := c.Exists == nil || *c.Exists
	st, statErr := os.Stat(p)
	exists := statErr == nil
	switch {
	case exists != wantExists:
		if wantExists {
			fail(r, "%s does not exist", c.Path)
		} else {
			fail(r, "%s exists but must not", c.Path)
		}
	case !exists:
		r.Status = domain.VPassed
	case c.MinSize > 0 && st.Size() < c.MinSize:
		fail(r, "%s is %d bytes, expected at least %d", c.Path, st.Size(), c.MinSize)
	case c.Contains != "":
		b, err := os.ReadFile(p)
		if err != nil {
			fail(r, "%v", err)
		} else if !strings.Contains(string(b), c.Contains) {
			fail(r, "%s does not contain %q", c.Path, c.Contains)
		} else {
			r.Status = domain.VPassed
		}
	default:
		r.Status = domain.VPassed
	}
}
