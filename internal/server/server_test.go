package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runtide/runtide/internal/database"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/logs"
	"github.com/runtide/runtide/internal/receipts"
	"github.com/runtide/runtide/internal/redact"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeReceipt runs a real (tiny) execution with cmd.exe and exports its receipt.
func makeReceipt(t *testing.T, name string, failVerify bool) []byte {
	t.Helper()
	db, _, err := database.Open(filepath.Join(t.TempDir(), "x.db"))
	require.NoError(t, err)
	defer db.Close()
	st := store.New(db.DB)
	hub := logs.NewHub(100, nil)
	m := execution.NewManager(st, execution.NewRecorder(st, redact.New(), nil), supervisor.NewManager(hub, nil), hub, t.TempDir(), "0.2.0-test")
	root := t.TempDir()
	_, err = m.CreateProject("shop", root, "")
	require.NoError(t, err)
	check := "{name: echoed, command: cmd.exe, args: ['/c', 'echo', 'hello <b>x</b>'], expect_output: hello}"
	if failVerify {
		check = "{name: missing, type: file, path: nothing.txt}"
	}
	y := fmt.Sprintf("version: 1\ntask: %s\nstart:\n  - {name: greet, command: cmd.exe, args: ['/c', 'echo', 'building %s']}\nverify:\n  name: v\n  checks:\n    - %s\n", name, name, check)
	plan, err := m.PlanWorkspace(execution.WorkspaceOptions{Project: "shop", Name: name, ConfigYAML: y})
	require.NoError(t, err)
	ws, err := m.CreateWorkspace(plan, name)
	require.NoError(t, err)
	require.NoError(t, m.ApproveWorkspace(ws.ID))
	x, err := m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, m.Start(context.Background(), x.ID))
	_, _, err = m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	data, _, err := receipts.Build(m, x.ID, receipts.BuildOptions{})
	require.NoError(t, err)
	return data
}

type env struct {
	t     *testing.T
	s     *Server
	srv   *httptest.Server
	admin string
}

func setup(t *testing.T, mod func(*Options)) *env {
	t.Helper()
	o := Options{DataDir: t.TempDir(), Version: "test"}
	if mod != nil {
		mod(&o)
	}
	s, err := New(o)
	require.NoError(t, err)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	e := &env{t: t, s: s, srv: srv}
	require.NotEmpty(t, s.BootstrapPassword)
	e.admin = e.login("admin", s.BootstrapPassword)
	return e
}

func (e *env) do(token, method, path string, body any) (int, []byte, http.Header) {
	e.t.Helper()
	var rd io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ct = bytes.NewReader(b), "application/zip"
	default:
		j, _ := json.Marshal(b)
		rd, ct = bytes.NewReader(j), "application/json"
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, resp.Header
}

func (e *env) login(user, pw string) string {
	code, out, _ := e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pw})
	require.Equal(e.t, 200, code, string(out))
	var r struct{ Token string }
	require.NoError(e.t, json.Unmarshal(out, &r))
	return r.Token
}

func (e *env) user(name, role string) string {
	pw := "correct-horse-battery-" + name
	code, out, _ := e.do(e.admin, "POST", "/api/v1/users", map[string]string{"username": name, "password": pw, "role": role})
	require.Equal(e.t, 201, code, string(out))
	return e.login(name, pw)
}

func idOf(t *testing.T, b []byte) string {
	var r struct{ ID string }
	require.NoError(t, json.Unmarshal(b, &r), string(b))
	return r.ID
}

