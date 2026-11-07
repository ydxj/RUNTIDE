// Package process provides the process explorer: enumeration, CPU sampling,
// tree building, classification and safe termination.
package process

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	winapi "github.com/runtide/runtide/internal/platform/windows"
)

// Protection levels.
const (
	ProtNone      = ""
	ProtProtected = "protected" // never terminable through ordinary controls
	ProtCaution   = "caution"   // allowed, but the UI must warn loudly
)

// Process is one row of the explorer.
type Process struct {
	PID            uint32    `json:"pid"`
	PPID           uint32    `json:"ppid"`
	Name           string    `json:"name"`
	Path           string    `json:"path"`
	CmdLine        string    `json:"cmdLine"`
	Threads        uint32    `json:"threads"`
	MemoryBytes    uint64    `json:"memoryBytes"`
	PrivateBytes   uint64    `json:"privateBytes"`
	CPUPercent     float64   `json:"cpuPercent"`
	CPUSeconds     float64   `json:"cpuSeconds"`
	StartTime      time.Time `json:"startTime"`
	Accessible     bool      `json:"accessible"`
	AccessError    string    `json:"accessError,omitempty"`
	Protection     string    `json:"protection"`
	ProtectionNote string    `json:"protectionNote,omitempty"`
	Managed        string    `json:"managed,omitempty"` // RUNTIDE service key when supervised
}

type cpuSample struct {
	cpu  time.Duration
	when time.Time
}

// Explorer samples processes and remembers the previous sample so it can
// compute CPU usage deltas.
type Explorer struct {
	mu      sync.Mutex
	prev    map[uint32]cpuSample
	numCPU  int
	selfPID uint32
	winDir  string
	managed func() map[uint32]string // pid -> service key
}

// NewExplorer creates an explorer. managed may be nil.
func NewExplorer(managed func() map[uint32]string) *Explorer {
	return &Explorer{
		prev:    map[uint32]cpuSample{},
		numCPU:  runtime.NumCPU(),
		selfPID: uint32(os.Getpid()),
		winDir:  winapi.WindowsDir(),
		managed: managed,
	}
}

var criticalNames = map[string]bool{
	"system": true, "system idle process": true, "registry": true, "memory compression": true,
	"secure system": true, "smss.exe": true, "csrss.exe": true, "wininit.exe": true,
	"services.exe": true, "lsass.exe": true, "winlogon.exe": true, "lsaiso.exe": true,
	"svchost.exe": true, "dwm.exe": true, "fontdrvhost.exe": true, "sihost.exe": true,
	"taskhostw.exe": true, "ctfmon.exe": true, "wudfhost.exe": true, "spoolsv.exe": true,
	"msmpeng.exe": true, "securityhealthservice.exe": true, "runtimebroker.exe": true,
}

