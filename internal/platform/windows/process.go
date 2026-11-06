//go:build windows

// Package winapi wraps the Windows APIs RUNTIDE depends on. Nothing in this
// package shells out to external commands.
package winapi

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modPsapi    = windows.NewLazySystemDLL("psapi.dll")
	modNtdll    = windows.NewLazySystemDLL("ntdll.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetProcessMemoryInfo     = modPsapi.NewProc("GetProcessMemoryInfo")
	procNtQueryInformationProc   = modNtdll.NewProc("NtQueryInformationProcess")
	procGlobalMemoryStatusEx     = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGenerateConsoleCtrlEvent = modKernel32.NewProc("GenerateConsoleCtrlEvent")
)

// ErrAccessDenied is returned when Windows refuses access to a process.
var ErrAccessDenied = errors.New("access denied")

// ProcessInfo is a raw snapshot of one process.
type ProcessInfo struct {
	PID         uint32
	PPID        uint32
	Name        string
	Path        string
	CmdLine     string
	Threads     uint32
	WorkingSet  uint64
	PrivateMem  uint64
	StartTime   time.Time
	CPUTime     time.Duration // kernel + user
	Accessible  bool          // false when the handle could not be opened
	AccessError string
}

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// ListProcesses enumerates every process using a Toolhelp snapshot and
// enriches each entry with the data the caller's privileges allow.
func ListProcesses(withCmdLine bool) ([]ProcessInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("create process snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, fmt.Errorf("enumerate processes: %w", err)
	}
	var out []ProcessInfo
	for {
		p := ProcessInfo{
			PID:     entry.ProcessID,
			PPID:    entry.ParentProcessID,
			Name:    windows.UTF16ToString(entry.ExeFile[:]),
			Threads: entry.Threads,
		}
		enrich(&p, withCmdLine)
		out = append(out, p)
		if err := windows.Process32Next(snap, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return out, fmt.Errorf("enumerate processes: %w", err)
		}
	}
	return out, nil
}

func enrich(p *ProcessInfo, withCmdLine bool) {
	if p.PID == 0 {
		p.Accessible = true
		return
	}
	access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
	h, err := windows.OpenProcess(access, false, p.PID)
	if err != nil {
		p.AccessError = describeErr(err)
		return
	}
	defer windows.CloseHandle(h)
	p.Accessible = true

	var buf [windows.MAX_PATH * 4]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err == nil {
		p.Path = windows.UTF16ToString(buf[:size])
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
		p.StartTime = time.Unix(0, creation.Nanoseconds())
		p.CPUTime = time.Duration((uint64(kernel.HighDateTime)<<32|uint64(kernel.LowDateTime))+
			(uint64(user.HighDateTime)<<32|uint64(user.LowDateTime))) * 100
	}
	var mc processMemoryCounters
	mc.CB = uint32(unsafe.Sizeof(mc))
	if r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.CB)); r != 0 {
		p.WorkingSet = uint64(mc.WorkingSetSize)
		p.PrivateMem = uint64(mc.PagefileUsage)
	}
	if withCmdLine {
		p.CmdLine = readCommandLine(p.PID)
	}
}

func describeErr(err error) string {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return "access denied"
	}
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return "process no longer exists"
	}
	return err.Error()
}

type processBasicInformation struct {
	ExitStatus       uintptr
	PebBaseAddress   uintptr
	AffinityMask     uintptr
	BasePriority     uintptr
	UniqueProcessID  uintptr
	InheritedFromPID uintptr
}

type unicodeString struct {
	Length    uint16
	MaxLength uint16
	_         uint32
	Buffer    uintptr
}

