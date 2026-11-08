//go:build !windows

// Package procutil holds small helpers for starting helper processes.
package procutil

import "os/exec"

// Hide is a no-op outside Windows (there are no console windows to hide).
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
