// Package store persists the execution-platform domain in SQLite. State
// transitions are validated against the domain state machine inside the same
// transaction that applies them, so concurrent writers cannot skip a state.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/domain"
)

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a database handle.
type Store struct {
	DB *sql.DB
	mu sync.Mutex // serialises event sequence allocation
}

// New returns a store over db (which must be migrated to schema v2+).
func New(db *sql.DB) *Store { return &Store{DB: db} }

func ms(t time.Time) int64 { return t.UnixMilli() }
func fromMs(v int64) time.Time {
	return time.UnixMilli(v)
}
func nullMs(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
func ptrTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- projects ----

// CreateProject inserts a project.
func (s *Store) CreateProject(p *domain.Project) error {
	if p.ID == "" {
		p.ID = domain.NewID(domain.PrefixProject)
	}
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.DB.Exec(`INSERT INTO projects(id,name,root,description,git_root,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Root, p.Description, p.GitRoot, ms(now), ms(now))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("project %q already exists", p.Name)
	}
	return err
}

func scanProject(r interface{ Scan(...any) error }) (*domain.Project, error) {
	var p domain.Project
	var c, u int64
	var arch sql.NullInt64
	var settings string
	if err := r.Scan(&p.ID, &p.Name, &p.Root, &p.Description, &p.GitRoot, &c, &u, &arch, &settings); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromMs(c), fromMs(u)
	if arch.Valid {
		t := fromMs(arch.Int64)
		p.Archived, p.ArchivedAt = true, &t
	}
	_ = json.Unmarshal([]byte(settings), &p.Settings)
	return &p, nil
}

const projectCols = `id,name,root,description,git_root,created_at,updated_at,archived_at,settings_json`

// UpdateProject changes the description and per-project settings.
func (s *Store) UpdateProject(id, description string, st domain.ProjectSettings) error {
	b, _ := json.Marshal(st)
	res, err := s.DB.Exec(`UPDATE projects SET description=?, settings_json=?, updated_at=? WHERE id=?`, description, string(b), ms(time.Now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProjectArchived archives or restores a project. Nothing is deleted.
func (s *Store) SetProjectArchived(id string, archived bool) error {
	var v any
	if archived {
		v = ms(time.Now())
	}
	res, err := s.DB.Exec(`UPDATE projects SET archived_at=?, updated_at=? WHERE id=?`, v, ms(time.Now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveExecutionCount counts executions of a project that are not finished.
func (s *Store) ActiveExecutionCount(projectID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM executions WHERE project_id=? AND state IN ('CONFIGURING','RUNNING','VERIFYING')`, projectID).Scan(&n)
	return n, err
}

// PurgePhantomReceipts removes receipt index rows that point into the temporary
// comparison directories RUNTIDE 0.2 created inside backupDir (cmp-*). Those
// files were deleted right after the comparison, so the rows can never be opened.
// Rows for real exports and imports are never touched.
func (s *Store) PurgePhantomReceipts(backupDir string) (int64, error) {
	esc := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	dir := strings.TrimRight(backupDir, `/\`)
	var n int64
	for _, sep := range []string{`\`, "/"} {
		res, err := s.DB.Exec(`DELETE FROM receipts WHERE imported=0 AND path LIKE ? ESCAPE '!'`, esc.Replace(dir)+sep+"cmp-%")
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += c
	}
	return n, nil
}

// ---- verification profiles

func scanProfile(r interface{ Scan(...any) error }) (*domain.VerificationProfile, error) {
	var p domain.VerificationProfile
	var c, u int64
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.Pipeline, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromMs(c), fromMs(u)
	return &p, nil
}

const profileCols = `id,name,description,pipeline_yaml,created_at,updated_at`

// SaveProfile creates a profile or replaces the one with the same name.
func (s *Store) SaveProfile(p *domain.VerificationProfile) error {
	now := time.Now()
	if old, err := s.GetProfile(p.Name); err == nil {
		p.ID, p.CreatedAt = old.ID, old.CreatedAt
		_, err = s.DB.Exec(`UPDATE verification_profiles SET description=?, pipeline_yaml=?, updated_at=? WHERE id=?`, p.Description, p.Pipeline, ms(now), p.ID)
		p.UpdatedAt = now
		return err
	}
	p.ID = domain.NewID("vp")
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.DB.Exec(`INSERT INTO verification_profiles(`+profileCols+`) VALUES(?,?,?,?,?,?)`, p.ID, p.Name, p.Description, p.Pipeline, ms(now), ms(now))
	return err
}

// GetProfile finds a profile by id or name.
func (s *Store) GetProfile(ref string) (*domain.VerificationProfile, error) {
	return scanProfile(s.DB.QueryRow(`SELECT `+profileCols+` FROM verification_profiles WHERE id=? OR name=?`, ref, ref))
}

// ListProfiles returns all profiles by name.
func (s *Store) ListProfiles() ([]*domain.VerificationProfile, error) {
	rows, err := s.DB.Query(`SELECT ` + profileCols + ` FROM verification_profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.VerificationProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteProfile removes a profile (recorded verification results keep the name).
func (s *Store) DeleteProfile(ref string) error {
	res, err := s.DB.Exec(`DELETE FROM verification_profiles WHERE id=? OR name=?`, ref, ref)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetProject finds a project by id or name.
func (s *Store) GetProject(idOrName string) (*domain.Project, error) {
	return scanProject(s.DB.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id=? OR name=?`, idOrName, idOrName))
}

// ListProjects returns all projects by name.
func (s *Store) ListProjects() ([]*domain.Project, error) {
	rows, err := s.DB.Query(`SELECT ` + projectCols + ` FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- workspaces ----

const wsCols = `id,project_id,name,task,state,isolation,dir,source_dir,worktree,branch,config_yaml,approved_hash,final_status,created_at,updated_at`

// WorkspaceRow carries the stored approval hash next to the workspace.
type WorkspaceRow struct {
	domain.Workspace
	ApprovedHash string
}

func scanWS(r interface{ Scan(...any) error }) (*WorkspaceRow, error) {
	var w WorkspaceRow
	var iso, wt int
	var st string
	var c, u int64
	if err := r.Scan(&w.ID, &w.ProjectID, &w.Name, &w.Task, &st, &iso, &w.Dir, &w.SourceDir, &wt, &w.Branch, &w.ConfigYAML, &w.ApprovedHash, &w.FinalStatus, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.State, w.Isolation, w.Worktree = domain.State(st), domain.Isolation(iso), wt == 1
	w.CreatedAt, w.UpdatedAt = fromMs(c), fromMs(u)
	return &w, nil
}

// CreateWorkspace inserts a workspace in state CREATED.
func (s *Store) CreateWorkspace(w *domain.Workspace) error {
	if w.ID == "" {
		w.ID = domain.NewID(domain.PrefixWorkspace)
	}
	now := time.Now()
	w.State, w.CreatedAt, w.UpdatedAt = domain.StateCreated, now, now
	_, err := s.DB.Exec(`INSERT INTO task_workspaces(`+wsCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.ProjectID, w.Name, w.Task, string(w.State), int(w.Isolation), w.Dir, w.SourceDir, b2i(w.Worktree), w.Branch, w.ConfigYAML, "", "", ms(now), ms(now))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("workspace %q already exists in this project", w.Name)
	}
	return err
}

// GetWorkspaceRow loads a workspace by id.
func (s *Store) GetWorkspaceRow(id string) (*WorkspaceRow, error) {
	return scanWS(s.DB.QueryRow(`SELECT `+wsCols+` FROM task_workspaces WHERE id=?`, id))
}

// FindWorkspace resolves an id, or "project/name", or a unique name.
func (s *Store) FindWorkspace(ref string) (*WorkspaceRow, error) {
	if w, err := s.GetWorkspaceRow(ref); err == nil {
		return w, nil
	}
	if proj, name, ok := strings.Cut(ref, "/"); ok {
		p, err := s.GetProject(proj)
		if err != nil {
			return nil, err
		}
		return scanWS(s.DB.QueryRow(`SELECT `+wsCols+` FROM task_workspaces WHERE project_id=? AND name=?`, p.ID, name))
	}
	rows, err := s.DB.Query(`SELECT `+wsCols+` FROM task_workspaces WHERE name=?`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*WorkspaceRow
	for rows.Next() {
		w, err := scanWS(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, w)
	}
	switch len(found) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return found[0], nil
	}
	return nil, fmt.Errorf("workspace name %q is ambiguous; use project/name or the id", ref)
}

// ListWorkspaces returns workspaces (all projects when projectID is empty), newest first.
func (s *Store) ListWorkspaces(projectID string) ([]*WorkspaceRow, error) {
	q := `SELECT ` + wsCols + ` FROM task_workspaces`
	var args []any
	if projectID != "" {
		q += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	rows, err := s.DB.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkspaceRow
	for rows.Next() {
		w, err := scanWS(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// TransitionWorkspace moves a workspace to a new state if the state machine allows it.
func (s *Store) TransitionWorkspace(id string, to domain.State, finalStatus string) error {
	return s.transition("task_workspaces", id, to, domain.CanTransitionWorkspace, finalStatus, true)
}

// TransitionExecution moves an execution to a new state if the state machine allows it.
func (s *Store) TransitionExecution(id string, to domain.State) error {
	return s.transition("executions", id, to, domain.CanTransitionExecution, "", false)
}

func (s *Store) transition(table, id string, to domain.State, can func(a, b domain.State) bool, final string, isWS bool) error {
	if !to.Valid() {
		return fmt.Errorf("unknown state %q", to)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur string
	if err := tx.QueryRow(`SELECT state FROM `+table+` WHERE id=?`, id).Scan(&cur); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	from := domain.State(cur)
	if !can(from, to) {
		return &domain.TransitionError{From: from, To: to}
	}
	now := time.Now()
	if isWS {
		_, err = tx.Exec(`UPDATE task_workspaces SET state=?, final_status=CASE WHEN ?<>'' THEN ? ELSE final_status END, updated_at=? WHERE id=?`, string(to), final, final, ms(now), id)
	} else {
		switch {
		case to == domain.StateRunning:
			_, err = tx.Exec(`UPDATE executions SET state=?, started_at=COALESCE(started_at,?) WHERE id=?`, string(to), ms(now), id)
		case to.Terminal():
			_, err = tx.Exec(`UPDATE executions SET state=?, ended_at=? WHERE id=?`, string(to), ms(now), id)
		default:
			_, err = tx.Exec(`UPDATE executions SET state=? WHERE id=?`, string(to), id)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SetApproval records the config hash the user approved ("" revokes).
func (s *Store) SetApproval(wsID, hash string) error {
	_, err := s.DB.Exec(`UPDATE task_workspaces SET approved_hash=?, updated_at=? WHERE id=?`, hash, ms(time.Now()), wsID)
	return err
}

// UpdateWorkspaceConfig replaces the configuration and revokes approval.
func (s *Store) UpdateWorkspaceConfig(wsID, yaml string) error {
	_, err := s.DB.Exec(`UPDATE task_workspaces SET config_yaml=?, approved_hash='', updated_at=? WHERE id=?`, yaml, ms(time.Now()), wsID)
	return err
}

// DeleteWorkspace removes a workspace that has no executions.
func (s *Store) DeleteWorkspace(id string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM executions WHERE workspace_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("workspace has %d recorded executions; they are evidence and are kept (archive instead)", n)
	}
	_, err := s.DB.Exec(`DELETE FROM task_workspaces WHERE id=?`, id)
	return err
}

// ---- executions ----

const exCols = `id,workspace_id,project_id,task,state,isolation,runtide_version,error,verification,environment_json,created_at,started_at,ended_at`

func scanExec(r interface{ Scan(...any) error }) (*domain.Execution, error) {
	var e domain.Execution
	var st, env string
	var iso int
	var c int64
	var sa, ea sql.NullInt64
	if err := r.Scan(&e.ID, &e.WorkspaceID, &e.ProjectID, &e.Task, &st, &iso, &e.RuntideVersion, &e.Error, &e.Verification, &env, &c, &sa, &ea); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	e.State, e.Isolation, e.Environment = domain.State(st), domain.Isolation(iso), []byte(env)
	e.CreatedAt, e.StartedAt, e.EndedAt = fromMs(c), ptrTime(sa), ptrTime(ea)
	return &e, nil
}

// CreateExecution inserts an execution in state CREATED.
func (s *Store) CreateExecution(e *domain.Execution) error {
	if e.ID == "" {
		e.ID = domain.NewID(domain.PrefixExecution)
	}
	e.State, e.CreatedAt = domain.StateCreated, time.Now()
	env := string(e.Environment)
	if env == "" {
		env = "{}"
	}
	_, err := s.DB.Exec(`INSERT INTO executions(`+exCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.WorkspaceID, e.ProjectID, e.Task, string(e.State), int(e.Isolation), e.RuntideVersion, "", "", env, ms(e.CreatedAt), nil, nil)
	return err
}

// GetExecution loads an execution by id (or unique id prefix of at least 8 chars).
func (s *Store) GetExecution(id string) (*domain.Execution, error) {
	e, err := scanExec(s.DB.QueryRow(`SELECT `+exCols+` FROM executions WHERE id=?`, id))
	if err == nil || len(id) < 8 {
		return e, err
	}
	rows, qerr := s.DB.Query(`SELECT `+exCols+` FROM executions WHERE id LIKE ? ESCAPE '\' LIMIT 2`, strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(id)+"%")
	if qerr != nil {
		return nil, qerr
	}
	defer rows.Close()
	var found []*domain.Execution
	for rows.Next() {
		x, err := scanExec(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, x)
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("execution id prefix %q is ambiguous", id)
	}
	return nil, ErrNotFound
}

// ExecFilter narrows ListExecutions.
type ExecFilter struct {
	ProjectID, WorkspaceID string
	States                 []domain.State
	Limit                  int
}

// PruneExecutions deletes ended executions (and, by cascade, their events,
// commands, output, file changes and verification results) that ended before
// cutoff. Executions that still have a live recorded process are never removed
// (their evidence is needed for orphan handling). With dryRun it only counts.
// Receipt files and receipt index rows are not touched.
func (s *Store) PruneExecutions(cutoff time.Time, dryRun bool) (int64, error) {
	where := `state IN ('COMPLETED','FAILED','CANCELLED','INTERRUPTED') AND COALESCE(ended_at, created_at) < ?
	  AND id NOT IN (SELECT execution_id FROM managed_processes WHERE ended_at IS NULL)`
	if dryRun {
		var n int64
		err := s.DB.QueryRow(`SELECT COUNT(*) FROM executions WHERE `+where, cutoff.Unix()).Scan(&n)
		return n, err
	}
	res, err := s.DB.Exec(`DELETE FROM executions WHERE `+where, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		s.Audit("local", "executions.prune", "", fmt.Sprintf("%d executions ended before %s", n, cutoff.Format(time.RFC3339)))
	}
	return n, nil
}

// ListExecutions returns executions newest first.
func (s *Store) ListExecutions(f ExecFilter) ([]*domain.Execution, error) {
	q := `SELECT ` + exCols + ` FROM executions WHERE 1=1`
	var args []any
	if f.ProjectID != "" {
		q += ` AND project_id=?`
		args = append(args, f.ProjectID)
	}
	if f.WorkspaceID != "" {
		q += ` AND workspace_id=?`
		args = append(args, f.WorkspaceID)
	}
	if len(f.States) > 0 {
		q += ` AND state IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.States)), ",") + `)`
		for _, st := range f.States {
			args = append(args, string(st))
		}
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	rows, err := s.DB.Query(q+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Execution
	for rows.Next() {
		e, err := scanExec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetExecutionError stores a failure explanation.
func (s *Store) SetExecutionError(id, msg string) error {
	_, err := s.DB.Exec(`UPDATE executions SET error=? WHERE id=?`, msg, id)
	return err
}

// SetExecutionVerification stores the overall verification outcome.
func (s *Store) SetExecutionVerification(id, status string) error {
	_, err := s.DB.Exec(`UPDATE executions SET verification=? WHERE id=?`, status, id)
	return err
}

// SetExecutionEnvironment stores the captured environment snapshot (JSON).
func (s *Store) SetExecutionEnvironment(id string, js []byte) error {
	_, err := s.DB.Exec(`UPDATE executions SET environment_json=? WHERE id=?`, string(js), id)
	return err
}

// ---- events ----

// AppendEvent assigns the next sequence number and stores the event.
func (s *Store) AppendEvent(e *domain.ExecEvent) error {
	if e.ID == "" {
		e.ID = domain.NewID(domain.PrefixEvent)
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Source == "" {
		e.Source = domain.SourceObserved
	}
	if e.Severity == "" {
		e.Severity = domain.SevInfo
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return fmt.Errorf("event data is not serialisable: %w", err)
	}
	if e.Data == nil {
		data = []byte("{}")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq int64
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM exec_events WHERE execution_id=?`, e.ExecutionID).Scan(&seq); err != nil {
		return err
	}
	e.Seq = seq
	_, err = s.DB.Exec(`INSERT INTO exec_events(id,execution_id,seq,ts,type,source,severity,subject,data_json) VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ExecutionID, e.Seq, ms(e.Time), e.Type, e.Source, e.Severity, e.Subject, string(data))
	return err
}

// EventFilter narrows ListEvents.
type EventFilter struct {
	Types    []string
	Subject  string
	Severity string
	Search   string
	AfterSeq int64
	Limit    int
}

// ListEvents returns events in sequence order.
func (s *Store) ListEvents(execID string, f EventFilter) ([]domain.ExecEvent, error) {
	q := `SELECT id,execution_id,seq,ts,type,source,severity,subject,data_json FROM exec_events WHERE execution_id=? AND seq>?`
	args := []any{execID, f.AfterSeq}
	if len(f.Types) > 0 {
		q += ` AND type IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Types)), ",") + `)`
		for _, t := range f.Types {
			args = append(args, t)
		}
	}
	if f.Subject != "" {
		q += ` AND subject=?`
		args = append(args, f.Subject)
	}
	if f.Severity != "" {
		q += ` AND severity=?`
		args = append(args, f.Severity)
	}
	if f.Search != "" {
		q += ` AND (type LIKE ? ESCAPE '\' OR subject LIKE ? ESCAPE '\' OR data_json LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Search) + "%"
		args = append(args, like, like, like)
	}
	if f.Limit <= 0 || f.Limit > 50000 {
		f.Limit = 5000
	}
	rows, err := s.DB.Query(q+` ORDER BY seq LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ExecEvent{}
	for rows.Next() {
		var e domain.ExecEvent
		var ts int64
		var js string
		if err := rows.Scan(&e.ID, &e.ExecutionID, &e.Seq, &ts, &e.Type, &e.Source, &e.Severity, &e.Subject, &js); err != nil {
			return nil, err
		}
		e.Time = fromMs(ts)
		_ = json.Unmarshal([]byte(js), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- commands & output ----

// InsertCommand stores a command at start (sequence allocated per execution).
func (s *Store) InsertCommand(c *domain.Command) error {
	if c.ID == "" {
		c.ID = domain.NewID(domain.PrefixCommand)
	}
	args, _ := json.Marshal(c.Args)
	keys, _ := json.Marshal(c.EnvKeys)
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq int
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM exec_commands WHERE execution_id=?`, c.ExecutionID).Scan(&seq); err != nil {
		return err
	}
	c.Seq = seq
	_, err := s.DB.Exec(`INSERT INTO exec_commands(id,execution_id,seq,kind,name,executable,args_json,cwd,env_keys_json,isolation,started_at,redacted) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.ExecutionID, c.Seq, c.Kind, c.Name, c.Executable, string(args), c.Cwd, string(keys), int(c.Isolation), ms(c.StartedAt), b2i(c.Redacted))
	return err
}

// SetCommandPID records the OS pid once known.
func (s *Store) SetCommandPID(id string, pid int) error {
	_, err := s.DB.Exec(`UPDATE exec_commands SET pid=? WHERE id=?`, pid, id)
	return err
}

// FinishCommand stores the result of a command.
func (s *Store) FinishCommand(c *domain.Command) error {
	var ec any
	if c.ExitCode != nil {
		ec = *c.ExitCode
	}
	_, err := s.DB.Exec(`UPDATE exec_commands SET ended_at=?, exit_code=?, cancelled=?, timed_out=?, truncated=?, stdout_bytes=?, stderr_bytes=?, error=? WHERE id=?`,
		nullMs(c.EndedAt), ec, b2i(c.Cancelled), b2i(c.TimedOut), b2i(c.Truncated), c.StdoutBytes, c.StderrBytes, c.Error, c.ID)
	return err
}

const cmdCols = `id,execution_id,seq,kind,name,executable,args_json,cwd,env_keys_json,isolation,started_at,ended_at,exit_code,pid,cancelled,timed_out,truncated,redacted,stdout_bytes,stderr_bytes,error`

func scanCmd(r interface{ Scan(...any) error }) (*domain.Command, error) {
	var c domain.Command
	var args, keys string
	var iso int
	var sa int64
	var ea, ec sql.NullInt64
	var cancelled, timed, trunc, red int
	if err := r.Scan(&c.ID, &c.ExecutionID, &c.Seq, &c.Kind, &c.Name, &c.Executable, &args, &c.Cwd, &keys, &iso, &sa, &ea, &ec, &c.PID, &cancelled, &timed, &trunc, &red, &c.StdoutBytes, &c.StderrBytes, &c.Error); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(args), &c.Args)
	_ = json.Unmarshal([]byte(keys), &c.EnvKeys)
	c.Isolation, c.StartedAt, c.EndedAt = domain.Isolation(iso), fromMs(sa), ptrTime(ea)
	if ec.Valid {
		v := int(ec.Int64)
		c.ExitCode = &v
	}
	c.Cancelled, c.TimedOut, c.Truncated, c.Redacted = cancelled == 1, timed == 1, trunc == 1, red == 1
	return &c, nil
}

// ListCommands returns an execution's commands in start order.
func (s *Store) ListCommands(execID string) ([]domain.Command, error) {
	rows, err := s.DB.Query(`SELECT `+cmdCols+` FROM exec_commands WHERE execution_id=? ORDER BY seq`, execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Command{}
	for rows.Next() {
		c, err := scanCmd(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCommand loads one command.
func (s *Store) GetCommand(id string) (*domain.Command, error) {
	return scanCmd(s.DB.QueryRow(`SELECT `+cmdCols+` FROM exec_commands WHERE id=?`, id))
}

// OutputBatch is lines to persist for a command.
type OutputBatch struct {
	CommandID string
	Lines     []domain.OutputLine
}

// AppendOutput stores output lines in one transaction.
func (s *Store) AppendOutput(b OutputBatch) error {
	if len(b.Lines) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		st, err := tx.Prepare(`INSERT INTO exec_output(command_id,ts,stream,line) VALUES(?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, l := range b.Lines {
			if _, err := st.Exec(b.CommandID, ms(l.Time), l.Stream, l.Line); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListOutput returns stored output lines of a command in order.
func (s *Store) ListOutput(commandID string, offset, limit int) ([]domain.OutputLine, error) {
	if limit <= 0 || limit > 100000 {
		limit = 10000
	}
	rows, err := s.DB.Query(`SELECT id,command_id,ts,stream,line FROM exec_output WHERE command_id=? ORDER BY id LIMIT ? OFFSET ?`, commandID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.OutputLine{}
	for rows.Next() {
		var l domain.OutputLine
		var ts int64
		if err := rows.Scan(&l.ID, &l.CommandID, &ts, &l.Stream, &l.Line); err != nil {
			return nil, err
		}
		l.Time = fromMs(ts)
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---- managed processes (orphan detection) ----

// ManagedProcess is a process RUNTIDE launched; open ones survive crashes in the table.
type ManagedProcess struct {
	ID          int64
	ExecutionID string
	CommandID   string
	PID         int
	StartTimeNs int64
	Exe         string
	StartedAt   time.Time
}

// AddManagedProcess records a launched process.
func (s *Store) AddManagedProcess(mp ManagedProcess) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO managed_processes(execution_id,command_id,pid,start_time_ns,exe,started_at) VALUES(?,?,?,?,?,?)`,
		mp.ExecutionID, mp.CommandID, mp.PID, mp.StartTimeNs, mp.Exe, ms(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EndManagedProcess marks a process as gone.
func (s *Store) EndManagedProcess(id int64) error {
	_, err := s.DB.Exec(`UPDATE managed_processes SET ended_at=? WHERE id=? AND ended_at IS NULL`, ms(time.Now()), id)
	return err
}

// OpenManagedProcesses lists processes never marked as ended.
func (s *Store) OpenManagedProcesses() ([]ManagedProcess, error) {
	rows, err := s.DB.Query(`SELECT id,execution_id,command_id,pid,start_time_ns,exe,started_at FROM managed_processes WHERE ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManagedProcess
	for rows.Next() {
		var m ManagedProcess
		var sa int64
		if err := rows.Scan(&m.ID, &m.ExecutionID, &m.CommandID, &m.PID, &m.StartTimeNs, &m.Exe, &sa); err != nil {
			return nil, err
		}
		m.StartedAt = fromMs(sa)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListManagedProcesses returns all processes of an execution (ended or not).
func (s *Store) ListManagedProcesses(execID string) ([]ManagedProcessRecord, error) {
	rows, err := s.DB.Query(`SELECT command_id,pid,exe,started_at,ended_at FROM managed_processes WHERE execution_id=? ORDER BY id`, execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedProcessRecord{}
	for rows.Next() {
		var m ManagedProcessRecord
		var sa int64
		var ea sql.NullInt64
		if err := rows.Scan(&m.CommandID, &m.PID, &m.Exe, &sa, &ea); err != nil {
			return nil, err
		}
		m.StartedAt, m.EndedAt = fromMs(sa), ptrTime(ea)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ManagedProcessRecord is the exportable view of a managed process.
type ManagedProcessRecord struct {
	CommandID string     `json:"commandId"`
	PID       int        `json:"pid"`
	Exe       string     `json:"exe"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// ---- services ----

// ServiceRecord is the stored state of an execution's service.
type ServiceRecord struct {
	Name         string    `json:"name"`
	CommandID    string    `json:"commandId"`
	Health       string    `json:"health"`
	HealthDetail string    `json:"healthDetail"`
	Restarts     int       `json:"restarts"`
	LastState    string    `json:"lastState"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// UpsertService stores a service snapshot.
func (s *Store) UpsertService(execID string, r ServiceRecord) error {
	_, err := s.DB.Exec(`INSERT INTO exec_services(execution_id,name,command_id,health,health_detail,restarts,last_state,updated_at) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(execution_id,name) DO UPDATE SET command_id=excluded.command_id, health=excluded.health, health_detail=excluded.health_detail, restarts=excluded.restarts, last_state=excluded.last_state, updated_at=excluded.updated_at`,
		execID, r.Name, r.CommandID, r.Health, r.HealthDetail, r.Restarts, r.LastState, ms(time.Now()))
	return err
}

// ListServices returns an execution's services.
func (s *Store) ListServices(execID string) ([]ServiceRecord, error) {
	rows, err := s.DB.Query(`SELECT name,command_id,health,health_detail,restarts,last_state,updated_at FROM exec_services WHERE execution_id=? ORDER BY name`, execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceRecord{}
	for rows.Next() {
		var r ServiceRecord
		var u int64
		if err := rows.Scan(&r.Name, &r.CommandID, &r.Health, &r.HealthDetail, &r.Restarts, &r.LastState, &u); err != nil {
			return nil, err
		}
		r.UpdatedAt = fromMs(u)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- file changes ----

// ReplaceFileChanges stores the change manifest of an execution.
func (s *Store) ReplaceFileChanges(execID string, changes []domain.FileChange) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM file_changes WHERE execution_id=?`, execID); err != nil {
			return err
		}
		st, err := tx.Prepare(`INSERT INTO file_changes(execution_id,path,change,old_path,size_before,size_after,hash_before,hash_after,mod_time) VALUES(?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, c := range changes {
			if _, err := st.Exec(execID, c.Path, c.Change, c.OldPath, c.SizeBefore, c.SizeAfter, c.HashBefore, c.HashAfter, c.ModTime); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListFileChanges returns the manifest.
func (s *Store) ListFileChanges(execID string) ([]domain.FileChange, error) {
	rows, err := s.DB.Query(`SELECT path,change,old_path,size_before,size_after,hash_before,hash_after,mod_time FROM file_changes WHERE execution_id=? ORDER BY path`, execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FileChange{}
	for rows.Next() {
		var c domain.FileChange
		if err := rows.Scan(&c.Path, &c.Change, &c.OldPath, &c.SizeBefore, &c.SizeAfter, &c.HashBefore, &c.HashAfter, &c.ModTime); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PutBaseline stores the pre-run content of a small text file for diffing.
func (s *Store) PutBaseline(execID, path string, content []byte) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO file_baselines(execution_id,path,content) VALUES(?,?,?)`, execID, path, content)
	return err
}

// GetBaseline returns stored baseline content.
func (s *Store) GetBaseline(execID, path string) ([]byte, error) {
	var b []byte
	err := s.DB.QueryRow(`SELECT content FROM file_baselines WHERE execution_id=? AND path=?`, execID, path).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// ---- verification ----

// SaveVerification stores a verification run's results and returns the run number.
func (s *Store) SaveVerification(execID string, results []domain.VerificationResult) (int, error) {
	var run int
	err := s.tx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT COALESCE(MAX(run),0)+1 FROM verification_results WHERE execution_id=?`, execID).Scan(&run); err != nil {
			return err
		}
		st, err := tx.Prepare(`INSERT INTO verification_results(execution_id,run,seq,name,description,type,status,required,started_at,ended_at,exit_code,output,error,needs_json,evidence_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, r := range results {
			var ec any
			if r.ExitCode != nil {
				ec = *r.ExitCode
			}
			needs, _ := json.Marshal(r.Needs)
			ev, _ := json.Marshal(r.Evidence)
			if _, err := st.Exec(execID, run, r.Seq, r.Name, r.Description, r.Type, r.Status, b2i(r.Required), ms(r.StartedAt), ms(r.EndedAt), ec, r.Output, r.Error, string(needs), string(ev)); err != nil {
				return err
			}
		}
		return nil
	})
	return run, err
}

// ListVerification returns the latest run's results (or all runs when all is true).
func (s *Store) ListVerification(execID string, all bool) ([]domain.VerificationResult, error) {
	q := `SELECT seq,name,description,type,status,required,started_at,ended_at,exit_code,output,error,needs_json,evidence_json FROM verification_results WHERE execution_id=?`
	if !all {
		q += ` AND run=(SELECT MAX(run) FROM verification_results WHERE execution_id=?)`
	}
	q += ` ORDER BY run, seq`
	args := []any{execID}
	if !all {
		args = append(args, execID)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.VerificationResult{}
	for rows.Next() {
		var r domain.VerificationResult
		var req int
		var sa, ea int64
		var ec sql.NullInt64
		var needs, ev string
		if err := rows.Scan(&r.Seq, &r.Name, &r.Description, &r.Type, &r.Status, &req, &sa, &ea, &ec, &r.Output, &r.Error, &needs, &ev); err != nil {
			return nil, err
		}
		r.Required, r.StartedAt, r.EndedAt = req == 1, fromMs(sa), fromMs(ea)
		r.DurationMs = ea - sa
		if ec.Valid {
			v := int(ec.Int64)
			r.ExitCode = &v
		}
		_ = json.Unmarshal([]byte(needs), &r.Needs)
		_ = json.Unmarshal([]byte(ev), &r.Evidence)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- receipts ----

// ReceiptRecord indexes a receipt file.
type ReceiptRecord struct {
	ID           string    `json:"id"`
	ExecutionID  string    `json:"executionId"`
	ProjectName  string    `json:"projectName"`
	Path         string    `json:"path"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
	Imported     bool      `json:"imported"`
	Status       string    `json:"status"`
	SignedBy     string    `json:"signedBy,omitempty"`
	ManifestJSON string    `json:"manifest,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AddReceipt indexes a receipt; an identical file (same SHA-256) is returned instead of duplicated.
func (s *Store) AddReceipt(r *ReceiptRecord) error {
	var existing string
	if err := s.DB.QueryRow(`SELECT id FROM receipts WHERE sha256=?`, r.SHA256).Scan(&existing); err == nil {
		r.ID = existing
		return nil
	}
	if r.ID == "" {
		r.ID = domain.NewID(domain.PrefixReceipt)
	}
	r.CreatedAt = time.Now()
	_, err := s.DB.Exec(`INSERT INTO receipts(id,execution_id,project_name,path,sha256,size,imported,status,signed_by,manifest_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ExecutionID, r.ProjectName, r.Path, r.SHA256, r.Size, b2i(r.Imported), r.Status, r.SignedBy, r.ManifestJSON, ms(r.CreatedAt))
	return err
}

// ListReceipts returns indexed receipts newest first.
func (s *Store) ListReceipts() ([]ReceiptRecord, error) {
	rows, err := s.DB.Query(`SELECT id,execution_id,project_name,path,sha256,size,imported,status,signed_by,created_at FROM receipts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReceiptRecord{}
	for rows.Next() {
		var r ReceiptRecord
		var imp int
		var c int64
		if err := rows.Scan(&r.ID, &r.ExecutionID, &r.ProjectName, &r.Path, &r.SHA256, &r.Size, &imp, &r.Status, &r.SignedBy, &c); err != nil {
			return nil, err
		}
		r.Imported, r.CreatedAt = imp == 1, fromMs(c)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetReceipt finds a receipt by id or sha256 prefix.
func (s *Store) GetReceipt(ref string) (*ReceiptRecord, error) {
	var r ReceiptRecord
	var imp int
	var c int64
	err := s.DB.QueryRow(`SELECT id,execution_id,project_name,path,sha256,size,imported,status,signed_by,manifest_json,created_at FROM receipts WHERE id=? OR sha256=? OR (length(?)>=8 AND sha256 LIKE ?||'%')`, ref, ref, ref, ref).
		Scan(&r.ID, &r.ExecutionID, &r.ProjectName, &r.Path, &r.SHA256, &r.Size, &imp, &r.Status, &r.SignedBy, &r.ManifestJSON, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	r.Imported, r.CreatedAt = imp == 1, fromMs(c)
	return &r, err
}

// ---- audit ----

// Audit appends an audit event.
func (s *Store) Audit(actor, action, subject, detail string) {
	_, _ = s.DB.Exec(`INSERT INTO audit_events(ts,actor,action,subject,detail) VALUES(?,?,?,?,?)`, ms(time.Now()), actor, action, subject, detail)
}

// ListAudit returns recent audit events.
func (s *Store) ListAudit(limit int) ([]domain.AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT ts,actor,action,subject,detail FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AuditEvent{}
	for rows.Next() {
		var a domain.AuditEvent
		var ts int64
		if err := rows.Scan(&ts, &a.Actor, &a.Action, &a.Subject, &a.Detail); err != nil {
			return nil, err
		}
		a.Time = fromMs(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) tx(fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
