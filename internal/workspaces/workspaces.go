// Package workspaces stores workspace definitions and orchestrates dependency
// aware start/stop of their services through the supervisor.
package workspaces

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/health"
	"github.com/runtide/runtide/internal/supervisor"
)

// ErrNotApproved is returned when a workspace has not been authorised to run.
var ErrNotApproved = errors.New("workspace has not been approved for execution")

// ErrNotFound is returned for unknown workspaces.
var ErrNotFound = errors.New("workspace not found")

// Record is a stored workspace.
type Record struct {
	Name        string            `json:"name"`
	Root        string            `json:"root"`
	Description string            `json:"description"`
	YAML        string            `json:"yaml"`
	Spec        *config.Workspace `json:"spec"`
	Approved    bool              `json:"approved"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// Preview is what an import would do; shown to the user before anything is stored.
type Preview struct {
	Name     string     `json:"name"`
	Root     string     `json:"root"`
	Services []string   `json:"services"`
	Commands []string   `json:"commands"` // human-readable command lines that would run
	Order    [][]string `json:"order"`
	Warnings []string   `json:"warnings"`
	Exists   bool       `json:"exists"`
}

// Progress is reported while a workspace starts or stops.
type Progress struct {
	Workspace string `json:"workspace"`
	Service   string `json:"service"`
	Phase     string `json:"phase"` // starting|ready|failed|stopping|stopped|rolled-back|done
	Detail    string `json:"detail"`
	Step      int    `json:"step"`
	Total     int    `json:"total"`
}

// Manager coordinates storage and execution.
type Manager struct {
	db   *database.DB
	sup  *supervisor.Manager
	mu   sync.Mutex
	busy map[string]bool // workspaces with an operation in flight
}

// NewManager creates a manager.
func NewManager(db *database.DB, sup *supervisor.Manager) *Manager {
	return &Manager{db: db, sup: sup, busy: map[string]bool{}}
}

// Key builds the supervisor key of a workspace service.
func Key(ws, svc string) string { return ws + "/" + svc }

func hash(yaml, root string) string {
	h := sha256.Sum256([]byte(yaml + "\x00" + strings.ToLower(root)))
	return hex.EncodeToString(h[:])
}

func commandLine(s config.ServiceSpec) string {
	parts := append([]string{s.Command}, s.Args...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t") {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, " ")
}

// Inspect parses and validates YAML without storing it. root must be an
// existing absolute directory.
func (m *Manager) Inspect(yamlText, root string) (*Preview, *config.Workspace, error) {
	ws, err := config.Parse([]byte(yamlText))
	if err != nil {
		return nil, nil, err
	}
	if root == "" {
		root = ws.Root
	}
	if root == "" {
		return nil, nil, errors.New("workspace root directory is required")
	}
	if !filepath.IsAbs(root) {
		return nil, nil, fmt.Errorf("workspace root %q must be an absolute path", root)
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("workspace root %q is not an existing directory", root)
	}
	ws.Root = filepath.Clean(root)
	if err := ws.Validate(); err != nil {
		return nil, nil, err
	}
	p := &Preview{Name: ws.Name, Root: ws.Root}
	for _, s := range ws.Services {
		p.Services = append(p.Services, s.Name)
		p.Commands = append(p.Commands, fmt.Sprintf("%s: %s  (in %s)", s.Name, commandLine(s), workdirOf(ws.Root, s)))
		if _, err := os.Stat(workdirOf(ws.Root, s)); err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("service %q: working directory %s does not exist yet", s.Name, workdirOf(ws.Root, s)))
		}
	}
	p.Order, _ = ws.StartOrder()
	if _, err := m.Get(ws.Name); err == nil {
		p.Exists = true
	}
	return p, ws, nil
}

func workdirOf(root string, s config.ServiceSpec) string {
	if s.WorkingDirectory == "" {
		return root
	}
	d, _ := config.ResolveDir(root, s.WorkingDirectory)
	return d
}

// Save stores a workspace (create or replace). Any change to the definition
// revokes approval: the user must authorise the new commands again.
func (m *Manager) Save(yamlText, root string, replace bool) (*Record, error) {
	_, ws, err := m.Inspect(yamlText, root)
	if err != nil {
		return nil, err
	}
	canon, err := config.Marshal(&config.Workspace{Name: ws.Name, Description: ws.Description, Services: ws.Services})
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	existing, getErr := m.Get(ws.Name)
	if getErr == nil && !replace {
		return nil, fmt.Errorf("workspace %q already exists", ws.Name)
	}
	if getErr == nil {
		// Block edits while running.
		for _, s := range ws.Services {
			if inf, ok := m.sup.Get(Key(ws.Name, s.Name)); ok && isActive(inf.State) {
				return nil, fmt.Errorf("workspace %q has running services; stop it before editing", ws.Name)
			}
		}
		keep := ""
		if existing.Approved && hash(string(canon), ws.Root) == hash(existing.YAML, existing.Root) {
			keep = hash(string(canon), ws.Root)
		}
		_, err = m.db.Exec(`UPDATE workspaces SET root=?, description=?, yaml=?, approved_hash=?, updated_at=? WHERE name=?`,
			ws.Root, ws.Description, string(canon), keep, now, ws.Name)
	} else {
		_, err = m.db.Exec(`INSERT INTO workspaces(name,root,description,yaml,approved_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			ws.Name, ws.Root, ws.Description, string(canon), "", now, now)
	}
	if err != nil {
		return nil, err
	}
	m.history(ws.Name, "save", "ok", "")
	return m.Get(ws.Name)
}

