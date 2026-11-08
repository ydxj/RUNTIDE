//go:build windows

package winapi

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var (
	procAttachConsole         = modKernel32.NewProc("AttachConsole")
	procFreeConsole           = modKernel32.NewProc("FreeConsole")
	procSetConsoleCtrlHandler = modKernel32.NewProc("SetConsoleCtrlHandler")
	procNtResumeProcess       = modNtdll.NewProc("NtResumeProcess")

	consoleMu sync.Mutex
)

// GracefulInterrupt asks the console processes sharing pid's (hidden) console
// to stop by delivering CTRL_BREAK. Services are launched with their own
// hidden console, so the event only reaches that service's processes.
//
// The technique temporarily detaches the calling process from its own console
// and attaches to the target's; it is serialised by a mutex. It returns an
// error when the target has no console, in which case callers fall back to
// forceful termination after their grace period.
func GracefulInterrupt(pid uint32) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	procFreeConsole.Call()
	r, _, err := procAttachConsole.Call(uintptr(pid))
	if r == 0 {
		return fmt.Errorf("attach to console of pid %d: %w", pid, err)
	}
	// Make sure we do not receive our own event.
	procSetConsoleCtrlHandler.Call(0, 1)
	r2, _, err2 := procGenerateConsoleCtrlEvent.Call(uintptr(windows.CTRL_BREAK_EVENT), 0)
	time.Sleep(100 * time.Millisecond) // let the event be delivered before detaching
	procFreeConsole.Call()
	procSetConsoleCtrlHandler.Call(0, 0)
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