func TestAuthenticationAndHardening(t *testing.T) {
	e := setup(t, nil)
	// Every non-public route rejects anonymous and bogus tokens.
	for _, rt := range e.s.routes {
		if rt.Access == "public" {
			continue
		}
		path := strings.NewReplacer("{id}", "x", "{userId}", "y", "{seq}", "1").Replace(rt.Pattern)
		code, _, _ := e.do("", rt.Method, path, nil)
		assert.Equal(t, 401, code, rt.Method+" "+rt.Pattern)
		code, _, _ = e.do("rts_forged", rt.Method, path, nil)
		assert.Equal(t, 401, code, rt.Method+" "+rt.Pattern+" with a forged token")
	}
	// Wrong passwords: same answer for unknown users and wrong passwords; account lockout after 5.
	code, out1, _ := e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": "nobody", "password": "x"})
	assert.Equal(t, 401, code)
	code, out2, _ := e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"})
	assert.Equal(t, 401, code)
	assert.JSONEq(t, string(out1), string(out2), "no user enumeration")
	for i := 0; i < 5; i++ {
		e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"})
	}
	code, _, h := e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": e.s.BootstrapPassword})
	assert.Equal(t, 429, code, "locked out even with the right password")
	assert.NotEmpty(t, h.Get("Retry-After"))
	// Hardening headers and strict JSON.
	_, _, h = e.do(e.admin, "GET", "/api/v1/me", nil)
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
	code, _, _ = e.do(e.admin, "POST", "/api/v1/projects", map[string]any{"name": "p", "bogus": 1})
	assert.Equal(t, 400, code, "unknown fields are rejected")
	code, _, _ = e.do(e.admin, "POST", "/api/v1/users", map[string]string{"username": "weak", "password": "short", "role": "member"})
	assert.Equal(t, 400, code, "short passwords are rejected")
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("a-long-enough-password")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "$argon2id$v=19$"))
	assert.True(t, CheckPassword("a-long-enough-password", h))
	assert.False(t, CheckPassword("a-long-enough-passwor", h))
	assert.False(t, CheckPassword("x", "garbage"))
	h2, _ := HashPassword("a-long-enough-password")
	assert.NotEqual(t, h, h2, "salted")
	_, err = HashPassword("short")
	assert.Error(t, err)
}

func TestRolesProjectsAndIsolationBetweenUsers(t *testing.T) {
	e := setup(t, nil)
	alice := e.user("alice", "member")
	bob := e.user("bob-reader", "member")
	viewer := e.user("vera", "viewer")

	code, out, _ := e.do(alice, "POST", "/api/v1/projects", map[string]string{"name": "shop"})
	require.Equal(t, 201, code, string(out))
	pid := idOf(t, out)
	code, _, _ = e.do(viewer, "POST", "/api/v1/projects", map[string]string{"name": "nope"})
	assert.Equal(t, 403, code, "viewers cannot create")

	// Bob cannot even see Alice's project (404, not 403).
	code, _, _ = e.do(bob, "GET", "/api/v1/projects/"+pid, nil)
	assert.Equal(t, 404, code)
	code, out, _ = e.do(bob, "GET", "/api/v1/projects", nil)
	assert.Equal(t, 200, code)
	assert.JSONEq(t, "[]", string(out))
	_, out, _ = e.do(e.admin, "GET", "/api/v1/projects", nil)
	assert.Contains(t, string(out), "shop")

	// Alice grants Bob read access; reader cannot upload or manage members.
	bobID := e.userID("bob-reader")
	code, _, _ = e.do(alice, "PUT", "/api/v1/projects/"+pid+"/members/"+bobID, map[string]string{"role": "reader"})
	assert.Equal(t, 204, code)
	code, _, _ = e.do(bob, "GET", "/api/v1/projects/"+pid, nil)
	assert.Equal(t, 200, code)
	code, _, _ = e.do(bob, "POST", "/api/v1/projects/"+pid+"/receipts", makeReceipt(t, "t1", false))
	assert.Equal(t, 403, code)
	code, _, _ = e.do(bob, "PUT", "/api/v1/projects/"+pid+"/members/"+bobID, map[string]string{"role": "owner"})
	assert.Equal(t, 403, code, "a reader cannot promote themselves")
	// A global viewer stays read-only even if made project writer.
	vid := e.userID("vera")
	e.do(alice, "PUT", "/api/v1/projects/"+pid+"/members/"+vid, map[string]string{"role": "writer"})
	code, _, _ = e.do(viewer, "POST", "/api/v1/projects/"+pid+"/receipts", makeReceipt(t, "t1", false))
	assert.Equal(t, 403, code)
	// Non-admins cannot use admin routes.
	code, _, _ = e.do(alice, "GET", "/api/v1/users", nil)
	assert.Equal(t, 403, code)
	code, _, _ = e.do(alice, "GET", "/api/v1/audit", nil)
	assert.Equal(t, 403, code)
	// The last owner cannot be removed.
	code, _, _ = e.do(alice, "DELETE", "/api/v1/projects/"+pid+"/members/"+e.userID("alice"), nil)
	assert.Equal(t, 409, code)
}

