package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/diagnostics"
	"github.com/runtide/runtide/internal/docker"
	"github.com/runtide/runtide/internal/envfile"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/ports"
	"github.com/runtide/runtide/internal/process"
	"github.com/runtide/runtide/internal/resources"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/runtide/runtide/internal/tools"
	"github.com/runtide/runtide/internal/winservices"
	"github.com/runtide/runtide/internal/workspaces"
	"github.com/runtide/runtide/internal/wsl"
)

// Dispatch executes a named operation. It is the single entry point used by
// the desktop bindings, the loopback API and the CLI.
func (e *Engine) Dispatch(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	p := func(v any) error {
		if len(raw) == 0 || string(raw) == "null" {
			return nil
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			return fmt.Errorf("invalid parameters for %s: %w", method, err)
		}
		return nil
	}
	switch method {
	case "version":
		return map[string]any{"version": Version, "home": e.Home, "elevated": isElevated()}, nil
	case "overview":
		return e.Overview(ctx)

	// ---- processes
	case "processes.list":
		var a struct {
			Query   string `json:"query"`
			CmdLine bool   `json:"cmdLine"`
			Wait    bool   `json:"wait"` // take two samples so CPU% is meaningful (CLI)
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		var ps []process.Process
		var err error
		if a.Wait {
			ps, err = e.Proc.SnapshotWithCPU(400*time.Millisecond, a.CmdLine)
		} else {
			ps, err = e.Proc.Snapshot(a.CmdLine)
		}
		return process.Filter(ps, a.Query), err
	case "processes.tree":
		ps, err := e.Proc.Snapshot(true)
		return process.BuildTree(ps), err
	case "processes.kill":
		var a struct {
			PID       uint32    `json:"pid"`
			StartTime time.Time `json:"startTime"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		res, err := e.Proc.Terminate(a.PID, a.StartTime)
		e.activity("process-kill", fmt.Sprint(a.PID), res.Name+": "+res.Detail, map[bool]string{true: "info", false: "warn"}[res.Terminated])
		return res, err
	case "ports.list":
		var a struct {
			Query string `json:"query"`
			UDP   bool   `json:"udp"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		ps, err := e.Proc.Snapshot(false)
		if err != nil {
			return nil, err
		}
		ls, err := ports.List(ps, a.UDP)
		return ports.Filter(ls, a.Query), err

	// ---- services
	case "services.list":
		return e.Services()
	case "services.save":
		var s config.ServiceSpec
		if err := p(&s); err != nil {
			return nil, err
		}
		return nil, e.SaveService(s)
	case "services.delete", "services.start", "services.stop", "services.restart":
		var a struct {
			Key string `json:"key"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		switch method {
		case "services.delete":
			return nil, e.DeleteService(a.Key)
		case "services.start":
			return nil, e.StartService(a.Key)
		case "services.stop":
			return nil, e.StopService(a.Key)
		}
		return nil, e.RestartService(a.Key)
	case "services.runs":
		var a struct {
			Key   string `json:"key"`
			Limit int    `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.runs(a.Key, a.Limit)

	// ---- native windows services (read-only unless explicitly controlled)
	case "winservices.list":
		return winservices.List()
	case "winservices.control":
		var a struct {
			Name    string `json:"name"`
			Action  string `json:"action"`
			Confirm bool   `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		if !a.Confirm {
			return nil, errors.New("changing a Windows service requires explicit confirmation")
		}
		err := winservices.Control(a.Name, a.Action)
		e.activity("winservice-"+a.Action, a.Name, fmt.Sprint(err), map[bool]string{true: "warn", false: "info"}[err != nil])
		return nil, err

	// ---- workspaces
	case "workspaces.list":
		return e.workspaceList()
	case "workspaces.get":
		return e.WS.Get(nameArg(raw))
	case "workspaces.status":
		return e.WS.Status(nameArg(raw))
	case "workspaces.inspect":
		var a struct{ YAML, Root string }
		if err := p(&a); err != nil {
			return nil, err
		}
		pv, _, err := e.WS.Inspect(a.YAML, a.Root)
		return pv, err
	case "workspaces.save":
		var a struct {
			YAML    string `json:"yaml"`
			Root    string `json:"root"`
			Replace bool   `json:"replace"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.WS.Save(a.YAML, a.Root, a.Replace)
	case "workspaces.approve":
		return nil, e.WS.Approve(nameArg(raw))
	case "workspaces.delete":
		return nil, e.WS.Delete(nameArg(raw))
	case "workspaces.export":
		return e.WS.Export(nameArg(raw))
	case "workspaces.history":
		var a struct {
			Name  string `json:"name"`
			Limit int    `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.WS.History(a.Name, a.Limit)
	case "workspaces.up", "workspaces.down", "workspaces.restart":
		var a struct {
			Name string   `json:"name"`
			Only []string `json:"only"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		prog := func(pr workspaces.Progress) { e.Publish("progress", pr) }
		bg := context.WithoutCancel(ctx) // operations survive a disconnected client
		switch method {
		case "workspaces.up":
			return nil, e.WS.Up(bg, a.Name, a.Only, prog)
		case "workspaces.down":
			return nil, e.WS.Down(bg, a.Name, prog)
		}
		return nil, e.WS.Restart(bg, a.Name, prog)

	// ---- logs
	case "logs.recent":
		var a struct {
			Services []string `json:"services"`
			N        int      `json:"n"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.Hub.Recent(a.N, a.Services...), nil
	case "logs.query":
		var a struct {
			Services []string  `json:"services"`
			Since    time.Time `json:"since"`
			Search   string    `json:"search"`
			Level    string    `json:"level"`
			Limit    int       `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return logs.QueryDB(e.DB.DB, logs.Query{Services: a.Services, Since: a.Since, Search: a.Search, Level: a.Level, Limit: a.Limit})
	case "logs.services":
		s := e.Hub.Services()
		sort.Strings(s)
		return s, nil
	case "logs.clear":
		var a struct {
			Services []string `json:"services"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		e.Hub.Clear(a.Services...)
		return nil, nil
	case "logs.export":
		var a struct {
			Services []string `json:"services"`
			N        int      `json:"n"`
			Path     string   `json:"path"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.exportLogs(a.Services, a.N, a.Path)

	// ---- resources
	case "resources.current":
		return e.currentResources(), nil
	case "resources.history":
		var a struct {
			N int `json:"n"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.Res.History(a.N), nil

	// ---- environment
	case "tools.list":
		return e.toolList(ctx), nil
	case "env.inspect":
		var a struct {
			Dir string `json:"dir"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return envfile.Inspect(a.Dir)
	case "env.compare":
		var a struct{ A, B string }
		if err := p(&a); err != nil {
			return nil, err
		}
		fa, err := envfile.Parse(a.A)
		if err != nil {
			return nil, err
		}
		fb, err := envfile.Parse(a.B)
		if err != nil {
			return nil, err
		}
		return envfile.Compare(fa, fb), nil
	case "env.generateExample":
		var a struct {
			Source   string `json:"source"`
			KeepSafe bool   `json:"keepSafe"`
			Write    bool   `json:"write"` // false = preview only
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.generateExample(a.Source, a.KeepSafe, a.Write)

	// ---- diagnostics
	case "doctor.run":
		return e.Doctor(ctx)
	case "doctor.plan":
		var a struct {
			FixID string `json:"fixId"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return diagnostics.PlanFix(a.FixID, e.workspaceRoots())
	case "doctor.apply":
		var a struct {
			FixID   string `json:"fixId"`
			Confirm bool   `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		if !a.Confirm {
			return nil, errors.New("applying a fix requires explicit confirmation")
		}
		plan, err := diagnostics.ApplyFix(a.FixID, e.workspaceRoots())
		e.activity("doctor-fix", a.FixID, fmt.Sprint(err), map[bool]string{true: "warn", false: "info"}[err != nil])
		return plan, err

	// ---- docker & wsl
	case "docker.status":
		return docker.GetStatus(ctx), nil
	case "docker.list":
		cs, err := docker.List(ctx, true)
		if err != nil {
			return nil, err
		}
		return docker.WithStats(ctx, cs), nil
	case "docker.start", "docker.stop":
		var a struct {
			ID string `json:"id"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		var err error
		if method == "docker.start" {
			err = docker.Start(ctx, a.ID)
		} else {
			err = docker.Stop(ctx, a.ID)
		}
		e.activity(method, a.ID, fmt.Sprint(err), "info")
		return nil, err
	case "wsl.status":
		return wsl.GetStatus(ctx), nil

	// ---- settings, data, activity
	case "settings.get":
		return e.Settings(), nil
	case "settings.set":
		s := e.Settings()
		if err := p(&s); err != nil {
			return nil, err
		}
		if err := e.SetSettings(s); err != nil {
			return nil, err
		}
		return s, nil
	case "db.info":
		return e.dbInfo()
	case "db.backup":
		dst := e.backupPath()
		if err := e.DB.Backup(dst); err != nil {
			return nil, err
		}
		return map[string]string{"path": dst}, nil
	case "db.backups":
		return e.listBackups(), nil
	case "db.reset":
		var a struct {
			Confirm bool `json:"confirm"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		if !a.Confirm {
			return nil, errors.New("reset requires explicit confirmation")
		}
		if len(e.Sup.ManagedPIDs()) > 0 {
			return nil, errors.New("stop all services before resetting")
		}
		dst := e.backupPath()
		if err := e.DB.Backup(dst); err != nil {
			return nil, fmt.Errorf("could not create safety backup: %w", err)
		}
		if err := e.DB.Reset(ctx); err != nil {
			return nil, err
		}
		e.mu.Lock()
		e.set = DefaultSettings()
		e.mu.Unlock()
		return map[string]string{"backup": dst}, nil
	case "activity.list":
		var a struct {
			Limit int `json:"limit"`
		}
		if err := p(&a); err != nil {
			return nil, err
		}
		return e.activityList(a.Limit)
	case "system.shutdown":
		select {
		case e.ShutdownRequested <- struct{}{}:
		default:
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

func nameArg(raw json.RawMessage) string {
	var a struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.Name
}

// ---- helpers ----

func (e *Engine) workspaceList() (any, error) {
	recs, err := e.WS.List()
	if err != nil {
		return nil, err
	}
	type row struct {
		*workspaces.Record
		Health  string `json:"health"`
		Running int    `json:"running"`
		Total   int    `json:"total"`
	}
	out := make([]row, 0, len(recs))
	for _, r := range recs {
		x := row{Record: r, Health: "stopped"}
		if st, err := e.WS.Status(r.Name); err == nil {
			x.Health, x.Running, x.Total = st.Health, st.Running, st.Total
		}
		out = append(out, x)
	}
	return out, nil
}

func (e *Engine) workspaceRoots() []string {
	recs, _ := e.WS.List()
	var r []string
	for _, w := range recs {
		r = append(r, w.Root)
	}
	return r
}

func (e *Engine) runs(key string, limit int) (any, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := e.DB.Query(`SELECT service,started_at,ended_at,exit_code,reason,pid FROM service_runs WHERE (?='' OR service=?) ORDER BY id DESC LIMIT ?`, key, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type run struct {
		Service  string     `json:"service"`
		Started  time.Time  `json:"startedAt"`
		Ended    *time.Time `json:"endedAt"`
		ExitCode *int       `json:"exitCode"`
		Reason   string     `json:"reason"`
		PID      int        `json:"pid"`
	}
	out := []run{}
	for rows.Next() {
		var r run
		var s int64
		var en, ec *int64
		if err := rows.Scan(&r.Service, &s, &en, &ec, &r.Reason, &r.PID); err != nil {
			return nil, err
		}
		r.Started = time.Unix(s, 0)
		if en != nil {
			t := time.Unix(*en, 0)
			r.Ended = &t
		}
		if ec != nil {
			c := int(*ec)
			r.ExitCode = &c
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (e *Engine) activityList(limit int) (any, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := e.DB.Query(`SELECT ts,kind,subject,detail,level FROM activity ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type act struct {
		Time    time.Time `json:"time"`
		Kind    string    `json:"kind"`
		Subject string    `json:"subject"`
		Detail  string    `json:"detail"`
		Level   string    `json:"level"`
	}
	out := []act{}
	for rows.Next() {
		var a act
		var ts int64
		if err := rows.Scan(&ts, &a.Kind, &a.Subject, &a.Detail, &a.Level); err != nil {
			return nil, err
		}
		a.Time = time.Unix(ts, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (e *Engine) exportLogs(services []string, n int, path string) (any, error) {
	var b strings.Builder
	for _, en := range e.Hub.Recent(n, services...) {
		fmt.Fprintf(&b, "%s [%s] %s %s\n", en.Time.Format("2006-01-02 15:04:05.000"), en.Service, en.Stream, en.Line)
	}
	if path == "" {
		return b.String(), nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("export path must be absolute")
	}
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("%s already exists; choose another file name", path)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}

// CurrentResources is the payload of resources.current.
type CurrentResources struct {
	Sample     *resources.Sample `json:"sample"`
	Disks      []resources.Disk  `json:"disks"`
	Interfaces []resources.Iface `json:"interfaces"`
	IntervalS  float64           `json:"intervalSec"`
}

func (e *Engine) currentResources() CurrentResources {
	c := CurrentResources{Disks: resources.Disks(), Interfaces: resources.Interfaces(), IntervalS: e.Res.Interval().Seconds()}
	if s, ok := e.Res.Latest(); ok {
		c.Sample = &s
	}
	return c
}

func (e *Engine) toolList(ctx context.Context) []tools.Tool {
	ts := tools.Detect(ctx)
	ds := docker.GetStatus(ctx)
	desk := tools.Tool{ID: "docker-desktop", Name: "Docker Desktop", Category: "containers", Installed: ds.DesktopInstalled,
		Method: "known-location", Note: ds.Message}
	if ds.DesktopInstalled {
		desk.Path = `C:\Program Files\Docker\Docker\Docker Desktop.exe`
		desk.Version = ds.ServerVersion
	}
	ws := wsl.GetStatus(ctx)
	w := tools.Tool{ID: "wsl", Name: "WSL", Category: "virtualization", Installed: ws.Installed && len(ws.Distros) > 0, Path: ws.Path, Method: "PATH", OnPath: ws.Path != "", Note: ws.Message}
	if len(ws.Distros) > 0 {
		w.Version = fmt.Sprintf("%d distribution(s)", len(ws.Distros))
	}
	return append(ts, desk, w)
}

func (e *Engine) generateExample(source string, keepSafe, write bool) (any, error) {
	if !filepath.IsAbs(source) {
		return nil, errors.New("source must be an absolute path")
	}
	base := filepath.Base(source)
	if !strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".example") || strings.HasSuffix(base, ".sample") {
		return nil, errors.New("source must be an env file such as .env or .env.local")
	}
	content, err := envfile.GenerateExample(source, keepSafe)
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(filepath.Dir(source), ".env.example")
	res := map[string]any{"content": content, "destination": dest, "written": false}
	if !write {
		return res, nil
	}
	backup, err := envfile.WriteWithBackup(dest, content)
	if err != nil {
		return nil, err
	}
	res["written"], res["backup"] = true, backup
	e.activity("env-example", dest, "generated", "info")
	return res, nil
}

func (e *Engine) dbInfo() (any, error) {
	v, err := e.DB.SchemaVersion()
	if err != nil {
		return nil, err
	}
	var size int64
	if st, err := os.Stat(e.DB.Path); err == nil {
		size = st.Size()
	}
	counts := map[string]int{}
	for _, t := range []string{"workspaces", "services", "logs", "activity", "service_runs", "health_events"} {
		var n int
		_ = e.DB.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n)
		counts[t] = n
	}
	return map[string]any{"path": e.DB.Path, "schemaVersion": v, "sizeBytes": size, "rows": counts, "recovered": e.Recovered}, nil
}

func (e *Engine) listBackups() []map[string]any {
	dir := filepath.Join(e.Home, "backups")
	ents, _ := os.ReadDir(dir)
	out := []map[string]any{}
	for _, en := range ents {
		if info, err := en.Info(); err == nil && !en.IsDir() {
			out = append(out, map[string]any{"path": filepath.Join(dir, en.Name()), "sizeBytes": info.Size(), "time": info.ModTime()})
		}
	}
	return out
}

// RestoreDatabase swaps in a backup. It must only be called while no services
// are running, and the engine must be restarted afterwards (the CLI does so).
func RestoreDatabase(home, backup string) (string, error) {
	return database.Restore(filepath.Join(home, "runtide.db"), backup)
}

// Doctor gathers real state and runs the diagnostic rules.
func (e *Engine) Doctor(ctx context.Context) (diagnostics.Report, error) {
	in := diagnostics.Inputs{DataDir: e.Home, Runtime: map[string]supervisor.Info{}}
	svcs, err := e.Services()
	if err != nil {
		return diagnostics.Report{}, err
	}
	for _, s := range svcs {
		in.Targets = append(in.Targets, diagnostics.Target{Workspace: s.Workspace, Root: s.Root, Spec: s.Spec, Key: s.Key})
		if s.Runtime != nil {
			in.Runtime[s.Key] = *s.Runtime
		}
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(s.Spec.Command), ".exe"), "wsl") {
			in.WSLUsed = true
		}
	}
	in.Roots = e.workspaceRoots()
	if ps, err := e.Proc.Snapshot(false); err == nil {
		in.Listeners, _ = ports.List(ps, false)
	}
	in.Tools = tools.Detect(ctx)
	in.Docker = docker.GetStatus(ctx)
	in.WSL = wsl.GetStatus(ctx)
	in.LastLogs = func(key string, n int) []string {
		var out []string
		for _, en := range e.Hub.Recent(n, key) {
			out = append(out, en.Line)
		}
		return out
	}
	rep := diagnostics.Run(ctx, in)
	if b, err := json.Marshal(rep); err == nil {
		_, _ = e.DB.Exec(`INSERT INTO diagnostic_history(ts,findings,errors,warnings,report_json) VALUES(?,?,?,?,?)`, time.Now().Unix(), len(rep.Findings), rep.Errors, rep.Warnings, string(b))
	}
	return rep, nil
}

// Overview is the dashboard payload; every field is measured, nothing is estimated.
type Overview struct {
	Processes   int               `json:"processes"`
	Listening   int               `json:"listening"`
	Services    ServiceCounts     `json:"services"`
	Resources   CurrentResources  `json:"resources"`
	Recent      []process.Process `json:"recentProcesses"`
	Failures    any               `json:"recentFailures"`
	Workspaces  []WorkspaceBrief  `json:"workspaces"`
	Warnings    []string          `json:"warnings"`
	GeneratedAt time.Time         `json:"generatedAt"`
}

// ServiceCounts summarises managed services.
type ServiceCounts struct {
	Total, Running, Failed, Unhealthy int
}

// WorkspaceBrief is a compact workspace status.
type WorkspaceBrief struct {
	Name    string `json:"name"`
	Health  string `json:"health"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
}

// Overview builds the dashboard.
func (e *Engine) Overview(ctx context.Context) (*Overview, error) {
	o := &Overview{GeneratedAt: time.Now(), Warnings: []string{}}
	ps, err := e.Proc.Snapshot(false)
	if err != nil {
		return nil, err
	}
	o.Processes = len(ps)
	ls, _ := ports.List(ps, false)
	o.Listening = len(ls)
	o.Resources = e.currentResources()
	// recently started: newest non-system processes
	var user []process.Process
	for _, p := range ps {
		if p.Protection == process.ProtNone && !p.StartTime.IsZero() {
			user = append(user, p)
		}
	}
	sort.Slice(user, func(i, j int) bool { return user[i].StartTime.After(user[j].StartTime) })
	if len(user) > 8 {
		user = user[:8]
	}
	o.Recent = user
	svcs, _ := e.Services()
	for _, s := range svcs {
		if s.Runtime == nil {
			continue
		}
		o.Services.Total++
		switch s.Runtime.State {
		case supervisor.StateRunning:
			o.Services.Running++
		case supervisor.StateFailed:
			o.Services.Failed++
		}
		if s.Runtime.Health == "unhealthy" {
			o.Services.Unhealthy++
		}
	}
	o.Services.Total = len(svcs)
	if wl, err := e.WS.List(); err == nil {
		for _, w := range wl {
			if st, err := e.WS.Status(w.Name); err == nil {
				o.Workspaces = append(o.Workspaces, WorkspaceBrief{w.Name, st.Health, st.Running, st.Total})
			}
		}
	}
	o.Failures, _ = e.recentFailures()
	if s := o.Resources.Sample; s != nil && s.MemPercent > 90 {
		o.Warnings = append(o.Warnings, fmt.Sprintf("Memory usage is high (%.0f%%).", s.MemPercent))
	}
	for _, d := range o.Resources.Disks {
		if d.Total > 0 && float64(d.Free)/float64(d.Total) < 0.10 {
			o.Warnings = append(o.Warnings, fmt.Sprintf("Drive %s has less than 10%% free space.", d.Path))
		}
	}
	if o.Services.Failed > 0 {
		o.Warnings = append(o.Warnings, fmt.Sprintf("%d managed service(s) have failed.", o.Services.Failed))
	}
	if o.Services.Unhealthy > 0 {
		o.Warnings = append(o.Warnings, fmt.Sprintf("%d managed service(s) are unhealthy.", o.Services.Unhealthy))
	}
	for _, l := range ls {
		if l.Conflict != "" && l.Exposure != "loopback" {
			o.Warnings = append(o.Warnings, fmt.Sprintf("Port %d is bound by multiple processes.", l.Port))
			break
		}
	}
	if o.Warnings == nil {
		o.Warnings = []string{}
	}
	return o, nil
}

func (e *Engine) recentFailures() (any, error) {
	rows, err := e.DB.Query(`SELECT ts,kind,subject,detail FROM activity WHERE level='error' ORDER BY id DESC LIMIT 6`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type f struct {
		Time    time.Time `json:"time"`
		Kind    string    `json:"kind"`
		Subject string    `json:"subject"`
		Detail  string    `json:"detail"`
	}
	out := []f{}
	for rows.Next() {
		var x f
		var ts int64
		if err := rows.Scan(&ts, &x.Kind, &x.Subject, &x.Detail); err != nil {
			return nil, err
		}
		x.Time = time.Unix(ts, 0)
		out = append(out, x)
	}
	return out, rows.Err()
}
