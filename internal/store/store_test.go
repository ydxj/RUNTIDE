package store

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db, _, err := database.Open(filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return New(db.DB)
}

func seed(t *testing.T, s *Store) (*domain.Project, *domain.Workspace, *domain.Execution) {
	p := &domain.Project{Name: "shop", Root: `C:\shop`}
	require.NoError(t, s.CreateProject(p))
	w := &domain.Workspace{ProjectID: p.ID, Name: "auth", Task: "Implement auth", Isolation: domain.IsolationManaged, Dir: `C:\shop`, SourceDir: `C:\shop`, ConfigYAML: "version: 1"}
	require.NoError(t, s.CreateWorkspace(w))
	e := &domain.Execution{WorkspaceID: w.ID, ProjectID: p.ID, Task: w.Task, Isolation: domain.IsolationManaged}
	require.NoError(t, s.CreateExecution(e))
	return p, w, e
}

func TestProjectsAndWorkspaces(t *testing.T) {
	s := newStore(t)
	p, w, _ := seed(t, s)
	assert.Error(t, s.CreateProject(&domain.Project{Name: "shop", Root: "x"}), "duplicate name")
	got, err := s.GetProject("shop")
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)
	assert.Error(t, s.CreateWorkspace(&domain.Workspace{ProjectID: p.ID, Name: "auth", Isolation: 1, Dir: "d", SourceDir: "s", ConfigYAML: "y"}), "duplicate in project")
	f, err := s.FindWorkspace("shop/auth")
	require.NoError(t, err)
	assert.Equal(t, w.ID, f.ID)
	f, err = s.FindWorkspace("auth")
	require.NoError(t, err)
	assert.Equal(t, w.ID, f.ID)
	_, err = s.FindWorkspace("missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStateTransitionsAreValidated(t *testing.T) {
	s := newStore(t)
	_, w, e := seed(t, s)
	var te *domain.TransitionError
	assert.ErrorAs(t, s.TransitionExecution(e.ID, domain.StateRunning), &te, "CREATED -> RUNNING is impossible")
	for _, st := range []domain.State{domain.StateConfiguring, domain.StateReady, domain.StateRunning, domain.StateVerifying, domain.StateCompleted} {
		require.NoError(t, s.TransitionExecution(e.ID, st), st)
	}
	got, _ := s.GetExecution(e.ID)
	assert.Equal(t, domain.StateCompleted, got.State)
	assert.NotNil(t, got.StartedAt)
	assert.NotNil(t, got.EndedAt)
	assert.ErrorAs(t, s.TransitionExecution(e.ID, domain.StateRunning), &te, "terminal state has no exits")
	assert.ErrorIs(t, s.TransitionExecution("nope", domain.StateReady), ErrNotFound)
	// Workspace: reopen allowed after completion.
	for _, st := range []domain.State{domain.StateConfiguring, domain.StateReady, domain.StateRunning, domain.StateCompleted, domain.StateReady} {
		require.NoError(t, s.TransitionWorkspace(w.ID, st, ""), st)
	}
	assert.Error(t, s.DeleteWorkspace(w.ID), "evidence is kept")
}

func TestEventsGetGaplessSequenceUnderConcurrency(t *testing.T) {
	s := newStore(t)
	_, _, e := seed(t, s)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, s.AppendEvent(&domain.ExecEvent{ExecutionID: e.ID, Type: domain.EvNote, Subject: "x", Data: map[string]any{"n": 1}}))
		}()
	}
	wg.Wait()
	evs, err := s.ListEvents(e.ID, EventFilter{})
	require.NoError(t, err)
	require.Len(t, evs, 40)
	for i, ev := range evs {
		assert.Equal(t, int64(i+1), ev.Seq)
	}
	require.NoError(t, s.AppendEvent(&domain.ExecEvent{ExecutionID: e.ID, Type: domain.EvCommandStarted, Subject: "build", Severity: domain.SevError}))
	f, _ := s.ListEvents(e.ID, EventFilter{Types: []string{domain.EvCommandStarted}})
	assert.Len(t, f, 1)
	f, _ = s.ListEvents(e.ID, EventFilter{Severity: "error"})
	assert.Len(t, f, 1)
	f, _ = s.ListEvents(e.ID, EventFilter{Search: "100%_"})
	assert.Empty(t, f, "LIKE wildcards escaped")
	f, _ = s.ListEvents(e.ID, EventFilter{AfterSeq: 40})
	assert.Len(t, f, 1)
}

