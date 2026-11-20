package execution

import (
	"fmt"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"github.com/runtide/runtide/internal/store"
)

// Recover repairs state after RUNTIDE stopped unexpectedly: executions that
// were in flight are marked INTERRUPTED (their evidence is kept and can still
// be exported) and the workspace becomes INTERRUPTED. Processes that survived
// are only *reported* (see Orphans); nothing is terminated here.
func (m *Manager) Recover() (interrupted []string, err error) {
	live := []domain.State{domain.StateConfiguring, domain.StateReady, domain.StateRunning, domain.StateVerifying}
	xs, err := m.St.ListExecutions(store.ExecFilter{States: live, Limit: 1000})
	if err != nil {
		return nil, err
	}
	for _, x := range xs {
		if m.IsActive(x.ID) {
			continue
		}
		if err := m.St.TransitionExecution(x.ID, domain.StateInterrupted); err != nil {
			continue
		}
		_ = m.St.SetExecutionError(x.ID, "RUNTIDE stopped while this execution was running")
		if ws, err := m.St.GetWorkspaceRow(x.WorkspaceID); err == nil && domain.CanTransitionWorkspace(ws.State, domain.StateInterrupted) {
			_ = m.St.TransitionWorkspace(ws.ID, domain.StateInterrupted, "interrupted")
		}
		m.ev(x.ID, domain.EvExecutionInterrupted, domain.SevError, "", map[string]any{"reason": "RUNTIDE was not running when this execution ended; commands and services it launched may not have been stopped cleanly"})
		interrupted = append(interrupted, x.ID)
	}
	return interrupted, nil
}

// Orphans lists recorded managed processes that never ended. Verified means a
// process with that pid exists and started at the recorded time, so it is
// almost certainly the process RUNTIDE launched (not a reused pid).
func (m *Manager) Orphans() ([]Orphan, error) {
	open, err := m.St.OpenManagedProcesses()
	if err != nil {
		return nil, err
	}
	procs, _ := winapi.ListProcesses(false)
	byPID := map[uint32]winapi.ProcessInfo{}
	for _, p := range procs {
		byPID[uint32(p.PID)] = p
	}
	var out []Orphan
	for _, mp := range open {
		if m.IsActive(mp.ExecutionID) {
			continue
		}
		o := Orphan{ExecutionID: mp.ExecutionID, CommandID: mp.CommandID, PID: mp.PID, Exe: mp.Exe, StartedAt: mp.StartedAt, rowID: mp.ID, startNs: mp.StartTimeNs}
		p, alive := byPID[uint32(mp.PID)]
		if !alive {
			_ = m.St.EndManagedProcess(mp.ID) // gone: just close the record
			continue
		}
		if mp.StartTimeNs != 0 {
			d := p.StartTime.UnixNano() - mp.StartTimeNs
			o.Verified = d > -int64(2*time.Second) && d < int64(2*time.Second)
		}
		out = append(out, o)
	}
	return out, nil
}

// CleanupOrphans terminates orphans that are verified to be RUNTIDE's own
// (pid + creation time match) and, with all=false, only those of execID. The
// caller must have obtained confirmation. Unverified entries are never touched.
func (m *Manager) CleanupOrphans(execID string) (killed, skipped int, err error) {
	orphans, err := m.Orphans()
	if err != nil {
		return 0, 0, err
	}
	for _, o := range orphans {
		if execID != "" && !strings.HasPrefix(o.ExecutionID, execID) {
			continue
		}
		if !o.Verified {
			skipped++
			continue
		}
		res, kerr := winapi.TerminateProcess(uint32(o.PID), time.Unix(0, o.startNs))
		if kerr != nil && !res.Terminated {
			skipped++
			m.ev(o.ExecutionID, domain.EvNote, domain.SevWarn, "cleanup", map[string]any{"message": fmt.Sprintf("could not terminate orphaned process %d: %s", o.PID, res.Detail)})
			continue
		}
		_ = m.St.EndManagedProcess(o.rowID)
		m.ev(o.ExecutionID, domain.EvProcessTerminated, domain.SevWarn, "cleanup", map[string]any{"pid": o.PID, "reason": "orphan cleanup after RUNTIDE restart"})
		killed++
	}
	return killed, skipped, nil
}
