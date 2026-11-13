// Command runtide is the RUNTIDE command-line interface. It uses the same
// engine as the desktop application; commands that need long-lived processes
// talk to a background engine (started on demand) so services keep running
// after the CLI exits.
package main

import (
	"errors"
	"fmt"
	"os"
)

// exitError carries a specific process exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// Exit codes (documented in docs/CLI_REFERENCE.md).
const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitFindings = 3 // doctor found errors
	exitNoEngine = 4
)

func main() {
	cmd := newRoot()
	if err := cmd.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, "runtide:", ee.msg)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "runtide:", err)
		os.Exit(exitFailure)
	}
}