func isActive(s supervisor.State) bool {
	return s == supervisor.StateRunning || s == supervisor.StateStarting || s == supervisor.StateStopping || s == supervisor.StateBackoff
}

// Get loads one workspace.
func (m *Manager) Get(name string) (*Record, error) {
	row := m.db.QueryRow(`SELECT name,root,description,yaml,approved_hash,created_at,updated_at FROM workspaces WHERE name=?`, name)
	return scan(row)
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (*Record, error) {
	var rec Record
	var ah string
	var c, u int64
	if err := r.Scan(&rec.Name, &rec.Root, &rec.Description, &rec.YAML, &ah, &c, &u); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rec.CreatedAt, rec.UpdatedAt = time.Unix(c, 0), time.Unix(u, 0)
	rec.Approved = ah != "" && ah == hash(rec.YAML, rec.Root)
	ws, err := config.Parse([]byte(rec.YAML))
	if err == nil {
		ws.Root = rec.Root
		for i := range ws.Services {
			ws.Services[i].ApplyDefaults()
		}
		rec.Spec = ws
	}
	return &rec, nil
}

// List returns all workspaces.
func (m *Manager) List() ([]*Record, error) {
	rows, err := m.db.Query(`SELECT name,root,description,yaml,approved_hash,created_at,updated_at FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Record
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Approve records the user's authorisation to execute the workspace's current
// definition. Callers must have shown the commands to the user first.
func (m *Manager) Approve(name string) error {
	r, err := m.Get(name)
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`UPDATE workspaces SET approved_hash=? WHERE name=?`, hash(r.YAML, r.Root), name)
	if err == nil {
		m.history(name, "approve", "ok", "")
	}
	return err
}

// Delete removes a workspace. Running services block deletion.
func (m *Manager) Delete(name string) error {
	r, err := m.Get(name)
	if err != nil {
		return err
	}
	if r.Spec != nil {
		for _, s := range r.Spec.Services {
			if inf, ok := m.sup.Get(Key(name, s.Name)); ok && isActive(inf.State) {
				return fmt.Errorf("workspace %q is running; stop it first", name)
			}
		}
		for _, s := range r.Spec.Services {
			_ = m.sup.Forget(Key(name, s.Name))
		}
	}
	if _, err := m.db.Exec(`DELETE FROM workspaces WHERE name=?`, name); err != nil {
		return err
	}
	m.history(name, "delete", "ok", "")
	return nil
}

// Export returns portable YAML for the workspace.
func (m *Manager) Export(name string) (string, error) {
	r, err := m.Get(name)
	if err != nil {
		return "", err
	}
	return r.YAML, nil
}

func (m *Manager) history(ws, action, outcome, detail string) {
	_, _ = m.db.Exec(`INSERT INTO workspace_history(workspace,ts,action,outcome,detail) VALUES(?,?,?,?,?)`, ws, time.Now().Unix(), action, outcome, detail)
}

// History is one past workspace operation.
type History struct {
	Workspace string    `json:"workspace"`
	Time      time.Time `json:"time"`
	Action    string    `json:"action"`
	Outcome   string    `json:"outcome"`
	Detail    string    `json:"detail"`
}

// History returns recent operations (all workspaces when name is empty).
func (m *Manager) History(name string, limit int) ([]History, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT workspace,ts,action,outcome,detail FROM workspace_history`
	var args []any
	if name != "" {
		q += ` WHERE workspace=?`
		args = append(args, name)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []History
	for rows.Next() {
		var h History
		var ts int64
		if err := rows.Scan(&h.Workspace, &ts, &h.Action, &h.Outcome, &h.Detail); err != nil {
			return nil, err
		}
		h.Time = time.Unix(ts, 0)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (m *Manager) lock(name string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[name] {
		return nil, fmt.Errorf("another operation on workspace %q is already in progress", name)
	}
	m.busy[name] = true
	return func() { m.mu.Lock(); delete(m.busy, name); m.mu.Unlock() }, nil
}

func report(fn func(Progress), p Progress) {
	if fn != nil {
		fn(p)
	}
}

// Up starts the workspace in dependency order. Services within a layer start
// concurrently; the next layer begins only when every service of the previous
// layer is ready. On failure, services started by this call are stopped again
// (rolled back) so a half-started environment is not left behind.
func (m *Manager) Up(ctx context.Context, name string, only []string, fn func(Progress)) (err error) {
	rec, err := m.Get(name)
	if err != nil {
		return err
	}
	if !rec.Approved {
		return ErrNotApproved
	}
	if rec.Spec == nil {
		return errors.New("stored workspace definition is unreadable")
	}
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	ws := rec.Spec
	if err := ws.Validate(); err != nil {
		return err
	}
	layers, err := ws.StartOrder()
	if err != nil {
		return err
	}
	want := map[string]bool{}
	if len(only) > 0 {
		for _, o := range only {
			if ws.Service(o) == nil {
				return fmt.Errorf("workspace %q has no service %q", name, o)
			}
			want[o] = true
		}
		// pull in dependencies transitively
		var add func(string)
		add = func(s string) {
			for _, d := range ws.Service(s).DependsOn {
				if !want[d] {
					want[d] = true
					add(d)
				}
			}
		}
		for o := range want {
			add(o)
		}
	}
	total := 0
	for _, l := range layers {
		for _, s := range l {
			if len(want) == 0 || want[s] {
				total++
			}
		}
	}
	var startedNow []string
	step := 0
	defer func() {
		if err != nil {
			// roll back what this run started
			for i := len(startedNow) - 1; i >= 0; i-- {
				_ = m.sup.Stop(Key(name, startedNow[i]))
				report(fn, Progress{Workspace: name, Service: startedNow[i], Phase: "rolled-back", Detail: "stopped because startup failed", Step: step, Total: total})
			}
			m.history(name, "up", "failed", err.Error())
		} else {
			m.history(name, "up", "ok", fmt.Sprintf("%d services", total))
		}
	}()
	for _, layer := range layers {
		var todo []string
		for _, s := range layer {
			if len(want) > 0 && !want[s] {
				continue
			}
			if inf, ok := m.sup.Get(Key(name, s)); ok && inf.State == supervisor.StateRunning {
				report(fn, Progress{Workspace: name, Service: s, Phase: "ready", Detail: "already running", Step: step, Total: total})
				step++
				continue
			}
			todo = append(todo, s)
		}
		var wg sync.WaitGroup
		var emu sync.Mutex
		var errs []error
		for _, s := range todo {
			spec := *ws.Service(s)
			report(fn, Progress{Workspace: name, Service: s, Phase: "starting", Detail: commandLine(spec), Step: step, Total: total})
			if e := m.sup.Start(Key(name, s), spec, ws.Root); e != nil {
				emu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", s, e))
				emu.Unlock()
				report(fn, Progress{Workspace: name, Service: s, Phase: "failed", Detail: e.Error(), Step: step, Total: total})
				continue
			}
			startedNow = append(startedNow, s)
			wg.Add(1)
			go func(s string, spec config.ServiceSpec) {
				defer wg.Done()
				timeout := time.Duration(spec.StartTimeoutSec) * time.Second
				if e := m.sup.WaitReady(ctx, Key(name, s), timeout, 1500*time.Millisecond); e != nil {
					emu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", s, e))
					emu.Unlock()
					report(fn, Progress{Workspace: name, Service: s, Phase: "failed", Detail: e.Error(), Step: step, Total: total})
					return
				}
				report(fn, Progress{Workspace: name, Service: s, Phase: "ready", Detail: "ready", Step: step, Total: total})
			}(s, spec)
		}
		wg.Wait()
		step += len(todo)
		if len(errs) > 0 {
			return fmt.Errorf("startup failed: %w", errors.Join(errs...))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	report(fn, Progress{Workspace: name, Phase: "done", Detail: "all services ready", Step: total, Total: total})
	return nil
}

// Down stops the workspace in reverse dependency order.
func (m *Manager) Down(ctx context.Context, name string, fn func(Progress)) (err error) {
	rec, err := m.Get(name)
	if err != nil {
		return err
	}
	if rec.Spec == nil {
		return errors.New("stored workspace definition is unreadable")
	}
	unlock, err := m.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	layers, err := rec.Spec.StopOrder()
	if err != nil {
		return err
	}
	defer func() {
		o, d := "ok", ""
		if err != nil {
			o, d = "failed", err.Error()
		}
		m.history(name, "down", o, d)
	}()
	var errs []error
	var emu sync.Mutex
	for _, layer := range layers {
		var wg sync.WaitGroup
		for _, s := range layer {
			inf, ok := m.sup.Get(Key(name, s))
			if !ok || !isActive(inf.State) {
				continue
			}
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				report(fn, Progress{Workspace: name, Service: s, Phase: "stopping"})
				if e := m.sup.Stop(Key(name, s)); e != nil {
					emu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", s, e))
					emu.Unlock()
					report(fn, Progress{Workspace: name, Service: s, Phase: "failed", Detail: e.Error()})
					return
				}
				report(fn, Progress{Workspace: name, Service: s, Phase: "stopped"})
			}(s)
		}
		wg.Wait()
	}
	report(fn, Progress{Workspace: name, Phase: "done"})
	return errors.Join(errs...)
}

// Restart is Down then Up.
func (m *Manager) Restart(ctx context.Context, name string, fn func(Progress)) error {
	if err := m.Down(ctx, name, fn); err != nil {
		return err
	}
	return m.Up(ctx, name, nil, fn)
}

// ServiceStatus pairs a service with its runtime state.
type ServiceStatus struct {
	Name    string             `json:"name"`
	Layer   int                `json:"layer"`
	Spec    config.ServiceSpec `json:"spec"`
	Runtime *supervisor.Info   `json:"runtime,omitempty"`
}

// Status is the live view of a workspace.
type Status struct {
	Name     string          `json:"name"`
	Health   string          `json:"health"` // stopped | starting | healthy | degraded | partial
	Running  int             `json:"running"`
	Total    int             `json:"total"`
	Services []ServiceStatus `json:"services"`
}

// Status computes live state.
func (m *Manager) Status(name string) (*Status, error) {
	rec, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	st := &Status{Name: name, Health: "stopped"}
	if rec.Spec == nil {
		return st, nil
	}
	layerOf := map[string]int{}
	if layers, err := rec.Spec.StartOrder(); err == nil {
		for i, l := range layers {
			for _, s := range l {
				layerOf[s] = i
			}
		}
	}
	starting, bad := 0, 0
	for _, s := range rec.Spec.Services {
		ss := ServiceStatus{Name: s.Name, Layer: layerOf[s.Name], Spec: s}
		if inf, ok := m.sup.Get(Key(name, s.Name)); ok {
			i := inf
			ss.Runtime = &i
			switch inf.State {
			case supervisor.StateRunning:
				st.Running++
				switch inf.Health {
				case health.Unhealthy:
					bad++
				case health.Starting:
					starting++
				}
			case supervisor.StateStarting, supervisor.StateBackoff:
				starting++
			case supervisor.StateFailed:
				bad++
			}
		}
		st.Services = append(st.Services, ss)
	}
	sort.SliceStable(st.Services, func(i, j int) bool { return st.Services[i].Layer < st.Services[j].Layer })
	st.Total = len(rec.Spec.Services)
	switch {
	case st.Running == 0 && starting == 0 && bad == 0:
		st.Health = "stopped"
	case bad > 0:
		st.Health = "degraded"
	case starting > 0:
		st.Health = "starting"
	case st.Running < st.Total:
		st.Health = "partial"
	default:
		st.Health = "healthy"
	}
	return st, nil
}