func TestCommandsOutputAndOrphanTable(t *testing.T) {
	s := newStore(t)
	_, _, e := seed(t, s)
	c := &domain.Command{ExecutionID: e.ID, Kind: "adhoc", Name: "go-test", Executable: "go", Args: []string{"test", "./..."}, Cwd: `C:\shop`, EnvKeys: []string{"PORT"}, Isolation: 1, StartedAt: time.Now()}
	require.NoError(t, s.InsertCommand(c))
	assert.Equal(t, 1, c.Seq)
	code := 3
	end := time.Now()
	c.ExitCode, c.EndedAt, c.StdoutBytes, c.Truncated = &code, &end, 42, true
	require.NoError(t, s.FinishCommand(c))
	got, err := s.GetCommand(c.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"test", "./..."}, got.Args)
	assert.Equal(t, 3, *got.ExitCode)
	assert.True(t, got.Truncated)

	require.NoError(t, s.AppendOutput(OutputBatch{CommandID: c.ID, Lines: []domain.OutputLine{{Time: time.Now(), Stream: "stdout", Line: "a"}, {Time: time.Now(), Stream: "stderr", Line: "b"}}}))
	lines, _ := s.ListOutput(c.ID, 0, 10)
	require.Len(t, lines, 2)
	assert.Equal(t, "stderr", lines[1].Stream)
	lines, _ = s.ListOutput(c.ID, 1, 10)
	assert.Len(t, lines, 1)

	id, err := s.AddManagedProcess(ManagedProcess{ExecutionID: e.ID, CommandID: c.ID, PID: 123, StartTimeNs: 5, Exe: "go.exe"})
	require.NoError(t, err)
	open, _ := s.OpenManagedProcesses()
	require.Len(t, open, 1)
	require.NoError(t, s.EndManagedProcess(id))
	open, _ = s.OpenManagedProcesses()
	assert.Empty(t, open)
}

