//go:build windows

package winapi

import (
	"fmt"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	procAttachConsole         = modKernel32.NewProc("AttachConsole")
	procFreeConsole           = modKernel32.NewProc("FreeConsole")
	procSetConsoleCtrlHandler = modKernel32.NewProc("SetConsoleCtrlHandler")
	procNtResumeProcess       = modNtdll.NewProc("NtResumeProcess")

	consoleMu   sync.Mutex
	swallowOnce sync.Once
	swallowCB   uintptr
)

// GracefulInterrupt asks every process sharing pid's hidden console to stop by
// delivering CTRL_BREAK. Services are launched with their own hidden console
// (CREATE_NO_WINDOW), so only that service's processes are reached.
//
// The technique temporarily detaches the caller from its own console and
// attaches to the target's, serialised by a mutex. While attached, a handler
// swallows the event for the caller so RUNTIDE does not interrupt itself. It
// returns an error when the target has no console; callers then fall back to
// forceful termination after their grace period.
func GracefulInterrupt(pid uint32) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	swallowOnce.Do(func() {
		swallowCB = syscall.NewCallback(func(ctrlType uint32) uintptr { return 1 })
	})
	procFreeConsole.Call()
	r, _, err := procAttachConsole.Call(uintptr(pid))
	if r == 0 {
		return fmt.Errorf("attach to console of pid %d: %w", pid, err)
	}
	procSetConsoleCtrlHandler.Call(swallowCB, 1)
	r2, _, err2 := procGenerateConsoleCtrlEvent.Call(uintptr(windows.CTRL_BREAK_EVENT), 0)
	time.Sleep(150 * time.Millisecond) // let the event be delivered before detaching
	procFreeConsole.Call()
	procSetConsoleCtrlHandler.Call(swallowCB, 0)
	if r2 == 0 {
		return fmt.Errorf("deliver CTRL_BREAK: %w", err2)
	}
	return nil
}

// ResumeProcess resumes a process created with CREATE_SUSPENDED.
func ResumeProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, pid)
	if err != nil {
		return fmt.Errorf("open process to resume: %w", err)
	}
	defer windows.CloseHandle(h)
	if st, _, _ := procNtResumeProcess.Call(uintptr(h)); st != 0 {
		return fmt.Errorf("NtResumeProcess failed: status 0x%x", st)
	}
	return nil
}
