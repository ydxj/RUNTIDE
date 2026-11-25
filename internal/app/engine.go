// Package app is RUNTIDE's core engine. The desktop app, the CLI and the
// background daemon all drive this one implementation through Dispatch, so
// behaviour can never diverge between them.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/runtide/runtide/internal/version"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/paths"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"github.com/runtide/runtide/internal/process"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/resources"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/workspaces"
)

// Settings are persisted user preferences.
type Settings struct {
	Theme            string   `json:"theme"` // light | dark | system
	RefreshSec       int      `json:"refreshSec"`
	SampleSec        int      `json:"sampleSec"`
	LogMemoryLines   int      `json:"logMemoryLines"`
	LogRetentionDays int      `json:"logRetentionDays"`
	LogMaxRows       int      `json:"logMaxRows"`
	Redaction        bool     `json:"redaction"`
	ExtraRedaction   []string `json:"extraRedaction"`
	ActivityDays     int      `json:"activityDays"`
	HealthDays       int      `json:"healthDays"`
	HistoryDays      int      `json:"historyDays"`
	MinimizeToTray   bool     `json:"minimizeToTray"`
	Notifications    bool     `json:"notifications"`
	StartWithWindows bool     `json:"startWithWindows"`
	StopOnExit       bool     `json:"stopServicesOnExit"`
	// TrustedKeys are receipt-signing key fingerprints (16 hex chars) the user trusts.
	TrustedKeys []string `json:"trustedKeys"`
	// Execution evidence limits. ExecRetentionDays 0 keeps executions forever
	// (the default: RUNTIDE never deletes evidence unless asked to).
	ExecRetentionDays int `json:"execRetentionDays"`
	ExecMaxOutputMB   int `json:"execMaxOutputMB"` // stored output per command
	ExecMaxEvents     int `json:"execMaxEvents"`   // events per execution
}

// DefaultSettings returns documented defaults.
func DefaultSettings() Settings {
	return Settings{Theme: "light", RefreshSec: 3, SampleSec: 2, LogMemoryLines: 5000, LogRetentionDays: 7,
		LogMaxRows: 200000, Redaction: true, ActivityDays: 30, HealthDays: 14, HistoryDays: 90,
		MinimizeToTray: true, Notifications: true, StopOnExit: true, ExecMaxOutputMB: 8, ExecMaxEvents: 100000}
}

func (s Settings) validate() error {
	in := func(v, lo, hi int, n string) error {
		if v < lo || v > hi {
			return fmt.Errorf("%s must be between %d and %d", n, lo, hi)
		}
		return nil
	}
	if s.Theme != "light" && s.Theme != "dark" && s.Theme != "system" {
		return errors.New("theme must be light, dark or system")
	}
	for _, c := range []error{
		in(s.RefreshSec, 1, 60, "refreshSec"), in(s.SampleSec, 1, 300, "sampleSec"),
		in(s.LogMemoryLines, 100, 100000, "logMemoryLines"), in(s.LogRetentionDays, 1, 365, "logRetentionDays"),
		in(s.LogMaxRows, 1000, 5000000, "logMaxRows"), in(s.ActivityDays, 1, 365, "activityDays"),
		in(s.HealthDays, 1, 365, "healthDays"), in(s.HistoryDays, 1, 730, "historyDays"),
		in(s.ExecRetentionDays, 0, 3650, "execRetentionDays"), in(s.ExecMaxOutputMB, 1, 256, "execMaxOutputMB"),
		in(s.ExecMaxEvents, 1000, 1000000, "execMaxEvents"),
	} {
		if c != nil {
			return c
		}
	}
	for _, k := range s.TrustedKeys {
		if len(k) != 16 || strings.Trim(k, "0123456789abcdef") != "" {
			return fmt.Errorf("trusted key %q must be a 16-character lowercase hex fingerprint", k)
		}
	}
	for _, p := range s.ExtraRedaction {
		if len(p) > 200 {
			return errors.New("redaction pattern too long")
		}
		if err := redact.Validate(p); err != nil {
			return fmt.Errorf("invalid redaction pattern %q: %w", p, err)
		}
	}
	return nil
}

// Event is pushed to subscribers (UI and SSE clients).
type Event struct {
	Topic string `json:"topic"` // log | service | progress | notify
	Data  any    `json:"data"`
}

