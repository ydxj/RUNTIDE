// Package health implements reusable health checks and a per-service monitor
// with start periods and failure thresholds.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/procutil"
)

// Status values shown in the UI.
type Status string

const (
	Healthy      Status = "healthy"
	Unhealthy    Status = "unhealthy"
	Starting     Status = "starting"
	Unknown      Status = "unknown"
	NotMonitored Status = "not-monitored"
)

// Result is the outcome of a single probe.
type Result struct {
	OK      bool          `json:"ok"`
	Detail  string        `json:"detail"`
	Latency time.Duration `json:"latencyNs"`
}

// Probe runs one check. isAlive is used by process checks.
func Probe(ctx context.Context, h *config.HealthSpec, isAlive func() bool) Result {
	timeout := time.Duration(h.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	r := probe(ctx, h, isAlive)
	r.Latency = time.Since(start)
	return r
}

var httpClient = &http.Client{
	Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return http.ErrUseLastResponse
		}
		if !config.IsLoopbackHost(req.URL.Hostname()) {
			return fmt.Errorf("redirect to non-loopback host %q blocked", req.URL.Hostname())
		}
		return nil
	},
}

func probe(ctx context.Context, h *config.HealthSpec, isAlive func() bool) Result {
	switch h.Type {
	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
		if err != nil {
			return Result{Detail: err.Error()}
		}
		req.Header.Set("User-Agent", "RUNTIDE-healthcheck")
		resp, err := httpClient.Do(req)
		if err != nil {
			return Result{Detail: err.Error()}
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		ok := resp.StatusCode >= 200 && resp.StatusCode < 400
		if h.ExpectStatus != 0 {
			ok = resp.StatusCode == h.ExpectStatus
		}
		return Result{OK: ok, Detail: "HTTP " + strconv.Itoa(resp.StatusCode)}
	case "tcp":
		host := h.Host
		if host == "" {
			host = "127.0.0.1"
		}
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(h.Port)))
		if err != nil {
			return Result{Detail: err.Error()}
		}
		c.Close()
		return Result{OK: true, Detail: "TCP connect ok"}
	case "process":
		if isAlive != nil && isAlive() {
			return Result{OK: true, Detail: "process is running (does not prove it is serving)"}
		}
		return Result{Detail: "process is not running"}
	case "command":
		// Explicitly configured command; executed directly, never through a shell.
		out, err := procutil.Hide(exec.CommandContext(ctx, h.Command, h.Args...)).CombinedOutput()
		if err != nil {
			msg := err.Error()
			if len(out) > 0 {
				s := string(out)
				if len(s) > 200 {
					s = s[:200]
				}
				msg += ": " + s
			}
			return Result{Detail: msg}
		}
		return Result{OK: true, Detail: "command exited 0"}
	}
	return Result{Detail: "unknown check type " + h.Type}
}

// Change is emitted when a service's status changes.
type Change struct {
	Key    string
	From   Status
	To     Status
	Detail string
}

// Monitor tracks status for many services using one goroutine per monitored
// service. Goroutines exit when their context ends.
type Monitor struct {
	mu       sync.RWMutex
	status   map[string]Status
	detail   map[string]string
	onChange func(Change)
}

// NewMonitor creates a monitor; onChange may be nil.
func NewMonitor(onChange func(Change)) *Monitor {
	return &Monitor{status: map[string]Status{}, detail: map[string]string{}, onChange: onChange}
}

// Status returns the current status and last detail.
func (m *Monitor) Status(key string) (Status, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.status[key]
	if !ok {
		return Unknown, ""
	}
	return s, m.detail[key]
}

func (m *Monitor) set(key string, s Status, detail string) {
	m.mu.Lock()
	old, had := m.status[key]
	m.status[key] = s
	m.detail[key] = detail
	m.mu.Unlock()
	if m.onChange != nil && (!had || old != s) {
		m.onChange(Change{Key: key, From: old, To: s, Detail: detail})
	}
}

// Forget removes a service's state.
func (m *Monitor) Forget(key string) {
	m.mu.Lock()
	delete(m.status, key)
	delete(m.detail, key)
	m.mu.Unlock()
}

// Set forces a status (used for not-monitored / stopped transitions).
func (m *Monitor) Set(key string, s Status, detail string) { m.set(key, s, detail) }

// Run monitors until ctx is cancelled. Failures during the start period are
// tolerated; afterwards FailureThreshold consecutive failures mean unhealthy.
func (m *Monitor) Run(ctx context.Context, key string, h *config.HealthSpec, startedAt time.Time, isAlive func() bool) {
	interval := time.Duration(h.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	threshold := h.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	grace := time.Duration(h.StartPeriodSec) * time.Second
	m.set(key, Starting, "waiting for first successful check")
	fails := 0
	everHealthy := false
	// Probe quickly at first so readiness is detected promptly, then settle.
	delay := 250 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		r := Probe(ctx, h, isAlive)
		if ctx.Err() != nil {
			return
		}
		switch {
		case r.OK:
			fails = 0
			everHealthy = true
			m.set(key, Healthy, r.Detail)
		case time.Since(startedAt) < grace && !everHealthy:
			m.set(key, Starting, r.Detail)
		default:
			fails++
			if fails >= threshold {
				m.set(key, Unhealthy, r.Detail)
			} else if !everHealthy {
				m.set(key, Starting, r.Detail)
			}
		}
		if everHealthy || fails > 0 {
			delay = interval
		} else if delay < time.Second {
			delay *= 2 // quick ramp while waiting for a slow starter
		} else {
			delay = interval
		}
	}
}
