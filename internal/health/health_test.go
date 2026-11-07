package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPProbe(t *testing.T) {
	var code atomic.Int32
	code.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(code.Load())) }))
	defer srv.Close()
	h := &config.HealthSpec{Type: "http", URL: srv.URL, TimeoutSec: 2}
	assert.True(t, Probe(context.Background(), h, nil).OK)
	code.Store(500)
	r := Probe(context.Background(), h, nil)
	assert.False(t, r.OK)
	assert.Equal(t, "HTTP 500", r.Detail)
	h.ExpectStatus = 500
	assert.True(t, Probe(context.Background(), h, nil).OK)
}

func TestTCPProbe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	h := &config.HealthSpec{Type: "tcp", Port: port, TimeoutSec: 1}
	assert.True(t, Probe(context.Background(), h, nil).OK)
	l.Close()
	assert.False(t, Probe(context.Background(), h, nil).OK)
}

func TestProcessAndCommandProbe(t *testing.T) {
	assert.True(t, Probe(context.Background(), &config.HealthSpec{Type: "process"}, func() bool { return true }).OK)
	assert.False(t, Probe(context.Background(), &config.HealthSpec{Type: "process"}, func() bool { return false }).OK)
	assert.True(t, Probe(context.Background(), &config.HealthSpec{Type: "command", Command: "cmd.exe", Args: []string{"/c", "exit", "0"}}, nil).OK)
	assert.False(t, Probe(context.Background(), &config.HealthSpec{Type: "command", Command: "cmd.exe", Args: []string{"/c", "exit", "3"}}, nil).OK)
	assert.False(t, Probe(context.Background(), &config.HealthSpec{Type: "command", Command: "definitely-not-a-command-xyz"}, nil).OK)
}

func TestMonitorTransitions(t *testing.T) {
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if up.Load() {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()
	var changes []Status
	var mu = make(chan Status, 20)
	m := NewMonitor(func(c Change) { mu <- c.To })
	h := &config.HealthSpec{Type: "http", URL: srv.URL, IntervalSec: 1, TimeoutSec: 1, FailureThreshold: 2, StartPeriodSec: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx, "svc", h, time.Now(), nil)
	expect := func(want Status, d time.Duration) {
		t.Helper()
		deadline := time.After(d)
		for {
			select {
			case s := <-mu:
				changes = append(changes, s)
				if s == want {
					return
				}
			case <-deadline:
				t.Fatalf("never reached %s; saw %v", want, changes)
			}
		}
	}
	expect(Starting, 2*time.Second)
	up.Store(true)
	expect(Healthy, 5*time.Second)
	up.Store(false)
	expect(Unhealthy, 8*time.Second)
	s, d := m.Status("svc")
	assert.Equal(t, Unhealthy, s)
	assert.Contains(t, d, strconv.Itoa(503))
}