// Engine owns every subsystem.
type Engine struct {
	Home  string
	DB    *database.DB
	Hub   *logs.Hub
	Sup   *supervisor.Manager
	Proc  *process.Explorer
	Res   *resources.Sampler
	WS    *workspaces.Manager
	Store *store.Store
	Exec  *execution.Manager

	Recovered string // non-empty when the database had to be recreated

	ctx               context.Context
	cancel            context.CancelFunc
	mu                sync.RWMutex
	set               Settings
	subMu             sync.Mutex
	subs              map[int]chan Event
	subSeq            int
	logDone           <-chan struct{}
	closeOnce         sync.Once
	ShutdownRequested chan struct{}

	// DesktopExe is the desktop app binary used for the opt-in "start with Windows"
	// entry. Defaults to RUNTIDE-Desktop.exe next to the running executable.
	DesktopExe string
}

// Options configure New.
type Options struct {
	Home string // overrides RUNTIDE_HOME / default
}

// New opens the database and starts background services (sampler, log
// persistence, retention). Call Close to release everything.
func New(opts Options) (*Engine, error) {
	home := opts.Home
	if home == "" {
		h, err := paths.Home()
		if err != nil {
			return nil, err
		}
		home = h
	} else if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	db, recovered, err := database.Open(paths.DB(home))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{Home: home, DB: db, Recovered: recovered, ctx: ctx, cancel: cancel,
		subs: map[int]chan Event{}, ShutdownRequested: make(chan struct{}, 1)}
	e.set = e.loadSettings()
	e.Hub = logs.NewHub(e.set.LogMemoryLines, e.redactor(e.set))
	e.Sup = supervisor.NewManager(e.Hub, &recorder{db: db, eng: e})
	e.Proc = process.NewExplorer(e.Sup.ManagedPIDs)
	e.Res = resources.NewSampler(900, time.Duration(e.set.SampleSec)*time.Second)
	e.WS = workspaces.NewManager(db, e.Sup)
	e.Store = store.New(db.DB)
	e.Exec = execution.NewManager(e.Store, execution.NewRecorder(e.Store, e.redactor(e.set), e.Publish), e.Sup, e.Hub, home, version.Version)
	e.applyExecLimits(e.set)
	if n, err := e.Store.PurgePhantomReceipts(paths.BackupDir(home)); err == nil && n > 0 {
		e.activity("receipts-cleaned", "", fmt.Sprintf("removed %d leftover entries from comparisons made by an older version", n), "info")
	}
	e.Sup.OnEvent(e.Exec.HandleSupervisorEvent)
	if ids, err := e.Exec.Recover(); err == nil {
		for _, id := range ids {
			e.activity("execution-interrupted", id, "RUNTIDE stopped while this execution was running", "warn")
		}
	}
	e.Res.Start(ctx)
	e.logDone = e.Hub.StartPersistence(ctx, db.DB)
	e.Sup.OnEvent(e.onSupervisorEvent)
	e.forwardLogs()
	go e.retentionLoop()
	if recovered != "" {
		e.activity("database", "", recovered, "warn")
	}
	return e, nil
}

func (e *Engine) redactor(s Settings) *redact.Redactor {
	r := redact.New(s.ExtraRedaction...)
	r.Enabled = s.Redaction
	return r
}

