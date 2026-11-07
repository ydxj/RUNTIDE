//go:build windows

package winapi

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job wraps a Windows Job Object configured to kill every member when the
// last handle closes. It is the foundation of process-tree supervision: child
// processes of a service are automatically members, so stopping the job
// stops the whole tree and nothing can leak if RUNTIDE itself dies.
type Job struct{ h windows.Handle }

// NewJob creates a kill-on-close job object.
func NewJob() (*Job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	return &Job{h: h}, nil
}

type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodUser, ThisPeriodKernel               int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
}

// Assign adds a process (by pid) to the job.
func (j *Job) Assign(pid uint32) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fmt.Errorf("open process for job assignment: %w", err)
	}
	defer windows.CloseHandle(ph)
	if err := windows.AssignProcessToJobObject(j.h, ph); err != nil {
		return fmt.Errorf("assign to job: %w", err)
	}
	return nil
}

// Terminate kills every process in the job.
func (j *Job) Terminate(code uint32) error {
	return windows.TerminateJobObject(j.h, code)
}

// ActiveProcesses returns how many processes are still alive in the job.
func (j *Job) ActiveProcesses() (uint32, error) {
	var info jobAccounting
	if err := windows.QueryInformationJobObject(j.h, 1,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return 0, err
	}
	return info.ActiveProcesses, nil
}

// Close releases the handle (killing remaining members).
func (j *Job) Close() error {
	if j.h == 0 {
		return nil
	}
	err := windows.CloseHandle(j.h)
	j.h = 0
	return err
}
