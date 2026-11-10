package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/runtide/runtide/internal/api"
	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/paths"
	"golang.org/x/sys/windows"
)

// backend abstracts "in-process engine" and "running engine over loopback".
type backend interface {
	Call(method string, params, out any) error
	Close()
}

type localBackend struct{ e *app.Engine }

func (l *localBackend) Call(method string, params, out any) error {
	pb, _ := json.Marshal(params)
	res, err := l.e.Dispatch(context.Background(), method, pb)
	if err != nil {
		return err
	}
	if out == nil || res == nil {
		return nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (l *localBackend) Close() { _ = l.e.Close(false) }

type remoteBackend struct{ c *api.Client }

func (r *remoteBackend) Call(method string, params, out any) error {
	return r.c.Call(method, params, out)
}
func (r *remoteBackend) Close() {}

// connect returns a backend. If a RUNTIDE engine (desktop app or daemon) is
// running it is always used so that state is consistent. Otherwise commands
// that only inspect the machine run against a private in-process engine.
func connect() (backend, error) {
	home, err := paths.Home()
	if err != nil {
		return nil, err
	}
	if c, err := api.Connect(home); err == nil {
		return &remoteBackend{c}, nil
	}
	e, err := app.New(app.Options{Home: home})
	if err != nil {
		return nil, err
	}
	return &localBackend{e}, nil
}

// connectRunning returns a backend attached to a long-lived engine, starting
// the background engine when none is running.
func connectRunning(quiet bool) (*api.Client, error) {
	home, err := paths.Home()
	if err != nil {
		return nil, err
	}
	if c, err := api.Connect(home); err == nil {
		return c, nil
	}
	if !quiet {
		fmt.Fprintln(os.Stderr, "Starting the RUNTIDE background engine (stop it with `runtide daemon stop`)...")
	}
	if err := spawnDaemon(); err != nil {
		return nil, &exitError{exitNoEngine, "could not start the background engine: " + err.Error()}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := api.Connect(home); err == nil {
			return c, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, &exitError{exitNoEngine, "the background engine did not become ready in time"}
}

func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	start := func(flags uint32) error {
		cmd := exec.Command(exe, "daemon", "run")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	// Try to leave the caller's job object so the daemon outlives it; some
	// environments forbid breakaway, so retry without it.
	if err := start(base | windows.CREATE_BREAKAWAY_FROM_JOB); err != nil {
		return start(base)
	}
	return nil
}

func isTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}

func confirm(prompt string) (bool, error) {
	if !isTerminal() {
		return false, errors.New("confirmation required but stdin is not interactive; re-run with --yes to confirm")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}
