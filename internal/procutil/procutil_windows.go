//go:build windows

// Package procutil holds small helpers for starting helper processes.
package procutil

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: a console program started without a console of its own
// gets none, instead of Windows opening a new visible console window for it.
const createNoWindow = 0x08000000

// Hide makes cmd start without a visible console window and returns it. Every short-lived
// helper RUNTIDE starts (git, docker, wsl, tool version probes, health-check commands) must
// go through it: the background engine has no console, so without this flag Windows opens
// a new console window for each helper, which looks like malware to the user.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}