// Classify decides the protection level of a process using only its
// attributes, so it is unit-testable.
func Classify(p Process, selfPID uint32, winDir string) (level, note string) {
	if p.PID == 0 || p.PID == 4 {
		return ProtProtected, "Windows kernel process"
	}
	if p.PID == selfPID {
		return ProtProtected, "This is RUNTIDE itself"
	}
	lname := strings.ToLower(p.Name)
	if criticalNames[lname] {
		return ProtProtected, "Critical Windows system process; terminating it can crash or log out the system"
	}
	if p.Path != "" && winDir != "" {
		lp := strings.ToLower(p.Path)
		lw := strings.ToLower(winDir) + `\`
		if strings.HasPrefix(lp, lw+`system32\`) || strings.HasPrefix(lp, lw+`syswow64\`) {
			return ProtProtected, "Located in the Windows system directory"
		}
	}
	if lname == "explorer.exe" {
		return ProtCaution, "Windows shell: terminating it removes the taskbar and desktop until restarted"
	}
	if !p.Accessible {
		return ProtCaution, "Details unavailable (" + p.AccessError + "); it likely belongs to another user or is elevated"
	}
	return ProtNone, ""
}

// Snapshot enumerates processes and computes CPU percentages relative to the
// previous call. The first call reports 0% for everything.
func (e *Explorer) Snapshot(withCmdLine bool) ([]Process, error) {
	raw, err := winapi.ListProcesses(withCmdLine)
	if err != nil && len(raw) == 0 {
		return nil, err
	}
	now := time.Now()
	var managed map[uint32]string
	if e.managed != nil {
		managed = e.managed()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next := make(map[uint32]cpuSample, len(raw))
	out := make([]Process, 0, len(raw))
	for _, r := range raw {
		p := Process{
			PID: r.PID, PPID: r.PPID, Name: r.Name, Path: r.Path, CmdLine: r.CmdLine,
			Threads: r.Threads, MemoryBytes: r.WorkingSet, PrivateBytes: r.PrivateMem,
			CPUSeconds: r.CPUTime.Seconds(), StartTime: r.StartTime,
			Accessible: r.Accessible, AccessError: r.AccessError, Managed: managed[r.PID],
		}
		if old, ok := e.prev[r.PID]; ok && r.CPUTime >= old.cpu {
			if dt := now.Sub(old.when); dt > 0 {
				p.CPUPercent = float64(r.CPUTime-old.cpu) / float64(dt) / float64(e.numCPU) * 100
			}
		}
		next[r.PID] = cpuSample{r.CPUTime, now}
		p.Protection, p.ProtectionNote = Classify(p, e.selfPID, e.winDir)
		out = append(out, p)
	}
	e.prev = next
	return out, nil
}

// SnapshotWithCPU takes two samples gap apart so one-shot callers (the CLI)
// get meaningful CPU figures.
func (e *Explorer) SnapshotWithCPU(gap time.Duration, withCmdLine bool) ([]Process, error) {
	if _, err := e.Snapshot(false); err != nil {
		return nil, err
	}
	time.Sleep(gap)
	return e.Snapshot(withCmdLine)
}

// Node is a process tree node.
type Node struct {
	Process
	Children []*Node `json:"children"`
}

// BuildTree arranges processes by parent. A process whose parent is missing
// (or whose parent started later, i.e. PID reuse) becomes a root.
func BuildTree(procs []Process) []*Node {
	byPID := make(map[uint32]*Node, len(procs))
	for i := range procs {
		byPID[procs[i].PID] = &Node{Process: procs[i]}
	}
	var roots []*Node
	for _, n := range byPID {
		parent, ok := byPID[n.PPID]
		if !ok || n.PPID == n.PID || (!n.StartTime.IsZero() && !parent.StartTime.IsZero() && parent.StartTime.After(n.StartTime)) {
			roots = append(roots, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}
	var sortNodes func([]*Node)
	sortNodes = func(ns []*Node) {
		sort.Slice(ns, func(i, j int) bool { return ns[i].PID < ns[j].PID })
		for _, n := range ns {
			sortNodes(n.Children)
		}
	}
	sortNodes(roots)
	return roots
}

// Filter returns processes whose name, path, command line or PID contain q.
func Filter(procs []Process, q string) []Process {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return procs
	}
	var out []Process
	for _, p := range procs {
		if strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Path), q) ||
			strings.Contains(strings.ToLower(p.CmdLine), q) || fmt.Sprint(p.PID) == q {
			out = append(out, p)
		}
	}
	return out
}

// ErrProtected is returned when termination is refused for safety.
var ErrProtected = errors.New("process is protected")

// TerminateResult is what the UI/CLI report back.
type TerminateResult struct {
	PID        uint32 `json:"pid"`
	Name       string `json:"name"`
	Terminated bool   `json:"terminated"`
	Detail     string `json:"detail"`
}

// Terminate ends a process after re-validating it. It re-reads the process to
// avoid acting on stale information, refuses protected processes, and only
// reports success when Windows confirms the process exited.
func (e *Explorer) Terminate(pid uint32, expectedStart time.Time) (TerminateResult, error) {
	procs, err := e.Snapshot(false)
	if err != nil {
		return TerminateResult{PID: pid}, err
	}
	var target *Process
	for i := range procs {
		if procs[i].PID == pid {
			target = &procs[i]
			break
		}
	}
	if target == nil {
		return TerminateResult{PID: pid, Detail: "The process no longer exists."}, errors.New("process not found")
	}
	res := TerminateResult{PID: pid, Name: target.Name}
	if target.Protection == ProtProtected {
		res.Detail = "Refused: " + target.ProtectionNote
		return res, ErrProtected
	}
	if expectedStart.IsZero() {
		expectedStart = target.StartTime
	}
	kr, err := winapi.TerminateProcess(pid, expectedStart)
	res.Terminated = kr.Terminated
	res.Detail = kr.Detail
	return res, err
}
