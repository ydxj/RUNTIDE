// Package capsule runs one command inside an isolation level and reports what
// happened. Level 1 and 2 execute natively inside a Windows Job Object (the
// whole process tree is killed on cancel, timeout or RUNTIDE exit); levels 3
// and 4 wrap the command for Docker or WSL2 and run that wrapper the same way.
//
// Honest scope: a Job Object does not isolate files or network. See
// docs/EXECUTION_CAPSULES.md for exactly what each level does and does not do.
package capsule

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	winapi "github.com/runtide/runtide/internal/platform/windows"
	"golang.org/x/sys/windows"
)

// Spec describes one command.
type Spec struct {
	Executable string
	Args       []string
	Dir        string
	Env        map[string]string // explicit variables added to the (filtered) inherited environment
	InheritAll bool              // pass the whole parent environment (default: a safe allow-list only)
	Timeout    time.Duration     // 0 = none
}

// Sink receives output and lifecycle notifications.
type Sink interface {
	Line(stream, line string)
	Started(pid int, startTimeNs int64)
}

// Result is the outcome of a run.
type Result struct {
	ExitCode  int
	Cancelled bool
	TimedOut  bool
	PID       int
	Err       error // failure to start or supervise (not a non-zero exit)
}

// safeEnv lists variables inherited by default. Everything else in the parent
// environment (API keys, tokens, cloud credentials...) is withheld.
var safeEnv = []string{
	"PATH", "PATHEXT", "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "TEMP", "TMP", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
	"APPDATA", "LOCALAPPDATA", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "COMMONPROGRAMFILES", "ALLUSERSPROFILE",
	"PUBLIC", "OS", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "COMPUTERNAME", "LANG", "LC_ALL", "TERM", "USERNAME", "USERDOMAIN",
	"GOPATH", "GOROOT", "GOCACHE", "GOMODCACHE", "GOFLAGS", "CARGO_HOME", "RUSTUP_HOME", "JAVA_HOME", "NVM_HOME", "NVM_SYMLINK", "PNPM_HOME",
}

// BuildEnv returns the process environment: allow-listed inherited variables
// plus explicit ones (explicit wins).
func BuildEnv(extra map[string]string, inheritAll bool) []string {
	allow := map[string]bool{}
	for _, k := range safeEnv {
		allow[k] = true
	}
	m := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if inheritAll || allow[strings.ToUpper(k)] {
			m[strings.ToUpper(k)+"\x00"+k] = v // keep original case, dedupe case-insensitively
		}
	}
	// Collapse case-insensitive duplicates and apply explicit values.
	final := map[string]string{}
	orig := map[string]string{}
	for key, v := range m {
		up, k, _ := strings.Cut(key, "\x00")
		final[up], orig[up] = v, k
	}
	for k, v := range extra {
		up := strings.ToUpper(k)
		final[up], orig[up] = v, k
	}
	out := make([]string, 0, len(final))
	for up, v := range final {
		out = append(out, orig[up]+"="+v)
	}
	sort.Strings(out)
	return out
}

func resolve(exe, dir string, env []string) (string, error) {
	if strings.ContainsAny(exe, `\/`) && !filepath.IsAbs(exe) {
		exe = filepath.Join(dir, exe)
	}
	p, err := exec.LookPath(exe)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return "", fmt.Errorf("executable %q not found: %w", exe, err)
	}
	if err != nil {
		return filepath.Abs(p)
	}
	return p, nil
}

// Run executes spec to completion. ctx cancellation requests a graceful stop
// (CTRL_BREAK), then terminates the whole job after a grace period.
func Run(ctx context.Context, spec Spec, sink Sink) Result {
	env := BuildEnv(spec.Env, spec.InheritAll)
	exe, err := resolve(spec.Executable, spec.Dir, env)
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	job, err := winapi.NewJob()
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	defer job.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return Result{ExitCode: -1, Err: err}
	}
	cmd := exec.Command(exe, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = outW, errW
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW, HideWindow: true}
	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		return Result{ExitCode: -1, Err: fmt.Errorf("start %s: %w", spec.Executable, err)}
	}
	outW.Close()
	errW.Close()
	pid := uint32(cmd.Process.Pid)
	if err := job.Assign(pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		outR.Close()
		errR.Close()
		return Result{ExitCode: -1, PID: int(pid), Err: err}
	}
	if err := winapi.ResumeProcess(pid); err != nil {
		_ = job.Terminate(1)
		_ = cmd.Wait()
		outR.Close()
		errR.Close()
		return Result{ExitCode: -1, PID: int(pid), Err: err}
	}
	if sink != nil {
		sink.Started(int(pid), processStartNs(pid))
	}
	var readers sync.WaitGroup
	readers.Add(2)
	pump := func(stream string, r io.ReadCloser) {
		defer readers.Done()
		defer r.Close()
		br := bufio.NewReaderSize(r, 64*1024)
		var acc []byte
		for {
			chunk, err := br.ReadSlice('\n')
			if len(acc)+len(chunk) <= 16*1024 {
				acc = append(acc, chunk...)
			} else if len(acc) < 16*1024 {
				acc = append(acc, chunk[:16*1024-len(acc)]...)
			}
			if err == nil {
				if sink != nil {
					sink.Line(stream, strings.TrimRight(string(acc), "\r\n"))
				}
				acc = acc[:0]
				continue
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if len(acc) > 0 && sink != nil {
				sink.Line(stream, strings.TrimRight(string(acc), "\r\n"))
			}
			return
		}
	}
	go pump("stdout", outR)
	go pump("stderr", errR)

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var res Result
	res.PID = int(pid)
	var timeout <-chan time.Time
	if spec.Timeout > 0 {
		t := time.NewTimer(spec.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	stop := func() {
		_ = winapi.GracefulInterrupt(pid)
		select {
		case err := <-waitDone:
			waitDone <- err
		case <-time.After(5 * time.Second):
			_ = job.Terminate(1)
		}
	}
	var werr error
	select {
	case werr = <-waitDone:
	case <-ctx.Done():
		res.Cancelled = true
		stop()
		werr = <-waitDone
	case <-timeout:
		res.TimedOut = true
		stop()
		werr = <-waitDone
	}
	// Anything the command left behind belongs to this job: clean it up.
	if n, err := job.ActiveProcesses(); err == nil && n > 0 {
		_ = job.Terminate(1)
	}
	done := make(chan struct{})
	go func() { readers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode, res.Err = -1, werr
		}
	}
	return res
}

func processStartNs(pid uint32) int64 {
	ps, err := winapi.ListProcesses(false)
	if err != nil {
		return 0
	}
	for _, p := range ps {
		if p.PID == pid {
			return p.StartTime.UnixNano()
		}
	}
	return 0
}
