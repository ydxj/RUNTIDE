// Package ports implements the port manager on top of the Windows TCP/UDP
// owner-PID tables.
package ports

import (
	"sort"
	"strings"

	"github.com/runtide/runtide/internal/process"
	winapi "github.com/runtide/runtide/internal/platform/windows"
)

// Listener is one listening socket with its owning process resolved.
type Listener struct {
	Protocol    string `json:"protocol"`
	Family      string `json:"family"`
	Address     string `json:"address"`
	Port        uint16 `json:"port"`
	PID         uint32 `json:"pid"`
	ProcessName string `json:"processName"`
	ProcessPath string `json:"processPath"`
	Exposure    string `json:"exposure"` // loopback | all-interfaces | network
	Managed     string `json:"managed,omitempty"`
	Protection  string `json:"protection"`
	Conflict    string `json:"conflict,omitempty"`
	Stale       bool   `json:"stale"` // owning PID could not be resolved
}

// List returns TCP listeners (and UDP bindings when includeUDP) with owners.
func List(procs []process.Process, includeUDP bool) ([]Listener, error) {
	tcp, err := winapi.ListTCP(true)
	if err != nil {
		return nil, err
	}
	eps := tcp
	if includeUDP {
		udp, err := winapi.ListUDP()
		if err == nil {
			eps = append(eps, udp...)
		}
	}
	byPID := make(map[uint32]process.Process, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	out := make([]Listener, 0, len(eps))
	for _, ep := range eps {
		l := Listener{
			Protocol: ep.Protocol, Family: ep.Family, Address: ep.Addr.String(), Port: ep.Port, PID: ep.PID,
			Exposure: exposure(ep.Addr.String()),
		}
		if p, ok := byPID[ep.PID]; ok {
			l.ProcessName, l.ProcessPath, l.Managed, l.Protection = p.Name, p.Path, p.Managed, p.Protection
		} else {
			l.Stale = true
			l.ProcessName = "(unknown)"
		}
		out = append(out, l)
	}
	markConflicts(out)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Family < out[j].Family
	})
	return out, nil
}

func exposure(addr string) string {
	switch {
	case addr == "0.0.0.0" || addr == "::":
		return "all-interfaces"
	case addr == "127.0.0.1" || addr == "::1" || strings.HasPrefix(addr, "127."):
		return "loopback"
	default:
		return "network"
	}
}

// markConflicts flags ports bound by more than one distinct process on the
// same protocol: this is the classic "my server says address in use / answers
// from the wrong app" situation (e.g. 0.0.0.0:3000 and 127.0.0.1:3000).
func markConflicts(ls []Listener) {
	type key struct {
		proto string
		port  uint16
	}
	owners := map[key]map[uint32]bool{}
	for _, l := range ls {
		k := key{l.Protocol, l.Port}
		if owners[k] == nil {
			owners[k] = map[uint32]bool{}
		}
		owners[k][l.PID] = true
	}
	for i := range ls {
		k := key{ls[i].Protocol, ls[i].Port}
		if len(owners[k]) > 1 {
			ls[i].Conflict = "port is bound by multiple processes"
		}
	}
}

// Find returns listeners on port.
func Find(ls []Listener, port uint16) []Listener {
	var out []Listener
	for _, l := range ls {
		if l.Port == port {
			out = append(out, l)
		}
	}
	return out
}

// Filter applies a free-text query (port, process name, address, PID).
func Filter(ls []Listener, q string) []Listener {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ls
	}
	var out []Listener
	for _, l := range ls {
		if strings.Contains(strings.ToLower(l.ProcessName), q) || strings.Contains(l.Address, q) ||
			strings.Contains(itoa(int(l.Port)), q) || itoa(int(l.PID)) == q {
			out = append(out, l)
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

// IsFree reports whether nothing is listening on port (TCP, any address).
func IsFree(port uint16) (bool, []Listener, error) {
	tcp, err := winapi.ListTCP(true)
	if err != nil {
		return false, nil, err
	}
	var hits []Listener
	for _, ep := range tcp {
		if ep.Port == port {
			hits = append(hits, Listener{Protocol: "tcp", Family: ep.Family, Address: ep.Addr.String(), Port: ep.Port, PID: ep.PID})
		}
	}
	return len(hits) == 0, hits, nil
}
