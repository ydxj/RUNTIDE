// Package api exposes the engine on a loopback-only, token-authenticated HTTP
// endpoint so the CLI can talk to a running engine (desktop app or daemon).
// Nothing here is reachable from the network: the listener binds 127.0.0.1,
// every request needs a per-session bearer token, and requests carrying an
// Origin header (i.e. from web pages) or a foreign Host header are rejected.
package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/paths"
	winapi "github.com/runtide/runtide/internal/platform/windows"
)

// Info is what a running engine publishes in api.json (mode 0600, in the
// user's private data directory).
type Info struct {
	Addr    string    `json:"addr"`
	Token   string    `json:"token"`
	PID     int       `json:"pid"`
	Kind    string    `json:"kind"` // desktop | daemon
	Version string    `json:"version"`
	Started time.Time `json:"started"`
}

// ErrAlreadyRunning is returned when another engine owns this data directory.
var ErrAlreadyRunning = errors.New("another RUNTIDE engine is already running for this user")

// Server serves one engine.
type Server struct {
	eng   *app.Engine
	home  string
	srv   *http.Server
	token string
	info  Info
}

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Serve starts the API. kind is "desktop" or "daemon".
func Serve(eng *app.Engine, kind string) (*Server, error) {
	home := eng.Home
	if c, err := Connect(home); err == nil {
		_ = c
		return nil, ErrAlreadyRunning
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{eng: eng, home: home, token: hex.EncodeToString(tok)}
	s.info = Info{Addr: ln.Addr().String(), Token: s.token, PID: os.Getpid(), Kind: kind, Version: app.Version, Started: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", s.guard(s.rpc))
	mux.HandleFunc("/events", s.guard(s.events))
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	b, _ := json.Marshal(s.info)
	if err := os.WriteFile(paths.APIFile(home), b, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	go s.srv.Serve(ln)
	return s, nil
}

// Close stops the server and removes api.json.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = os.Remove(paths.APIFile(s.home))
}

func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not allowed", http.StatusForbidden)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) rpc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	res, err := s.eng.Dispatch(r.Context(), req.Method, req.Params)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(response{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(response{Result: res})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.eng.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// Client talks to a running engine.
type Client struct {
	Info Info
	http *http.Client
}

// Connect finds a live engine for home. It verifies the published pid is
// alive and that the endpoint answers with the published token, so stale
// api.json files are ignored.
func Connect(home string) (*Client, error) {
	b, err := os.ReadFile(paths.APIFile(home))
	if err != nil {
		return nil, errors.New("no running RUNTIDE engine")
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, errors.New("api.json is unreadable")
	}
	if !winapi.IsRunning(uint32(in.PID)) {
		_ = os.Remove(paths.APIFile(home))
		return nil, errors.New("engine not running (stale lock removed)")
	}
	c := &Client{Info: in, http: &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{Proxy: nil}}}
	var v map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.CallCtx(ctx, "version", nil, &v); err != nil {
		return nil, fmt.Errorf("engine did not respond: %w", err)
	}
	return c, nil
}

// Call performs an RPC and decodes the result into out (may be nil).
func (c *Client) Call(method string, params any, out any) error {
	return c.CallCtx(context.Background(), method, params, out)
}

// CallCtx is Call with a context.
func (c *Client) CallCtx(ctx context.Context, method string, params any, out any) error {
	pb, err := json.Marshal(params)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(request{Method: method, Params: pb})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.Info.Addr+"/rpc", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.Info.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("engine rejected request: %s", resp.Status)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	if out != nil && len(r.Result) > 0 {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// Events streams engine events until ctx ends.
func (c *Client) Events(ctx context.Context, fn func(topic string, data json.RawMessage)) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.Info.Addr+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+c.Info.Token)
	cl := &http.Client{Transport: &http.Transport{Proxy: nil}} // no timeout: long-lived
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events rejected: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Topic string          `json:"topic"`
			Data  json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(line[6:]), &ev) == nil {
			fn(ev.Topic, ev.Data)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}
