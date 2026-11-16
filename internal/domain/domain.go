// Package domain defines RUNTIDE's execution-platform model: projects,
// task workspaces, executions, events, commands and verification results,
// plus the validated lifecycle state machine. It has no I/O.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID returns a sortable, collision-resistant identifier such as
// "ex_018c3f2a91b4a1b2c3d4e5f6" (48-bit millisecond time + 48 random bits).
func NewID(prefix string) string {
	var b [12]byte
	ms := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (8 * (5 - i)))
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic(fmt.Sprintf("entropy source failed: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// ID prefixes.
const (
	PrefixProject   = "pr"
	PrefixWorkspace = "ws"
	PrefixExecution = "ex"
	PrefixEvent     = "ev"
	PrefixCommand   = "cm"
	PrefixReceipt   = "rc"
)

// State is the lifecycle state of a workspace or an execution.
type State string

const (
	StateCreated           State = "CREATED"
	StateConfiguring       State = "CONFIGURING"
	StateReady             State = "READY"
	StateRunning           State = "RUNNING"
	StateVerifying         State = "VERIFYING"
	StateCompleted         State = "COMPLETED"
	StateFailed            State = "FAILED"
	StateCancelled         State = "CANCELLED"
	StateInterrupted       State = "INTERRUPTED"
	StateRequiresAttention State = "REQUIRES_ATTENTION"
)

// AllStates lists every state.
var AllStates = []State{StateCreated, StateConfiguring, StateReady, StateRunning, StateVerifying,
	StateCompleted, StateFailed, StateCancelled, StateInterrupted, StateRequiresAttention}

// Valid reports whether s is a known state.
func (s State) Valid() bool {
	for _, x := range AllStates {
		if x == s {
			return true
		}
	}
	return false
}

// Terminal reports whether the state ends an execution (workspaces may be reopened, see CanTransition).
func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateInterrupted, StateRequiresAttention:
		return true
	}
	return false
}

var execTransitions = map[State][]State{
	StateCreated:     {StateConfiguring, StateCancelled},
	StateConfiguring: {StateReady, StateFailed, StateCancelled},
	StateReady:       {StateRunning, StateConfiguring, StateCancelled},
	// A run without a verification pipeline may complete directly.
	StateRunning:   {StateVerifying, StateCompleted, StateFailed, StateCancelled, StateInterrupted, StateRequiresAttention},
	StateVerifying: {StateCompleted, StateFailed, StateCancelled, StateInterrupted, StateRequiresAttention},
}

// Workspaces can additionally be reopened after they ended so a task can be
// run again; every reopen is recorded as an event by the caller.
var workspaceExtra = map[State][]State{
	StateCompleted:         {StateReady},
	StateFailed:            {StateReady, StateCancelled},
	StateInterrupted:       {StateReady, StateCancelled, StateFailed},
	StateRequiresAttention: {StateReady, StateCancelled, StateFailed},
}

// CanTransitionExecution validates an execution state change.
func CanTransitionExecution(from, to State) bool { return in(execTransitions[from], to) }

// CanTransitionWorkspace validates a workspace state change.
func CanTransitionWorkspace(from, to State) bool {
	return in(execTransitions[from], to) || in(workspaceExtra[from], to)
}

// TransitionError describes a rejected transition.
type TransitionError struct{ From, To State }

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid state transition %s -> %s", e.From, e.To)
}

func in(l []State, s State) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// Isolation levels. They describe what RUNTIDE actually enforces; see docs/EXECUTION_CAPSULES.md.
type Isolation int

const (
	IsolationManaged  Isolation = 1 // native managed process (Job Object); no filesystem or network isolation
	IsolationWorktree Isolation = 2 // separate working directory (optionally a git worktree); processes can still reach the whole user profile
	IsolationDocker   Isolation = 3 // container with an explicit mount list
	IsolationWSL      Isolation = 4 // execution inside a WSL2 distribution; not a security boundary
)

// Valid reports whether the level is known.
func (i Isolation) Valid() bool { return i >= 1 && i <= 4 }

// Name is the configuration spelling.
func (i Isolation) Name() string {
	switch i {
	case IsolationManaged:
		return "managed"
	case IsolationWorktree:
		return "worktree"
	case IsolationDocker:
		return "docker"
	case IsolationWSL:
		return "wsl"
	}
	return "unknown"
}

// ParseIsolation converts a configuration name.
func ParseIsolation(s string) (Isolation, error) {
	switch s {
	case "", "managed", "1":
		return IsolationManaged, nil
	case "worktree", "filesystem", "2":
		return IsolationWorktree, nil
	case "docker", "container", "3":
		return IsolationDocker, nil
	case "wsl", "wsl2", "4":
		return IsolationWSL, nil
	}
	return 0, fmt.Errorf("unknown isolation %q (managed, worktree, docker, wsl)", s)
}

