package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestLogIsStructuredAndNeverContainsSecrets(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	w := writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) })
	e := setup(t, func(o *Options) { o.Logger = slog.New(slog.NewJSONHandler(w, nil)) })
	alice := e.user("alice", "member")

	code, _, h := e.do(alice, "GET", "/api/v1/projects?token=query-secret-123", nil)
	require.Equal(t, 200, code)
	assert.Len(t, h.Get("X-Request-Id"), 16, "every response carries a request id")
	code, _, _ = e.do("wrong-token-value", "GET", "/api/v1/projects", nil)
	assert.Equal(t, 401, code)
	e.do("", "POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": "not-the-password-xyz"})

	mu.Lock()
	logText := buf.String()
	mu.Unlock()
	assert.NotContains(t, logText, "query-secret-123", "query strings are never logged")
	assert.NotContains(t, logText, "wrong-token-value", "tokens are never logged")
	assert.NotContains(t, logText, "not-the-password-xyz", "bodies are never logged")
	assert.NotContains(t, logText, alice, "session tokens are never logged")
	var sawOK, sawUnauthorized bool
	for _, line := range strings.Split(strings.TrimSpace(logText), "\n") {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		if rec["msg"] != "request" {
			continue
		}
		for _, k := range []string{"id", "method", "path", "status", "duration", "client"} {
			assert.Contains(t, rec, k)
		}
		if rec["path"] == "/api/v1/projects" && rec["status"] == float64(200) {
			sawOK = true
			assert.Equal(t, "alice", rec["user"])
			assert.Equal(t, "INFO", rec["level"])
		}
		if rec["status"] == float64(401) {
			sawUnauthorized = true
			assert.Equal(t, "WARN", rec["level"], "authentication failures are warnings")
		}
	}
	assert.True(t, sawOK && sawUnauthorized)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Several users upload, list, inspect and compare at the same time. Nothing may
// fail with a server error, duplicates must be stored once, and counts must add up.
func TestConcurrentUsersDoNotCorruptState(t *testing.T) {
	e := setup(t, nil)
	good := makeReceipt(t, "good", false)
	bad := makeReceipt(t, "bad", true)

	const users = 6
	tokens := make([]string, users)
	pids := make([]string, users)
	for i := range tokens {
		tokens[i] = e.user(fmt.Sprintf("user%d", i), "member")
		_, out, _ := e.do(tokens[i], "POST", "/api/v1/projects", map[string]string{"name": fmt.Sprintf("proj%d", i)})
		pids[i] = idOf(t, out)
	}
	// One shared project everybody may write to.
	_, out, _ := e.do(tokens[0], "POST", "/api/v1/projects", map[string]string{"name": "shared"})
	shared := idOf(t, out)
	for i := 1; i < users; i++ {
		_, uo, _ := e.do(e.admin, "GET", "/api/v1/users", nil)
		var us []struct{ ID, Username string }
		require.NoError(t, json.Unmarshal(uo, &us))
		for _, u := range us {
			if u.Username == fmt.Sprintf("user%d", i) {
				code, b, _ := e.do(tokens[0], "PUT", "/api/v1/projects/"+shared+"/members/"+u.ID, map[string]string{"role": "writer"})
				require.Equal(t, 204, code, string(b))
			}
		}
	}

	var serverErrors, created atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < users; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 3; round++ {
				for _, data := range [][]byte{good, bad} {
					for _, pid := range []string{pids[i], shared} {
						code, body, _ := e.do(tokens[i], "POST", "/api/v1/projects/"+pid+"/receipts", data)
						switch {
						case code >= 500:
							serverErrors.Add(1)
							t.Logf("upload to %s failed: %d %s", pid, code, body)
						case code == 201:
							created.Add(1)
						}
					}
				}
				for _, path := range []string{"/api/v1/projects", "/api/v1/projects/" + shared + "/executions", "/healthz"} {
					if code, body, _ := e.do(tokens[i], "GET", path, nil); code >= 500 {
						serverErrors.Add(1)
						t.Logf("GET %s failed: %d %s", path, code, body)
					}
				}
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 0, serverErrors.Load(), "no request may fail with a server error under concurrency")

	// Identical content is stored once per project: 2 distinct receipts in each of the 7 projects.
	_, out, _ = e.do(tokens[0], "GET", "/api/v1/projects/"+shared+"/executions", nil)
	var shExec []map[string]any
	require.NoError(t, json.Unmarshal(out, &shExec))
	assert.Len(t, shExec, 2, "the shared project holds each distinct receipt exactly once")
	for i := 0; i < users; i++ {
		_, out, _ = e.do(tokens[i], "GET", "/api/v1/projects/"+pids[i]+"/executions", nil)
		var l []map[string]any
		require.NoError(t, json.Unmarshal(out, &l))
		assert.Len(t, l, 2, "project %d", i)
	}
	assert.EqualValues(t, int64(users*2+2), created.Load(), "exactly one successful creation per distinct receipt per project")
}
