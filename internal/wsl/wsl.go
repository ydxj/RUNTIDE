// Package wsl reports Windows Subsystem for Linux availability and the list
// of installed distributions. It is read-only: it never runs anything inside
// a distribution and never modifies one.
package wsl

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/runtide/runtide/internal/procutil"
)

// Distro is one installed distribution.
type Distro struct {
	Name    string `json:"name"`
	State   string `json:"state"` // Running | Stopped | ...
	Version int    `json:"version"`
	Default bool   `json:"default"`
}

// Status describes WSL availability.
type Status struct {
	Installed bool     `json:"installed"`
	Path      string   `json:"path"`
	Distros   []Distro `json:"distros"`
	Message   string   `json:"message"`
}

var runner = func(ctx context.Context, args ...string) ([]byte, error) {
	return procutil.Hide(exec.CommandContext(ctx, "wsl.exe", args...)).Output()
}

// decode converts wsl.exe output (UTF-16LE, no BOM) to a string; plain ASCII
// output is passed through.
func decode(b []byte) string {
	if len(b) >= 2 && bytes.IndexByte(b, 0) >= 0 {
		if len(b)%2 == 1 {
			b = b[:len(b)-1]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		return strings.TrimPrefix(string(utf16.Decode(u)), "\xEF\xBB\xBF")
	}
	return string(b)
}

// parseList parses `wsl -l -v`. The header text is localised, so parsing is
// positional: skip the first line; each row is [*] NAME STATE VERSION.
func parseList(text string) []Distro {
	var out []Distro
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for i, l := range lines {
		if i == 0 || strings.TrimSpace(l) == "" {
			continue
		}
		def := strings.HasPrefix(strings.TrimSpace(l), "*")
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "*"))
		if len(f) < 3 {
			continue
		}
		ver := 0
		if v := f[len(f)-1]; v == "1" || v == "2" {
			ver = int(v[0] - '0')
		} else {
			continue
		}
		out = append(out, Distro{Name: strings.Join(f[:len(f)-2], " "), State: f[len(f)-2], Version: ver, Default: def})
	}
	return out
}

// GetStatus queries WSL.
func GetStatus(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var s Status
	p, err := exec.LookPath("wsl.exe")
	if err != nil {
		s.Message = "wsl.exe was not found. WSL is not installed on this system."
		return s
	}
	s.Path = p
	out, err := runner(ctx, "-l", "-v")
	text := decode(out)
	if err != nil {
		s.Installed = true
		s.Message = "WSL is present but reports no installed distributions (or is not enabled)."
		if t := strings.TrimSpace(text); t != "" {
			s.Message += " " + firstLine(t)
		}
		return s
	}
	s.Installed = true
	s.Distros = parseList(text)
	if len(s.Distros) == 0 {
		s.Message = "WSL is installed but no distributions are installed."
	} else {
		s.Message = "WSL is available."
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i > 0 {
		return s[:i]
	}
	return s
}