// Project is a software project RUNTIDE executes tasks for.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Root        string    `json:"root"`
	Description string    `json:"description"`
	GitRoot     string    `json:"gitRoot,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Workspace is the environment of one task.
type Workspace struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Task        string    `json:"task"`
	State       State     `json:"state"`
	Isolation   Isolation `json:"isolation"`
	Dir         string    `json:"dir"`       // working directory commands run in
	SourceDir   string    `json:"sourceDir"` // the project directory it was derived from
	Worktree    bool      `json:"worktree"`
	Branch      string    `json:"branch,omitempty"`
	ConfigYAML  string    `json:"configYaml"`
	Approved    bool      `json:"approved"` // current config hash has been approved for execution
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	FinalStatus string    `json:"finalStatus,omitempty"`
}

// Execution is one run of a task inside a workspace.
type Execution struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspaceId"`
	ProjectID      string     `json:"projectId"`
	Task           string     `json:"task"`
	State          State      `json:"state"`
	Isolation      Isolation  `json:"isolation"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	RuntideVersion string     `json:"runtideVersion"`
	Error          string     `json:"error,omitempty"`
	Verification   string     `json:"verification,omitempty"` // passed | failed | none
	Environment    []byte     `json:"-"`
}

// Event provenance.
const (
	SourceObserved = "observed" // directly observed by RUNTIDE
	SourceReported = "reported" // reported by an integrated tool or agent
	SourceInferred = "inferred" // derived from other evidence
)

// Severities.
const (
	SevInfo  = "info"
	SevWarn  = "warn"
	SevError = "error"
)

// Event types recorded by the recorder.
const (
	EvExecutionCreated     = "execution.created"
	EvWorkspacePrepared    = "workspace.prepared"
	EvStateChanged         = "state.changed"
	EvCommandStarted       = "command.started"
	EvCommandFinished      = "command.finished"
	EvProcessStarted       = "process.started"
	EvProcessTerminated    = "process.terminated"
	EvServiceHealthy       = "service.healthy"
	EvServiceUnhealthy     = "service.unhealthy"
	EvFilesChanged         = "files.changed"
	EvTestStarted          = "test.started"
	EvTestCompleted        = "test.completed"
	EvVerificationPassed   = "verification.passed"
	EvVerificationFailed   = "verification.failed"
	EvExecutionCancelled   = "execution.cancelled"
	EvExecutionCompleted   = "execution.completed"
	EvExecutionInterrupted = "execution.interrupted"
	EvNote                 = "note"
)

// ExecEvent is one entry in the execution timeline.
type ExecEvent struct {
	ID          string         `json:"id"`
	ExecutionID string         `json:"executionId"`
	Seq         int64          `json:"seq"`
	Time        time.Time      `json:"time"`
	Type        string         `json:"type"`
	Source      string         `json:"source"`
	Severity    string         `json:"severity"`
	Subject     string         `json:"subject,omitempty"` // command/service/check name
	Data        map[string]any `json:"data,omitempty"`
}

// Command is one process RUNTIDE launched for an execution.
type Command struct {
	ID          string     `json:"id"`
	ExecutionID string     `json:"executionId"`
	Seq         int        `json:"seq"`
	Kind        string     `json:"kind"` // setup | start | service | verify | adhoc
	Name        string     `json:"name"`
	Executable  string     `json:"executable"`
	Args        []string   `json:"args"` // redacted
	Cwd         string     `json:"cwd"`
	EnvKeys     []string   `json:"envKeys"` // names only, never values
	Isolation   Isolation  `json:"isolation"`
	StartedAt   time.Time  `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	PID         int        `json:"pid,omitempty"`
	Cancelled   bool       `json:"cancelled"`
	TimedOut    bool       `json:"timedOut"`
	Truncated   bool       `json:"truncated"` // output exceeded the storage bound
	Redacted    bool       `json:"redacted"`  // at least one argument was masked: not replayable verbatim
	StdoutBytes int64      `json:"stdoutBytes"`
	StderrBytes int64      `json:"stderrBytes"`
	Error       string     `json:"error,omitempty"`
}

// OutputLine is stored command output.
type OutputLine struct {
	ID        int64     `json:"id"`
	CommandID string    `json:"commandId"`
	Time      time.Time `json:"time"`
	Stream    string    `json:"stream"` // stdout | stderr
	Line      string    `json:"line"`
}

// Verification statuses.
const (
	VPassed        = "passed"
	VFailed        = "failed"
	VSkipped       = "skipped"
	VTimedOut      = "timed_out"
	VNotApplicable = "not_applicable"
)

// VerificationResult is the outcome of one check.
type VerificationResult struct {
	Seq         int       `json:"seq"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Required    bool      `json:"required"`
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt"`
	DurationMs  int64     `json:"durationMs"`
	ExitCode    *int      `json:"exitCode,omitempty"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	Needs       []string  `json:"needs,omitempty"`
	Evidence    []string  `json:"evidence,omitempty"` // references: command ids, urls, file paths
}

// FileChange describes one changed path in a workspace.
type FileChange struct {
	Path       string `json:"path"`
	Change     string `json:"change"` // added | modified | deleted | renamed
	OldPath    string `json:"oldPath,omitempty"`
	SizeBefore int64  `json:"sizeBefore"`
	SizeAfter  int64  `json:"sizeAfter"`
	HashBefore string `json:"hashBefore,omitempty"`
	HashAfter  string `json:"hashAfter,omitempty"`
	ModTime    string `json:"modTime,omitempty"`
}

// AuditEvent records a security-relevant action.
type AuditEvent struct {
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Subject string    `json:"subject"`
	Detail  string    `json:"detail,omitempty"`
}