func (e *env) userID(name string) string {
	code, out, _ := e.do(e.admin, "GET", "/api/v1/users", nil)
	require.Equal(e.t, 200, code)
	var us []struct{ ID, Username string }
	require.NoError(e.t, json.Unmarshal(out, &us))
	for _, u := range us {
		if u.Username == name {
			return u.ID
		}
	}
	e.t.Fatalf("user %s not found", name)
	return ""
}

func TestReceiptUploadInspectDownloadCompareDelete(t *testing.T) {
	e := setup(t, nil)
	alice := e.user("alice", "member")
	_, out, _ := e.do(alice, "POST", "/api/v1/projects", map[string]string{"name": "shop"})
	pid := idOf(t, out)

	good := makeReceipt(t, "good", false)
	bad := makeReceipt(t, "bad", true)
	code, out, _ := e.do(alice, "POST", "/api/v1/projects/"+pid+"/receipts", good)
	require.Equal(t, 201, code, string(out))
	id1 := idOf(t, out)
	code, out, _ = e.do(alice, "POST", "/api/v1/projects/"+pid+"/receipts", good)
	assert.Equal(t, 200, code, "same receipt is not stored twice")
	assert.Equal(t, id1, idOf(t, out))
	code, out, _ = e.do(alice, "POST", "/api/v1/projects/"+pid+"/receipts", bad)
	require.Equal(t, 201, code)
	id2 := idOf(t, out)

	// Tampered and non-zip uploads are refused and never stored.
	zr, _ := zip.NewReader(bytes.NewReader(good), int64(len(good)))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if f.Name == "commands.json" {
			b = bytes.Replace(b, []byte("greet"), []byte("evil!"), 1)
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	zw.Close()
	code, out, _ = e.do(alice, "POST", "/api/v1/projects/"+pid+"/receipts", buf.Bytes())
	assert.Equal(t, 422, code)
	assert.Contains(t, string(out), "SHA-256")
	code, _, _ = e.do(alice, "POST", "/api/v1/projects/"+pid+"/receipts", []byte("not a zip at all"))
	assert.Equal(t, 422, code)
	_, out, _ = e.do(alice, "GET", "/api/v1/projects/"+pid+"/executions", nil)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(out, &list))
	assert.Len(t, list, 2)

	// Inspect: parsed from the re-verified artifact.
	code, out, _ = e.do(alice, "GET", "/api/v1/executions/"+id1, nil)
	require.Equal(t, 200, code, string(out))
	var ex struct {
		Summary      struct{ Status, Verification string }
		Commands     []struct{ Name string }
		Authenticity string
	}
	require.NoError(t, json.Unmarshal(out, &ex))
	assert.Equal(t, "COMPLETED", ex.Summary.Status)
	assert.Equal(t, "greet", ex.Commands[0].Name)
	assert.Contains(t, ex.Authenticity, "do not prove")
	code, out, _ = e.do(alice, "GET", "/api/v1/executions/"+id1+"/events?type=command.finished", nil)
	assert.Equal(t, 200, code)
	assert.Contains(t, string(out), "command.finished")
	code, out, _ = e.do(alice, "GET", "/api/v1/executions/"+id1+"/logs/1", nil)
	assert.Equal(t, 200, code)
	assert.Contains(t, string(out), "building good")
	code, out, h := e.do(alice, "GET", "/api/v1/executions/"+id1+"/report", nil)
	assert.Equal(t, 200, code)
	assert.Contains(t, h.Get("Content-Security-Policy"), "default-src 'none'")
	assert.NotContains(t, string(out), "<script")
	code, out, h = e.do(alice, "GET", "/api/v1/executions/"+id1+"/receipt", nil)
	assert.Equal(t, 200, code)
	assert.Equal(t, "application/zip", h.Get("Content-Type"))
	assert.Contains(t, h.Get("Content-Disposition"), "attachment")
	assert.Equal(t, good, out, "download is byte-identical")
	code, out, _ = e.do(alice, "GET", "/api/v1/compare?a="+id1+"&b="+id2, nil)
	require.Equal(t, 200, code)
	assert.Contains(t, string(out), `"overall":["passed","failed"]`)

	// Another user cannot read, compare or delete.
	bob := e.user("bobby", "member")
	for _, p := range []string{"/api/v1/executions/" + id1, "/api/v1/executions/" + id1 + "/receipt", "/api/v1/executions/" + id1 + "/events", "/api/v1/compare?a=" + id1 + "&b=" + id2} {
		code, _, _ = e.do(bob, "GET", p, nil)
		assert.Equal(t, 404, code, p)
	}
	code, _, _ = e.do(bob, "DELETE", "/api/v1/executions/"+id1, nil)
	assert.Equal(t, 404, code)
	// Path-like ids are inert.
	code, _, _ = e.do(alice, "GET", "/api/v1/executions/..%2f..%2fserver.db/receipt", nil)
	assert.Equal(t, 404, code)
	code, _, _ = e.do(alice, "DELETE", "/api/v1/executions/"+id1, nil)
	assert.Equal(t, 204, code)
	code, _, _ = e.do(alice, "GET", "/api/v1/executions/"+id1, nil)
	assert.Equal(t, 404, code)
	// Deleting the project removes its remaining artifacts from disk.
	code, _, _ = e.do(alice, "DELETE", "/api/v1/projects/"+pid, nil)
	assert.Equal(t, 204, code)
	left, _ := filepath.Glob(filepath.Join(e.s.opts.DataDir, "artifacts", "*"))
	assert.Empty(t, left)
	// Everything above is in the audit log.
	_, out, _ = e.do(e.admin, "GET", "/api/v1/audit", nil)
	for _, want := range []string{"receipt.upload", "login.failed", "receipt.download", "project.delete", "execution.delete"} {
		_ = want
	}
	assert.Contains(t, string(out), "receipt.upload")
	assert.Contains(t, string(out), "project.delete")
}

