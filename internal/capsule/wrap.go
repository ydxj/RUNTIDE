package capsule

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/runtide/runtide/internal/domain"
)

// DockerConfig is the container level configuration.
type DockerConfig struct {
	Image   string   `yaml:"image" json:"image"`
	Mounts  []Mount  `yaml:"mounts,omitempty" json:"mounts,omitempty"` // extra mounts; the workspace itself is always mounted at /workspace
	Ports   []string `yaml:"ports,omitempty" json:"ports,omitempty"`   // "8080:80", published on 127.0.0.1 only
	CPUs    float64  `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory  string   `yaml:"memory,omitempty" json:"memory,omitempty"`   // e.g. 512m, 2g
	Network string   `yaml:"network,omitempty" json:"network,omitempty"` // "" (default bridge) or "none"
}

// Mount is an additional bind mount (read-only unless explicitly writable).
type Mount struct {
	Host     string `yaml:"host" json:"host"`
	Target   string `yaml:"target" json:"target"`
	Writable bool   `yaml:"writable,omitempty" json:"writable,omitempty"`
}

// WSLConfig selects the distribution.
type WSLConfig struct {
	Distro string `yaml:"distro" json:"distro"`
}

var (
	imageRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]{0,199}$`)
	memRe     = regexp.MustCompile(`^[1-9][0-9]*[kmg]$`)
	portRe    = regexp.MustCompile(`^[0-9]{1,5}:[0-9]{1,5}(/(tcp|udp))?$`)
	distroRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	denyMount = []string{`\.ssh`, `\.aws`, `\.azure`, `\.gnupg`, `\.kube`, `\appdata`, `\windows`, `\program files`, `\users\`, `docker.sock`, `docker_engine`}
)

// ValidateDocker rejects unsafe container configuration. workspaceDir is the
// only directory mounted automatically.
func ValidateDocker(c DockerConfig, workspaceDir string) error {
	if !imageRe.MatchString(c.Image) {
		return fmt.Errorf("docker.image %q is not a valid image reference", c.Image)
	}
	if c.Memory != "" && !memRe.MatchString(strings.ToLower(c.Memory)) {
		return fmt.Errorf("docker.memory %q must look like 512m or 2g", c.Memory)
	}
	if c.CPUs < 0 || c.CPUs > 64 {
		return fmt.Errorf("docker.cpus must be between 0 and 64")
	}
	if c.Network != "" && c.Network != "none" {
		return fmt.Errorf("docker.network must be empty or \"none\"")
	}
	for _, p := range c.Ports {
		if !portRe.MatchString(p) {
			return fmt.Errorf("docker.ports entry %q must look like 8080:80", p)
		}
	}
	ws := strings.ToLower(filepath.Clean(workspaceDir))
	for _, m := range c.Mounts {
		h := strings.ToLower(filepath.Clean(m.Host))
		if !filepath.IsAbs(m.Host) || len(h) <= 3 {
			return fmt.Errorf("mount host %q must be an absolute path below a drive root", m.Host)
		}
		if !strings.HasPrefix(m.Target, "/") || strings.ContainsAny(m.Target, " :,") {
			return fmt.Errorf("mount target %q must be an absolute container path", m.Target)
		}
		inside := h == ws || strings.HasPrefix(h, ws+`\`)
		for _, d := range denyMount {
			if strings.Contains(h+`\`, d) && !inside {
				return fmt.Errorf("mount %q is refused: it exposes a sensitive host location (%s)", m.Host, strings.TrimPrefix(d, `\`))
			}
		}
		if strings.ContainsAny(m.Host, ",:") && !strings.HasPrefix(h[1:], ":\\") {
			return fmt.Errorf("mount host %q contains unsupported characters", m.Host)
		}
	}
	return nil
}

// ValidateWSL checks the distro name.
func ValidateWSL(c WSLConfig) error {
	if !distroRe.MatchString(c.Distro) {
		return fmt.Errorf("wsl.distro %q is not a valid distribution name", c.Distro)
	}
	return nil
}

// Wrap converts spec for the isolation level. The returned cleanup (may be nil)
// must run after the command ends (it removes a container left behind by a kill).
// Explicit environment values are passed through the client's environment so
// they never appear on a command line.
func Wrap(level domain.Isolation, spec Spec, name string, d *DockerConfig, w *WSLConfig) (Spec, func() Spec, error) {
	switch level {
	case domain.IsolationManaged, domain.IsolationWorktree:
		return spec, nil, nil
	case domain.IsolationDocker:
		if d == nil {
			return spec, nil, fmt.Errorf("isolation docker requires a docker section")
		}
		if err := ValidateDocker(*d, spec.Dir); err != nil {
			return spec, nil, err
		}
		cname := "runtide-" + sanitize(name)
		args := []string{"run", "--rm", "--name", cname, "--init", "-v", spec.Dir + ":/workspace", "-w", "/workspace"}
		if d.CPUs > 0 {
			args = append(args, "--cpus", strconv.FormatFloat(d.CPUs, 'f', -1, 64))
		}
		if d.Memory != "" {
			args = append(args, "--memory", strings.ToLower(d.Memory))
		}
		if d.Network == "none" {
			args = append(args, "--network", "none")
		}
		for _, p := range d.Ports {
			args = append(args, "-p", "127.0.0.1:"+p)
		}
		for _, m := range d.Mounts {
			mode := "ro"
			if m.Writable {
				mode = "rw"
			}
			args = append(args, "-v", m.Host+":"+m.Target+":"+mode)
		}
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		for _, k := range sortedStrings(keys) {
			args = append(args, "-e", k) // value comes from the docker client's environment
		}
		args = append(args, d.Image, spec.Executable)
		args = append(args, spec.Args...)
		out := Spec{Executable: "docker", Args: args, Dir: spec.Dir, Env: spec.Env, Timeout: spec.Timeout}
		cleanup := func() Spec { return Spec{Executable: "docker", Args: []string{"rm", "-f", cname}, Dir: spec.Dir} }
		return out, cleanup, nil
	case domain.IsolationWSL:
		if w == nil {
			return spec, nil, fmt.Errorf("isolation wsl requires a wsl section")
		}
		if err := ValidateWSL(*w); err != nil {
			return spec, nil, err
		}
		args := []string{"-d", w.Distro, "--cd", spec.Dir, "--exec", spec.Executable}
		args = append(args, spec.Args...)
		env := map[string]string{}
		var names []string
		for k, v := range spec.Env {
			env[k] = v
			names = append(names, k)
		}
		if len(names) > 0 {
			env["WSLENV"] = strings.Join(sortedStrings(names), ":")
		}
		return Spec{Executable: "wsl.exe", Args: args, Dir: spec.Dir, Env: env, Timeout: spec.Timeout}, nil, nil
	}
	return spec, nil, fmt.Errorf("unknown isolation level %d", level)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