func TestVerificationFilesReceipts(t *testing.T) {
	s := newStore(t)
	_, _, e := seed(t, s)
	now := time.Now()
	code := 1
	run, err := s.SaveVerification(e.ID, []domain.VerificationResult{
		{Seq: 1, Name: "unit", Type: "command", Status: domain.VFailed, Required: true, StartedAt: now, EndedAt: now.Add(time.Second), ExitCode: &code, Needs: nil},
		{Seq: 2, Name: "e2e", Type: "command", Status: domain.VSkipped, Required: true, StartedAt: now, EndedAt: now, Needs: []string{"unit"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, run)
	run, _ = s.SaveVerification(e.ID, []domain.VerificationResult{{Seq: 1, Name: "unit", Type: "command", Status: domain.VPassed, StartedAt: now, EndedAt: now}})
	assert.Equal(t, 2, run)
	latest, _ := s.ListVerification(e.ID, false)
	require.Len(t, latest, 1)
	assert.Equal(t, domain.VPassed, latest[0].Status)
	all, _ := s.ListVerification(e.ID, true)
	assert.Len(t, all, 3)

	require.NoError(t, s.ReplaceFileChanges(e.ID, []domain.FileChange{{Path: "a.go", Change: "added", SizeAfter: 5}, {Path: "b.go", Change: "deleted"}}))
	require.NoError(t, s.ReplaceFileChanges(e.ID, []domain.FileChange{{Path: "a.go", Change: "modified"}}))
	fc, _ := s.ListFileChanges(e.ID)
	assert.Len(t, fc, 1)
	require.NoError(t, s.PutBaseline(e.ID, "a.go", []byte("old")))
	b, err := s.GetBaseline(e.ID, "a.go")
	require.NoError(t, err)
	assert.Equal(t, "old", string(b))

	r := &ReceiptRecord{SHA256: "abcdef0123456789", Path: "x.zip", Size: 1, ManifestJSON: "{}"}
	require.NoError(t, s.AddReceipt(r))
	r2 := &ReceiptRecord{SHA256: "abcdef0123456789", Path: "y.zip", Size: 1, ManifestJSON: "{}"}
	require.NoError(t, s.AddReceipt(r2))
	assert.Equal(t, r.ID, r2.ID, "identical receipt is not duplicated")
	got, err := s.GetReceipt("abcdef01")
	require.NoError(t, err)
	assert.Equal(t, r.ID, got.ID)
}

func TestPruneExecutionsOnlyRemovesOldEndedOnesWithoutLiveProcesses(t *testing.T) {
	s := newStore(t)
	_, w, old := seed(t, s)
	p2 := w.ProjectID
	mk := func() *domain.Execution {
		e := &domain.Execution{WorkspaceID: w.ID, ProjectID: p2, Task: "t", Isolation: domain.IsolationManaged}
		require.NoError(t, s.CreateExecution(e))
		return e
	}
	running, withProc := mk(), mk()
	for _, e := range []*domain.Execution{old, running, withProc} {
		require.NoError(t, s.TransitionExecution(e.ID, domain.StateConfiguring))
		require.NoError(t, s.TransitionExecution(e.ID, domain.StateReady))
		require.NoError(t, s.TransitionExecution(e.ID, domain.StateRunning))
	}
	require.NoError(t, s.TransitionExecution(old.ID, domain.StateFailed))
	require.NoError(t, s.TransitionExecution(withProc.ID, domain.StateInterrupted))
	c := &domain.Command{ExecutionID: withProc.ID, Kind: "service", Name: "api", Executable: "x", Isolation: 1, StartedAt: time.Now()}
	require.NoError(t, s.InsertCommand(c))
	_, err := s.AddManagedProcess(ManagedProcess{ExecutionID: withProc.ID, CommandID: c.ID, PID: 9, StartTimeNs: 1, Exe: "x"})
	require.NoError(t, err)
	_, err = s.DB.Exec(`UPDATE executions SET ended_at=? WHERE id IN (?,?)`, time.Now().Add(-100*24*time.Hour).Unix(), old.ID, withProc.ID)
	require.NoError(t, err)

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	n, err := s.PruneExecutions(cutoff, true)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "dry run counts only the old ended execution without a live process")
	_, err = s.GetExecution(old.ID)
	require.NoError(t, err, "dry run deletes nothing")

	n, err = s.PruneExecutions(cutoff, false)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	_, err = s.GetExecution(old.ID)
	assert.Error(t, err)
	_, err = s.GetExecution(running.ID)
	assert.NoError(t, err, "running executions are never pruned")
	_, err = s.GetExecution(withProc.ID)
	assert.NoError(t, err, "an execution with a live recorded process is kept for orphan handling")
}

func TestPurgePhantomReceiptsOnlyRemovesComparisonLeftovers(t *testing.T) {
	s := newStore(t)
	add := func(path, sha string, imported bool) {
		require.NoError(t, s.AddReceipt(&ReceiptRecord{ExecutionID: "ex", ProjectName: "p", Path: path, SHA256: sha, Size: 1, Imported: imported, Status: "COMPLETED", ManifestJSON: "{}"}))
	}
	add(`C:\Users\u\AppData\Local\Runtide\backups\cmp-123\ex_1.runtide.zip`, "a1", false)
	add(`C:\Users\u\AppData\Local\Runtide\backups\cmp-456\ex_2.runtide.zip`, "a2", false)
	add(`C:\work\shop\build.runtide.zip`, "b1", false)
	add(`C:\Users\u\AppData\Local\Runtide\backups\cmp-789\imported.runtide.zip`, "c1", true)
	n, err := s.PurgePhantomReceipts(`C:\Users\u\AppData\Local\Runtide\backups`)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
	left, err := s.ListReceipts()
	require.NoError(t, err)
	assert.Len(t, left, 2, "real exports and imports are untouched")
}
