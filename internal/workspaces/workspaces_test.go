package workspaces

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain makes the test binary double as a fake dev server.
func TestMain(m *testing.M) {
	switch os.Getenv("RUNTIDE_HELPER") {
	case "":
		os.Exit(m.Run())
	case "sleep":
		fmt.Println("up")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		os.Exit(0)
	case "crash":
		os.Exit(7)
	case "serve":
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("HELPER_PORT"))
		if err != nil {
			os.Exit(4)
		}
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() { <-c; os.Exit(0) }()
		for {
			if cn, err := l.Accept(); err == nil {
				cn.Close()
			}
		}
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type env struct {
	m    *Manager
	sup  *supervisor.Manager
	hub  *logs.Hub
	root string
}

func setup(t *testing.T) *env {
	t.Helper()
	db, _, err := database.Open(filepath.Join(t.TempDir(), "w.db"))
	require.NoError(t, err)
	hub := logs.NewHub(1000, nil)
	sup := supervisor.NewManager(hub, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sup.Shutdown(ctx)
		db.Close()
	})
	return &env{m: NewManager(db, sup), sup: sup, hub: hub, root: t.TempDir()}
}

func svcYAML(name, mode string, deps []string, extra string) string {
	exe := strings.ReplaceAll(os.Args[0], `\`, `/`)
	d := ""
	if len(deps) > 0 {
		d = "    depends_on: [" + strings.Join(deps, ", ") + "]\n"
	}
	return fmt.Sprintf("  - name: %s\n    command: '%s'\n    env: {RUNTIDE_HELPER: %s}\n    stop_timeout_sec: 3\n%s%s", name, exe, mode, d, extra)
}

func TestUpOrderingDownReverseAndApproval(t *testing.T) {
	e := setup(t)
	port := freePort(t)
	y := "name: shop\nservices:\n" +
		svcYAML("frontend", "sleep", []string{"api"}, "") +
		svcYAML("api", "sleep", []string{"db"}, "") +
		fmt.Sprintf("  - name: db\n    command: '%s'\n    env: {RUNTIDE_HELPER: serve, HELPER_PORT: '%d'}\n    stop_timeout_sec: 3\n    healthcheck: {type: tcp, port: %d, interval_sec: 1}\n", strings.ReplaceAll(os.Args[0], `\`, `/`), port, port)

	rec, err := e.m.Save(y, e.root, false)
	require.NoError(t, err)
	assert.False(t, rec.Approved)

	err = e.m.Up(context.Background(), "shop", nil, nil)
	require.ErrorIs(t, err, ErrNotApproved, "execution requires explicit approval")

	require.NoError(t, e.m.Approve("shop"))
	var mu sync.Mutex
	var order []string
	require.NoError(t, e.m.Up(context.Background(), "shop", nil, func(p Progress) {
		if p.Phase == "starting" {
			mu.Lock()
			order = append(order, p.Service)
			mu.Unlock()
		}
	}))
	assert.Equal(t, []string{"db", "api", "frontend"}, order)
	st, err := e.m.Status("shop")
	require.NoError(t, err)
	assert.Equal(t, 3, st.Running)
	assert.Equal(t, "healthy", st.Health)

	// Editing revokes approval.
	_, err = e.m.Save(y, e.root, true)
	assert.ErrorContains(t, err, "running services")

	var stops []string
	require.NoError(t, e.m.Down(context.Background(), "shop", func(p Progress) {
		if p.Phase == "stopped" {
			mu.Lock()
			stops = append(stops, p.Service)
			mu.Unlock()
		}
	}))
	assert.Equal(t, []string{"frontend", "api", "db"}, stops)
	st, _ = e.m.Status("shop")
	assert.Equal(t, "stopped", st.Health)

	rec, err = e.m.Save(strings.Replace(y, "stop_timeout_sec: 3", "stop_timeout_sec: 4", 1), e.root, true)
	require.NoError(t, err)
	assert.False(t, rec.Approved, "changed definition must need re-approval")
	h, _ := e.m.History("shop", 10)
	assert.NotEmpty(t, h)
}

func TestFailureRollsBack(t *testing.T) {
	e := setup(t)
	y := "name: bad\nservices:\n" +
		svcYAML("ok", "sleep", nil, "") +
		svcYAML("boom", "crash", []string{"ok"}, "")
	_, err := e.m.Save(y, e.root, false)
	require.NoError(t, err)
	require.NoError(t, e.m.Approve("bad"))
	err = e.m.Up(context.Background(), "bad", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	inf, _ := e.sup.Get("bad/ok")
	assert.Equal(t, supervisor.StateStopped, inf.State, "already-started dependencies are rolled back")
}

func TestValidationRejectsDangerousInput(t *testing.T) {
	e := setup(t)
	_, err := e.m.Save("name: x\nservices:\n  - {name: a, command: x, depends_on: [b]}\n  - {name: b, command: x, depends_on: [a]}\n", e.root, false)
	assert.ErrorContains(t, err, "cycle")
	_, err = e.m.Save("name: x\nservices:\n  - {name: a, command: x, working_directory: ../../Windows}\n", e.root, false)
	assert.ErrorContains(t, err, "escapes")
	_, err = e.m.Save("name: x\nservices:\n  - {name: a, command: x}\n", `relative\path`, false)
	assert.ErrorContains(t, err, "absolute")
	_, err = e.m.Save("name: x\nservices:\n  - {name: a, command: x}\n", filepath.Join(e.root, "missing"), false)
	assert.ErrorContains(t, err, "not an existing directory")
	list, _ := e.m.List()
	assert.Empty(t, list)
}

func TestPreviewListsCommandsAndDelete(t *testing.T) {
	e := setup(t)
	p, _, err := e.m.Inspect("name: p\nservices:\n  - {name: web, command: npm, args: [run, dev]}\n", e.root)
	require.NoError(t, err)
	assert.Contains(t, p.Commands[0], "npm run dev")
	_, err = e.m.Save("name: p\nservices:\n  - {name: web, command: npm, args: [run, dev]}\n", e.root, false)
	require.NoError(t, err)
	_, err = e.m.Save("name: p\nservices:\n  - {name: web, command: npm}\n", e.root, false)
	assert.ErrorContains(t, err, "already exists")
	require.NoError(t, e.m.Delete("p"))
	_, err = e.m.Get("p")
	assert.ErrorIs(t, err, ErrNotFound)
}
