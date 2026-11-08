// Package docker integrates with an existing Docker installation through its
// CLI. Docker is optional: every function degrades to an accurate status.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/procutil"
)

// Status is the high-level Docker availability picture.
type Status struct {
	CLIInstalled     bool   `json:"cliInstalled"`
	CLIPath          string `json:"cliPath"`
	ClientVersion    string `json:"clientVersion"`
	DesktopInstalled bool   `json:"desktopInstalled"`
	DesktopRunning   bool   `json:"desktopRunning"`
	DaemonAvailable  bool   `json:"daemonAvailable"`
	ServerVersion    string `json:"serverVersion"`
	Message          string `json:"message"`
}

// Container is a Docker container.
type Container struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	State          string   `json:"state"`
	Status         string   `json:"status"`
	Ports          []string `json:"ports"`
	ComposeProject string   `json:"composeProject,omitempty"`
	ComposeService string   `json:"composeService,omitempty"`
	CPUPercent     string   `json:"cpuPercent,omitempty"`
	MemUsage       string   `json:"memUsage,omitempty"`
}

// runner executes the docker CLI; replaced in tests.
var runner = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := procutil.Hide(exec.CommandContext(ctx, "docker", args...))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), errors.New(firstLine(msg))
	}
	return out.Bytes(), nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		s = s[:i]
	}
	return s
}

var desktopPaths = []string{
	`C:\Program Files\Docker\Docker\Docker Desktop.exe`,
}

// GetStatus inspects CLI, Docker Desktop and daemon reachability.
func GetStatus(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var s Status
	if p, err := exec.LookPath("docker"); err == nil {
		s.CLIInstalled, s.CLIPath = true, p
	}
	for _, p := range desktopPaths {
		if _, err := os.Stat(p); err == nil {
			s.DesktopInstalled = true
		}
	}
	if !s.CLIInstalled {
		s.Message = "Docker CLI was not found on PATH. Docker is optional; install Docker Desktop to use container features."
		return s
	}
	if out, err := runner(ctx, "version", "--format", "{{.Client.Version}}"); err == nil || len(out) > 0 {
		s.ClientVersion = strings.TrimSpace(string(out))
	}
	out, err := runner(ctx, "info", "--format", "{{.ServerVersion}}")
	if err == nil && strings.TrimSpace(string(out)) == "" {
		// `docker info` can exit 0 with only client output when the daemon is down;
		// a reachable daemon always reports a server version.
		err = errors.New("the docker CLI reported no server version")
	}
	if err != nil {
		s.Message = "Docker CLI is installed but the daemon is not reachable: " + err.Error()
		if s.DesktopInstalled {
			s.Message += " (Docker Desktop may not be running.)"
		}
		return s
	}
	s.DaemonAvailable = true
	s.DesktopRunning = true
	s.ServerVersion = strings.TrimSpace(string(out))
	s.Message = "Docker daemon is running."
	return s
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// ValidID rejects anything that could be parsed as a CLI flag.
func ValidID(id string) bool { return idRe.MatchString(id) }

// List returns containers; when all is false only running ones.
func List(ctx context.Context, all bool) ([]Container, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := []string{"ps", "--no-trunc", "--format", "{{json .}}"}
	if all {
		args = append(args, "-a")
	}
	out, err := runner(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePS(out)
}

func parsePS(out []byte) ([]Container, error) {
	var cs []Container
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r struct {
			ID, Names, Image, State, Status, Ports, Labels string
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("unexpected docker output: %w", err)
		}
		c := Container{ID: r.ID, Name: r.Names, Image: r.Image, State: r.State, Status: r.Status}
		if len(c.ID) > 12 {
			c.ID = c.ID[:12]
		}
		for _, p := range strings.Split(r.Ports, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Ports = append(c.Ports, p)
			}
		}
		for _, kv := range strings.Split(r.Labels, ",") {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "com.docker.compose.project":
				c.ComposeProject = v
			case "com.docker.compose.service":
				c.ComposeService = v
			}
		}
		cs = append(cs, c)
	}
	return cs, sc.Err()
}

// WithStats annotates running containers with CPU/memory from `docker stats`.
func WithStats(ctx context.Context, cs []Container) []Container {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := runner(ctx, "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		return cs
	}
	stats := map[string][2]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var r struct{ ID, CPUPerc, MemUsage string }
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID != "" {
			if len(r.ID) > 12 {
				r.ID = r.ID[:12]
			}
			stats[r.ID] = [2]string{r.CPUPerc, r.MemUsage}
		}
	}
	for i := range cs {
		if s, ok := stats[cs[i].ID]; ok {
			cs[i].CPUPercent, cs[i].MemUsage = s[0], s[1]
		}
	}
	return cs
}

// Start starts a container by id or name.
func Start(ctx context.Context, id string) error { return act(ctx, "start", id) }

// Stop stops a container gracefully (Docker's own timeout applies).
func Stop(ctx context.Context, id string) error { return act(ctx, "stop", id) }

func act(ctx context.Context, verb, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid container identifier %q", id)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, err := runner(ctx, verb, id)
	return err
}

// ComposeFile returns a compose file found in dir, or "".
func ComposeFile(dir string) string {
	for _, n := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// ComposeProjects groups containers by compose project.
func ComposeProjects(cs []Container) map[string][]Container {
	m := map[string][]Container{}
	for _, c := range cs {
		if c.ComposeProject != "" {
			m[c.ComposeProject] = append(m[c.ComposeProject], c)
		}
	}
	return m
}
