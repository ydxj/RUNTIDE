package execution

import (
	"strings"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/store"
)

// Limits bound what a single execution may store.
type Limits struct {
	MaxCommandOutputBytes int64 // stored output per command (default 8 MiB)
	MaxCommandLines       int   // stored lines per command (default 100000)
	MaxEvents             int64 // events per execution (default 100000)
}

func (l Limits) withDefaults() Limits {
	if l.MaxCommandOutputBytes <= 0 {
		l.MaxCommandOutputBytes = 8 << 20
	}
	if l.MaxCommandLines <= 0 {
		l.MaxCommandLines = 100000
	}
	if l.MaxEvents <= 0 {
		l.MaxEvents = 100000
	}
	return l
}

// Recorder writes the execution timeline and command output, applies
// redaction, enforces storage bounds and fans live data out to subscribers.
type Recorder struct {
	St     *store.Store
	Red    *redact.Redactor
	Limits Limits
	mu     sync.Mutex
	pub    func(topic string, data any)
	counts map[string]int64
}

// NewRecorder creates a recorder. pub may be nil.
func NewRecorder(st *store.Store, red *redact.Redactor, pub func(string, any)) *Recorder {
	if red == nil {
		red = redact.New()
	}
	return &Recorder{St: st, Red: red, Limits: Limits{}.withDefaults(), pub: pub, counts: map[string]int64{}}
}

// Event appends an event. Failures to persist are returned to the caller:
// evidence is never silently dropped.
func (r *Recorder) Event(execID, typ, source, sev, subject string, data map[string]any) (*domain.ExecEvent, error) {
	r.mu.Lock()
	n := r.counts[execID] + 1
	r.counts[execID] = n
	max := r.Limits.withDefaults().MaxEvents
	r.mu.Unlock()
	if n > max {
		if n == max+1 { // say so exactly once
			_ = r.St.AppendEvent(&domain.ExecEvent{ExecutionID: execID, Type: domain.EvNote, Source: domain.SourceObserved, Severity: domain.SevWarn,
				Subject: "recorder", Data: map[string]any{"message": "event limit reached; further events are not stored", "limit": max}})
		}
		return nil, nil
	}
	e := &domain.ExecEvent{ExecutionID: execID, Type: typ, Source: source, Severity: sev, Subject: subject, Data: redactMap(r.Red, data)}
	if err := r.St.AppendEvent(e); err != nil {
		return nil, err
	}
	if r.pub != nil {
		r.pub("exec.event", e)
	}
	return e, nil
}

func redactMap(red *redact.Redactor, m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = red.Line(x)
		case []string:
			cp := make([]string, len(x))
			for i, s := range x {
				cp[i] = red.Line(s)
			}
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// CmdRec records one command's output into the store.
type CmdRec struct {
	r       *Recorder
	Cmd     *domain.Command
	mu      sync.Mutex
	buf     []domain.OutputLine
	stored  int64
	lines   int
	tail    []string
	tailLen int
	done    chan struct{}
	closed  bool
}

const tailBytes = 64 * 1024

// StartCommand stores the command (arguments redacted) and returns its recorder.
func (r *Recorder) StartCommand(c *domain.Command) (*CmdRec, error) {
	var red bool
	c.Args, red = redact.Args(r.Red, c.Args)
	c.Redacted = red
	if c.StartedAt.IsZero() {
		c.StartedAt = time.Now()
	}
	if err := r.St.InsertCommand(c); err != nil {
		return nil, err
	}
	cr := &CmdRec{r: r, Cmd: c, done: make(chan struct{})}
	go cr.flusher()
	return cr, nil
}

func (c *CmdRec) flusher() {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.flush()
		case <-c.done:
			return
		}
	}
}

func (c *CmdRec) flush() {
	c.mu.Lock()
	batch := c.buf
	c.buf = nil
	c.mu.Unlock()
	if len(batch) > 0 {
		_ = c.r.St.AppendOutput(store.OutputBatch{CommandID: c.Cmd.ID, Lines: batch})
	}
}

// Line implements capsule.Sink. Output beyond the bounds is counted but not stored.
func (c *CmdRec) Line(stream, line string) {
	line = c.r.Red.Line(line)
	now := time.Now()
	c.mu.Lock()
	size := int64(len(line)) + 1
	if stream == "stderr" {
		c.Cmd.StderrBytes += size
	} else {
		c.Cmd.StdoutBytes += size
	}
	lim := c.r.Limits.withDefaults()
	if c.stored+size > lim.MaxCommandOutputBytes || c.lines >= lim.MaxCommandLines {
		c.Cmd.Truncated = true
	} else {
		c.stored += size
		c.lines++
		c.buf = append(c.buf, domain.OutputLine{CommandID: c.Cmd.ID, Time: now, Stream: stream, Line: line})
	}
	c.tail = append(c.tail, line)
	c.tailLen += len(line) + 1
	for c.tailLen > tailBytes && len(c.tail) > 1 {
		c.tailLen -= len(c.tail[0]) + 1
		c.tail = c.tail[1:]
	}
	flushNow := len(c.buf) >= 200
	c.mu.Unlock()
	if c.r.pub != nil {
		c.r.pub("exec.output", map[string]any{"executionId": c.Cmd.ExecutionID, "commandId": c.Cmd.ID, "name": c.Cmd.Name, "stream": stream, "line": line, "time": now})
	}
	if flushNow {
		c.flush()
	}
}

// Started implements capsule.Sink.
func (c *CmdRec) Started(pid int, startNs int64) {
	c.Cmd.PID = pid
	_ = c.r.St.SetCommandPID(c.Cmd.ID, pid)
}

// Tail returns the last ~64 KiB of output (what verification reports).
func (c *CmdRec) Tail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.tail, "\n")
}

// Finish stores the outcome and flushes remaining output.
func (c *CmdRec) Finish(exit int, cancelled, timedOut bool, runErr error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	close(c.done)
	c.flush()
	end := time.Now()
	c.Cmd.EndedAt = &end
	if runErr == nil {
		c.Cmd.ExitCode = &exit
	}
	c.Cmd.Cancelled, c.Cmd.TimedOut = cancelled, timedOut
	if runErr != nil {
		c.Cmd.Error = c.r.Red.Line(runErr.Error())
	}
	_ = c.r.St.FinishCommand(c.Cmd)
}