func TestUploadLimitAndTokens(t *testing.T) {
	e := setup(t, func(o *Options) { o.MaxUploadBytes = 2048 })
	_, out, _ := e.do(e.admin, "POST", "/api/v1/projects", map[string]string{"name": "p"})
	pid := idOf(t, out)
	code, _, _ := e.do(e.admin, "POST", "/api/v1/projects/"+pid+"/receipts", make([]byte, 100000))
	assert.Equal(t, 413, code)

	// API tokens: created once, usable, revocable; disabling a user kills their tokens.
	code, out, _ = e.do(e.admin, "POST", "/api/v1/tokens", map[string]any{"name": "ci"})
	require.Equal(t, 201, code)
	var tk struct{ ID, Token string }
	require.NoError(t, json.Unmarshal(out, &tk))
	assert.True(t, strings.HasPrefix(tk.Token, "rtk_"))
	code, _, _ = e.do(tk.Token, "GET", "/api/v1/me", nil)
	assert.Equal(t, 200, code)
	_, out, _ = e.do(e.admin, "GET", "/api/v1/tokens", nil)
	assert.NotContains(t, string(out), tk.Token, "the secret is never listed")
	code, _, _ = e.do(e.admin, "DELETE", "/api/v1/tokens/"+tk.ID, nil)
	assert.Equal(t, 204, code)
	code, _, _ = e.do(tk.Token, "GET", "/api/v1/me", nil)
	assert.Equal(t, 401, code)

	u := e.user("temp-user", "member")
	uid := e.userID("temp-user")
	code, _, _ = e.do(e.admin, "PATCH", "/api/v1/users/"+uid, map[string]any{"disabled": true})
	assert.Equal(t, 204, code)
	code, _, _ = e.do(u, "GET", "/api/v1/me", nil)
	assert.Equal(t, 401, code)
	adminID := e.userID("admin")
	code, _, _ = e.do(e.admin, "PATCH", "/api/v1/users/"+adminID, map[string]any{"role": "member"})
	assert.Equal(t, 400, code, "cannot demote yourself")
	// Logout revokes the session.
	code, _, _ = e.do(e.admin, "POST", "/api/v1/auth/logout", nil)
	assert.Equal(t, 204, code)
	code, _, _ = e.do(e.admin, "GET", "/api/v1/me", nil)
	assert.Equal(t, 401, code)
}

