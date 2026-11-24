// Package desktop contains the Wails-facing application object. It holds no
// business logic: every call is forwarded to the engine (in-process, or a
// running background engine over the loopback API).
package desktop

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/runtide/runtide/internal/version"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/api"
	"github.com/runtide/runtide/internal/app"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed tray.png
var trayPNG []byte

// App is bound to the frontend.
type App struct {
	ctx    context.Context
	eng    *app.Engine // set in local mode
	client *api.Client // set in remote mode (attached to an existing engine)
	srv    *api.Server // loopback API published by the local engine
	tray   *winapi.Tray
	quit   bool
	mu     sync.Mutex
	Hidden bool // start minimised to tray
}

// New attaches to a running engine when one exists, otherwise creates one and
// publishes the loopback API so the CLI can reach the same engine.
func New() (*App, error) {
	a := &App{}
	home := ""
	if c, err := api.Connect(homeDir()); err == nil {
		a.client = c
		return a, nil
	}
	_ = home
	e, err := app.New(app.Options{})
	if err != nil {
		return nil, err
	}
	if exe, err := os.Executable(); err == nil {
		e.DesktopExe = exe
	}
	srv, err := api.Serve(e, "desktop")
	if err != nil && !errors.Is(err, api.ErrAlreadyRunning) {
		_ = e.Close(false)
		return nil, err
	}
	a.eng, a.srv = e, srv
	return a, nil
}

func homeDir() string {
	if d := os.Getenv("RUNTIDE_HOME"); d != "" {
		return d
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Runtide")
}

// Mode reports "local" or "attached" (to an already running engine).
func (a *App) Mode() string {
	if a.client != nil {
		return "attached"
	}
	return "local"
}

// Startup is the Wails OnStartup hook.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	go a.forwardEvents()
	t, err := winapi.NewTray("RUNTIDE", trayPNG, a.Show, []winapi.TrayItem{
		{Label: "Open RUNTIDE", OnClick: a.Show},
		{Separator: true},
		{Label: "Stop all services", OnClick: a.stopAll},
		{Separator: true},
		{Label: "Quit RUNTIDE", OnClick: a.Quit},
	})
	if err == nil {
		a.tray = t
	}
	if a.Hidden && a.tray != nil {
		wruntime.WindowHide(ctx)
	}
}

// BeforeClose implements minimise-to-tray. Returning true prevents closing.
func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.quit || a.tray == nil {
		return false
	}
	if s, err := a.settings(); err == nil && s.MinimizeToTray {
		wruntime.WindowHide(ctx)
		return true
	}
	return false
}

// Shutdown is the Wails OnShutdown hook: graceful cleanup of every subsystem.
func (a *App) Shutdown(ctx context.Context) {
	if a.tray != nil {
		a.tray.Close()
	}
	if a.srv != nil {
		a.srv.Close()
	}
	if a.eng != nil {
		_ = a.eng.Close(true)
	}
}

// Show restores the window from the tray.
func (a *App) Show() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
}

// Quit really exits the application (stopping managed services).
func (a *App) Quit() {
	a.mu.Lock()
	a.quit = true
	a.mu.Unlock()
	wruntime.Quit(a.ctx)
}

func (a *App) stopAll() {
	var svcs []app.ServiceView
	if err := a.rpc("services.list", nil, &svcs); err != nil {
		return
	}
	for _, s := range svcs {
		if s.Runtime != nil && s.Runtime.PID > 0 {
			_ = a.rpc("services.stop", map[string]string{"key": s.Key}, nil)
		}
	}
}

func (a *App) settings() (app.Settings, error) {
	var s app.Settings
	err := a.rpc("settings.get", nil, &s)
	return s, err
}

func (a *App) rpc(method string, params, out any) error {
	if a.client != nil {
		return a.client.Call(method, params, out)
	}
	pb, _ := json.Marshal(params)
	res, err := a.eng.Dispatch(context.Background(), method, pb)
	if err != nil || out == nil || res == nil {
		return err
	}
	b, _ := json.Marshal(res)
	return json.Unmarshal(b, out)
}

// Call is the single frontend entry point: method + JSON params in, JSON out.
func (a *App) Call(method, params string) (string, error) {
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	if a.client != nil {
		var out json.RawMessage
		if err := a.client.Call(method, raw, &out); err != nil {
			return "", err
		}
		return string(out), nil
	}
	res, err := a.eng.Dispatch(a.ctx, method, raw)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	return string(b), err
}

// Info describes how the UI is connected.
func (a *App) Info() map[string]any {
	return map[string]any{"mode": a.Mode(), "version": version.Version}
}

// PickDirectory opens a native folder chooser.
func (a *App) PickDirectory(title string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title})
}

// PickFile opens a native file chooser.
func (a *App) PickFile(title string) (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{Title: title})
}

// PickSaveFile opens a native save dialog.
func (a *App) PickSaveFile(title, defaultName string) (string, error) {
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: title, DefaultFilename: defaultName})
}

// ReadTextFile reads a user-chosen text file (size-limited).
func (a *App) ReadTextFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > 1<<20 {
		return "", errors.New("file is larger than 1 MB")
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// forwardEvents relays engine events to the UI. Log lines are batched to keep
// the IPC channel quiet under heavy output.
func (a *App) forwardEvents() {
	type ev struct {
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
	}
	in := make(chan ev, 4096)
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	if a.client != nil {
		go func() {
			for ctx.Err() == nil {
				_ = a.client.Events(ctx, func(topic string, data json.RawMessage) {
					select {
					case in <- ev{topic, data}:
					default:
					}
				})
				time.Sleep(2 * time.Second)
			}
		}()
	} else {
		ch, unsub := a.eng.Subscribe()
		defer unsub()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-ch:
					b, _ := json.Marshal(e.Data)
					select {
					case in <- ev{e.Topic, b}:
					default:
					}
				}
			}
		}()
	}
	var logs, outs []json.RawMessage
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	flush := func() {
		if len(logs) > 0 {
			wruntime.EventsEmit(a.ctx, "log-batch", logs)
			logs = nil
		}
		if len(outs) > 0 {
			wruntime.EventsEmit(a.ctx, "exec-output-batch", outs)
			outs = nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-in:
			switch e.Topic {
			case "log":
				logs = append(logs, e.Data)
				if len(logs) >= 500 {
					flush()
				}
			case "exec.output":
				outs = append(outs, e.Data)
				if len(outs) >= 500 {
					flush()
				}
			case "notify":
				a.notify(e.Data)
				wruntime.EventsEmit(a.ctx, "runtide", e.Topic, e.Data)
			default:
				wruntime.EventsEmit(a.ctx, "runtide", e.Topic, e.Data)
			}
		case <-t.C:
			flush()
		}
	}
}

func (a *App) notify(data json.RawMessage) {
	var n struct{ Title, Body string }
	if json.Unmarshal(data, &n) != nil || a.tray == nil {
		return
	}
	if s, err := a.settings(); err == nil && s.Notifications {
		a.tray.Notify(n.Title, n.Body)
	}
}

// WriteTextFile saves text to a path the user chose in a native save dialog.
// The dialog already confirmed overwriting; content is size-limited.
func (a *App) WriteTextFile(path, content string) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if len(content) > 8<<20 {
		return errors.New("content too large")
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