// readCommandLine reads the command line out of the target's PEB. It needs
// PROCESS_VM_READ, so it quietly returns "" for processes we cannot inspect
// (protected processes, other users, elevated processes).
func readCommandLine(pid uint32) string {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var pbi processBasicInformation
	var ret uint32
	st, _, _ := procNtQueryInformationProc.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&pbi)), unsafe.Sizeof(pbi), uintptr(unsafe.Pointer(&ret)))
	if st != 0 || pbi.PebBaseAddress == 0 {
		return ""
	}
	var params uintptr
	var n uintptr
	// PEB.ProcessParameters at offset 0x20 (x64).
	if err := windows.ReadProcessMemory(h, pbi.PebBaseAddress+0x20, (*byte)(unsafe.Pointer(&params)), unsafe.Sizeof(params), &n); err != nil || params == 0 {
		return ""
	}
	var us unicodeString
	// RTL_USER_PROCESS_PARAMETERS.CommandLine at offset 0x70 (x64).
	if err := windows.ReadProcessMemory(h, params+0x70, (*byte)(unsafe.Pointer(&us)), unsafe.Sizeof(us), &n); err != nil || us.Length == 0 || us.Buffer == 0 {
		return ""
	}
	if us.Length > 32768 {
		us.Length = 32768
	}
	buf := make([]uint16, us.Length/2)
	if err := windows.ReadProcessMemory(h, us.Buffer, (*byte)(unsafe.Pointer(&buf[0])), uintptr(us.Length), &n); err != nil {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// KillResult describes what actually happened when terminating a process.
type KillResult struct {
	Terminated bool
	Detail     string
}

// TerminateProcess forcefully ends a process and verifies that it exited.
// expectedStart guards against PID reuse: when non-zero the process creation
// time must match.
func TerminateProcess(pid uint32, expectedStart time.Time) (KillResult, error) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return KillResult{Detail: "Windows denied access. The process belongs to another user, is elevated or is protected. RUNTIDE does not elevate privileges automatically."}, ErrAccessDenied
		}
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return KillResult{Detail: "The process no longer exists."}, err
		}
		return KillResult{Detail: err.Error()}, err
	}
	defer windows.CloseHandle(h)
	if !expectedStart.IsZero() {
		var c, e, k, u windows.Filetime
		if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err == nil {
			if d := time.Unix(0, c.Nanoseconds()).Sub(expectedStart); d > 2*time.Second || d < -2*time.Second {
				return KillResult{Detail: "PID was reused by a different process; refusing to terminate."}, errors.New("pid reused")
			}
		}
	}
	if err := windows.TerminateProcess(h, 1); err != nil {
		return KillResult{Detail: "TerminateProcess failed: " + err.Error()}, err
	}
	ev, err := windows.WaitForSingleObject(h, 5000)
	if err != nil || ev != windows.WAIT_OBJECT_0 {
		return KillResult{Detail: "Termination was requested but the process is still running."}, errors.New("process did not exit")
	}
	return KillResult{Terminated: true, Detail: "Process exited."}, nil
}

// SendCtrlBreak sends CTRL_BREAK to a process group. It only works when the
// target shares a console with the caller; callers must treat failure as
// "fall back to forceful termination after the grace period".
func SendCtrlBreak(pgid uint32) error {
	r, _, err := procGenerateConsoleCtrlEvent.Call(uintptr(windows.CTRL_BREAK_EVENT), uintptr(pgid))
	if r == 0 {
		return err
	}
	return nil
}

// IsRunning reports whether the pid refers to a live process.
func IsRunning(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}

// WindowsDir returns %SystemRoot%.
func WindowsDir() string {
	d, err := windows.GetWindowsDirectory()
	if err != nil {
		return `C:\Windows`
	}
	return strings.TrimRight(d, `\`)
}

// ProcessUser returns DOMAIN\user owning the process, or "" when unknown.
func ProcessUser(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return ""
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	account, domain, _, err := u.User.Sid.LookupAccount("")
	if err != nil {
		return u.User.Sid.String()
	}
	return domain + `\` + account
}

// IsElevated reports whether the current process runs with an elevated token.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