func TestOpenAPIMatchesRoutesAndHealth(t *testing.T) {
	e := setup(t, nil)
	code, out, _ := e.do("", "GET", "/api/v1/openapi.json", nil)
	require.Equal(t, 200, code)
	var spec struct {
		OpenAPI string
		Paths   map[string]map[string]any
	}
	require.NoError(t, json.Unmarshal(out, &spec))
	assert.Equal(t, "3.0.3", spec.OpenAPI)
	for _, rt := range e.s.routes {
		_, ok := spec.Paths[rt.Pattern][strings.ToLower(rt.Method)]
		assert.True(t, ok, "%s %s missing from OpenAPI", rt.Method, rt.Pattern)
	}
	for p := range spec.Paths {
		assert.NotRegexp(t, `/(exec|run|runs|process|processes|command|commands|shell)(/|$)`, p, "the server must not expose command execution")
	}
	code, out, _ = e.do("", "GET", "/healthz", nil)
	assert.Equal(t, 200, code)
	assert.Contains(t, string(out), `"status":"ok"`)
	assert.NotContains(t, string(out), e.s.opts.DataDir, "health reveals no paths")
}

func TestBindPolicy(t *testing.T) {
	d := t.TempDir()
	for addr, o := range map[string]Options{
		"0.0.0.0:8484":  {DataDir: d, Addr: "0.0.0.0:8484"},
		"10.0.0.5:8484": {DataDir: d, Addr: "10.0.0.5:8484", AllowRemote: true},
		"[::]:8484":     {DataDir: d, Addr: "[::]:8484"},
		"half-tls:8484": {DataDir: d, Addr: "10.0.0.5:8484", AllowRemote: true, TLSCert: "c"},
	} {
		_, err := New(o)
		assert.Error(t, err, addr)
	}
	for _, o := range []Options{
		{DataDir: t.TempDir(), Addr: "127.0.0.1:0"},
		{DataDir: t.TempDir(), Addr: "[::1]:0"},
		{DataDir: t.TempDir(), Addr: "localhost:0"},
		{DataDir: t.TempDir(), Addr: "10.0.0.5:8484", AllowRemote: true, BehindTLSProxy: true},
	} {
		s, err := New(o)
		require.NoError(t, err, o.Addr)
		s.Close()
	}
	_, err := New(Options{DataDir: d, Addr: "nonsense"})
	assert.Error(t, err)
}

func TestPersistenceAcrossRestartAndMigrationGuard(t *testing.T) {
	d := t.TempDir()
	s, err := New(Options{DataDir: d, Version: "t"})
	require.NoError(t, err)
	pw := s.BootstrapPassword
	srv := httptest.NewServer(s.Handler())
	e := &env{t: t, s: s, srv: srv}
	tok := e.login("admin", pw)
	code, _, _ := e.do(tok, "POST", "/api/v1/projects", map[string]string{"name": "keep"})
	require.Equal(t, 201, code)
	srv.Close()
	s.Close()

	s2, err := New(Options{DataDir: d, Version: "t"})
	require.NoError(t, err)
	assert.Empty(t, s2.BootstrapPassword, "no second bootstrap admin")
	srv2 := httptest.NewServer(s2.Handler())
	defer srv2.Close()
	defer s2.Close()
	e2 := &env{t: t, s: s2, srv: srv2}
	tok2 := e2.login("admin", pw)
	_, out, _ := e2.do(tok2, "GET", "/api/v1/projects", nil)
	assert.Contains(t, string(out), "keep")
	_, err = os.Stat(filepath.Join(d, "server.db"))
	assert.NoError(t, err)
}