func (e *Engine) loadSettings() Settings {
	s := DefaultSettings()
	var raw string
	if err := e.DB.QueryRow(`SELECT value FROM settings WHERE key='app'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &s) // missing fields keep defaults
	}
	if s.validate() != nil {
		s = DefaultSettings()
	}
	return s
}

// Settings returns the current settings.
func (e *Engine) Settings() Settings {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.set
}

// SetSettings validates, persists and applies settings.
func (e *Engine) SetSettings(s Settings) error {
	if err := s.validate(); err != nil {
		return err
	}
	if old := e.Settings(); old.StartWithWindows != s.StartWithWindows {
		if err := e.applyAutostart(s.StartWithWindows); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(s)
	if _, err := e.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('app',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, string(b), time.Now().Unix()); err != nil {
		return err
	}
	e.mu.Lock()
	e.set = s
	e.mu.Unlock()
	e.Hub.SetRedactor(e.redactor(s))
	e.Exec.Rec.Red = e.redactor(s)
	e.applyExecLimits(s)
	e.Res.SetInterval(time.Duration(s.SampleSec) * time.Second)
	return nil
}

func (e *Engine) applyExecLimits(s Settings) {
	e.Exec.Rec.SetLimits(execution.Limits{MaxCommandOutputBytes: int64(s.ExecMaxOutputMB) << 20, MaxEvents: int64(s.ExecMaxEvents)})
}

// PruneExecutions removes ended executions older than days (dryRun counts only).
func (e *Engine) PruneExecutions(days int, dryRun bool) (int64, error) {
	if days < 1 {
		return 0, errors.New("days must be at least 1")
	}
	return e.Store.PruneExecutions(time.Now().Add(-time.Duration(days)*24*time.Hour), dryRun)
}

// Subscribe returns a channel of events and a cancel function.
func (e *Engine) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 1024)
	e.subMu.Lock()
	e.subSeq++
	id := e.subSeq
	e.subs[id] = ch
	e.subMu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			e.subMu.Lock()
			delete(e.subs, id)
			e.subMu.Unlock()
		})
	}
}

// Publish broadcasts an event, dropping it for subscribers that fall behind.
func (e *Engine) Publish(topic string, data any) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	for _, ch := range e.subs {
		select {
		case ch <- Event{topic, data}:
		default:
		}
	}
}

func (e *Engine) forwardLogs() {
	ch, cancel := e.Hub.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-e.ctx.Done():
				return
			case en := <-ch:
				e.Publish("log", en)
			}
		}
	}()
}

func (e *Engine) onSupervisorEvent(ev supervisor.Event) {
	lvl := ev.Level
	if lvl == "warn" || lvl == "error" || ev.Kind == "started" || ev.Kind == "stopped" {
		e.activity(ev.Kind, ev.Key, ev.Detail, lvl)
	}
	e.Publish("service", ev)
	if lvl == "error" || (ev.Kind == "health" && lvl == "warn") {
		e.Publish("notify", map[string]string{"title": "RUNTIDE: " + ev.Key, "body": ev.Detail, "level": lvl})
	}
}

func (e *Engine) activity(kind, subject, detail, level string) {
	_, _ = e.DB.Exec(`INSERT INTO activity(ts,kind,subject,detail,level) VALUES(?,?,?,?,?)`, time.Now().Unix(), kind, subject, detail, level)
}

func (e *Engine) retentionLoop() {
	run := func() {
		s := e.Settings()
		_, _ = e.DB.Prune(database.Retention{LogDays: s.LogRetentionDays, LogMaxRows: s.LogMaxRows,
			ActivityDays: s.ActivityDays, HealthDays: s.HealthDays, RunDays: s.HistoryDays, HistoryDays: s.HistoryDays})
		if s.ExecRetentionDays > 0 {
			_, _ = e.PruneExecutions(s.ExecRetentionDays, false)
		}
	}
	run()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// Close stops services (when stopServices is true), flushes logs, closes the DB.
func (e *Engine) Close(stopServices bool) error {
	var err error
	e.closeOnce.Do(func() {
		if stopServices {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			err = e.Sup.Shutdown(ctx)
			cancel()
		}
		e.cancel()
		e.Res.Stop()
		<-e.logDone
		if cerr := e.DB.Close(); err == nil {
			err = cerr
		}
	})
	return err
}

// ---- run history recorder ----

type recorder struct {
	db  *database.DB
	eng *Engine
}

func (r *recorder) RunStarted(key string, pid int) int64 {
	res, err := r.db.Exec(`INSERT INTO service_runs(service,started_at,pid) VALUES(?,?,?)`, key, time.Now().Unix(), pid)
	if err != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

func (r *recorder) RunEnded(id int64, code int, reason string) {
	if id == 0 {
		return
	}
	_, _ = r.db.Exec(`UPDATE service_runs SET ended_at=?, exit_code=?, reason=? WHERE id=?`, time.Now().Unix(), code, reason, id)
}

func (r *recorder) HealthEvent(key, status, detail string) {
	_, _ = r.db.Exec(`INSERT INTO health_events(service,ts,status,detail) VALUES(?,?,?,?)`, key, time.Now().Unix(), status, detail)
}

// ---- standalone services ----

// ServiceView is a service (standalone or workspace member) with runtime state.
type ServiceView struct {
	Key       string             `json:"key"`
	Workspace string             `json:"workspace"`
	Name      string             `json:"name"`
	Spec      config.ServiceSpec `json:"spec"`
	Root      string             `json:"root"`
	Runtime   *supervisor.Info   `json:"runtime,omitempty"`
}

func (e *Engine) standalone() ([]config.ServiceSpec, error) {
	rows, err := e.DB.Query(`SELECT spec_json FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.ServiceSpec
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			return nil, err
		}
		var s config.ServiceSpec
		if json.Unmarshal([]byte(js), &s) == nil {
			s.ApplyDefaults()
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// Services lists standalone and workspace services with live state.
func (e *Engine) Services() ([]ServiceView, error) {
	var out []ServiceView
	st, err := e.standalone()
	if err != nil {
		return nil, err
	}
	for _, s := range st {
		v := ServiceView{Key: s.Name, Name: s.Name, Spec: s}
		if inf, ok := e.Sup.Get(s.Name); ok {
			i := inf
			v.Runtime = &i
		}
		out = append(out, v)
	}
	wss, err := e.WS.List()
	if err != nil {
		return nil, err
	}
	for _, w := range wss {
		if w.Spec == nil {
			continue
		}
		for _, s := range w.Spec.Services {
			k := workspaces.Key(w.Name, s.Name)
			v := ServiceView{Key: k, Workspace: w.Name, Name: s.Name, Spec: s, Root: w.Root}
			if inf, ok := e.Sup.Get(k); ok {
				i := inf
				v.Runtime = &i
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// ResolveService maps a user-supplied name ("api" or "shop/api") to a view.
func (e *Engine) ResolveService(name string) (*ServiceView, error) {
	all, err := e.Services()
	if err != nil {
		return nil, err
	}
	var matches []*ServiceView
	for i := range all {
		if all[i].Key == name {
			return &all[i], nil
		}
		if all[i].Name == name {
			matches = append(matches, &all[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("unknown service %q", name)
	case 1:
		return matches[0], nil
	}
	var keys []string
	for _, m := range matches {
		keys = append(keys, m.Key)
	}
	return nil, fmt.Errorf("service name %q is ambiguous; use one of: %s", name, strings.Join(keys, ", "))
}

// SaveService creates or updates a standalone service.
func (e *Engine) SaveService(spec config.ServiceSpec) error {
	spec.ApplyDefaults()
	if strings.Contains(spec.Name, "/") {
		return errors.New("service name must not contain '/'")
	}
	if probs := config.ValidateService(&spec, ""); len(probs) > 0 {
		return &config.ValidationError{Problems: probs}
	}
	if inf, ok := e.Sup.Get(spec.Name); ok && (inf.State == supervisor.StateRunning || inf.State == supervisor.StateStarting) {
		return fmt.Errorf("service %s is running; stop it before editing", spec.Name)
	}
	if spec.WorkingDirectory != "" {
		if st, err := os.Stat(spec.WorkingDirectory); err != nil || !st.IsDir() {
			return fmt.Errorf("working directory %q does not exist", spec.WorkingDirectory)
		}
	}
	b, _ := json.Marshal(spec)
	now := time.Now().Unix()
	_, err := e.DB.Exec(`INSERT INTO services(name,spec_json,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET spec_json=excluded.spec_json, updated_at=excluded.updated_at`, spec.Name, string(b), now, now)
	return err
}

// DeleteService removes a standalone service.
func (e *Engine) DeleteService(name string) error {
	if inf, ok := e.Sup.Get(name); ok && (inf.State == supervisor.StateRunning || inf.State == supervisor.StateStarting || inf.State == supervisor.StateBackoff) {
		return fmt.Errorf("service %s is running; stop it first", name)
	}
	res, err := e.DB.Exec(`DELETE FROM services WHERE name=?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown standalone service %q", name)
	}
	_ = e.Sup.Forget(name)
	return nil
}

// StartService starts a service by name/key. Workspace services must be
// approved through their workspace first.
func (e *Engine) StartService(name string) error {
	v, err := e.ResolveService(name)
	if err != nil {
		return err
	}
	if v.Workspace != "" {
		rec, err := e.WS.Get(v.Workspace)
		if err != nil {
			return err
		}
		if !rec.Approved {
			return workspaces.ErrNotApproved
		}
	}
	return e.Sup.Start(v.Key, v.Spec, v.Root)
}

// StopService stops a service.
func (e *Engine) StopService(name string) error {
	v, err := e.ResolveService(name)
	if err != nil {
		return err
	}
	return e.Sup.Stop(v.Key)
}

// RestartService restarts a service.
func (e *Engine) RestartService(name string) error {
	v, err := e.ResolveService(name)
	if err != nil {
		return err
	}
	if v.Runtime == nil {
		return e.StartService(name)
	}
	return e.Sup.Restart(v.Key)
}

// DataPath helper for backups.
func (e *Engine) backupPath() string {
	return filepath.Join(paths.BackupDir(e.Home), "runtide-"+time.Now().Format("20060102-150405.000")+".db")
}

// applyAutostart registers or removes the per-user Run entry. It is only ever
// called when the user toggles the setting; it never runs implicitly.
func (e *Engine) applyAutostart(enable bool) error {
	target := e.DesktopExe
	if target == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		target = filepath.Join(filepath.Dir(exe), "RUNTIDE-Desktop.exe")
	}
	if enable {
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("desktop app not found at %s; start-with-Windows needs RUNTIDE-Desktop.exe", target)
		}
	}
	return winapi.SetAutostart("RUNTIDE", `"`+target+`" --background`, enable)
}
