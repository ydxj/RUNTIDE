package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/receipts"
)

// Options configure the server.
type Options struct {
	DataDir        string
	Addr           string // default 127.0.0.1:8484
	TLSCert        string
	TLSKey         string
	AllowRemote    bool // permit binding a non-loopback address
	BehindTLSProxy bool // acknowledge that a TLS-terminating proxy protects a non-loopback plain-HTTP listener
	Version        string
	AdminPassword  string // initial admin password; generated when empty
	MaxUploadBytes int64  // default 256 MiB
}

// Server is the HTTP application.
type Server struct {
	opts      Options
	db        *sql.DB
	mux       *http.ServeMux
	routes    []route
	loginIP   *limiter
	loginUser *limiter
	api       *limiter
	// BootstrapPassword is set when the first admin account was created with a generated password.
	BootstrapPassword string
}

// New opens the database, creates the first admin when needed and builds routes.
func New(o Options) (*Server, error) {
	if o.Addr == "" {
		o.Addr = "127.0.0.1:8484"
	}
	if o.MaxUploadBytes <= 0 {
		o.MaxUploadBytes = 256 << 20
	}
	if o.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if err := checkBind(o); err != nil {
		return nil, err
	}
	for _, d := range []string{"artifacts", "tmp"} {
		if err := os.MkdirAll(filepath.Join(o.DataDir, d), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := openDB(o.DataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: o, db: db, mux: http.NewServeMux(), loginIP: newLimiter(time.Minute, 20), loginUser: newLimiter(5*time.Minute, 5), api: newLimiter(time.Minute, 600)}
	if err := s.ensureAdmin(); err != nil {
		db.Close()
		return nil, err
	}
	s.routes = s.buildRoutes()
	for _, rt := range s.routes {
		rt := rt
		s.mux.HandleFunc(rt.Method+" "+rt.Pattern, s.wrap(rt))
	}
	return s, nil
}

// checkBind refuses to expose the server beyond loopback unless explicitly
// configured, and never over plain HTTP without acknowledging a TLS proxy.
func checkBind(o Options) error {
	host, _, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", o.Addr, err)
	}
	loop := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loop = ip.IsLoopback()
	}
	if loop {
		return nil
	}
	if !o.AllowRemote {
		return fmt.Errorf("refusing to listen on %s: the server only binds loopback unless --allow-remote is given", o.Addr)
	}
	if (o.TLSCert == "") != (o.TLSKey == "") {
		return errors.New("--tls-cert and --tls-key must be given together")
	}
	if o.TLSCert == "" && !o.BehindTLSProxy {
		return errors.New("refusing to serve credentials over plain HTTP on a non-loopback address: provide --tls-cert/--tls-key, or pass --behind-tls-proxy if a TLS-terminating proxy fronts this port")
	}
	return nil
}

// Close releases the database.
func (s *Server) Close() error { return s.db.Close() }

// Handler returns the root handler (used by tests and ListenAndServe).
func (s *Server) Handler() http.Handler { return s.mux }

// ListenAndServe serves until ctx ends, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{Addr: s.opts.Addr, Handler: s.mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Minute, WriteTimeout: 30 * time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
	errc := make(chan error, 1)
	go func() {
		if s.opts.TLSCert != "" {
			srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			errc <- srv.ListenAndServeTLS(s.opts.TLSCert, s.opts.TLSKey)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

func (s *Server) ensureAdmin() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	pw := s.opts.AdminPassword
	if pw == "" {
		b := make([]byte, 15)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		const alpha = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
		out := make([]byte, 20)
		for i := range out {
			out[i] = alpha[int(b[i%len(b)])%len(alpha)]
		}
		pw = string(out)
		s.BootstrapPassword = pw
	}
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)`, domain.NewID("us"), "admin", h, "admin", time.Now().Unix())
	if err == nil {
		s.auditRaw("system", "", "user.bootstrap", "admin", "initial administrator created")
	}
	return err
}

func (s *Server) auditRaw(actor, ip, action, subject, detail string) {
	_, _ = s.db.Exec(`INSERT INTO audit(ts,actor,ip,action,subject,detail) VALUES(?,?,?,?,?,?)`, time.Now().Unix(), actor, ip, action, subject, detail)
}

// ---- routing ----

type principal struct {
	UserID   string
	Username string
	Role     string
	TokenID  string
	Kind     string
}

type route struct {
	Method, Pattern, Summary string
	Access                   string // public | user | admin
	Handler                  func(w http.ResponseWriter, r *http.Request, p *principal)
}

type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return e.Message }

func fail(status int, code, msg string, a ...any) *apiError {
	return &apiError{Status: status, Code: code, Message: fmt.Sprintf(msg, a...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		ae = fail(500, "internal", "internal error")
	}
	writeJSON(w, ae.Status, map[string]any{"error": ae})
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) wrap(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=63072000")
		}
		var p *principal
		if rt.Access != "public" {
			var err error
			p, err = s.authenticate(r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="runtide"`)
				writeErr(w, err)
				return
			}
			if ok, retry := s.api.allow(p.TokenID); !ok {
				w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
				writeErr(w, fail(429, "rate_limited", "too many requests"))
				return
			}
			if rt.Access == "admin" && p.Role != "admin" {
				writeErr(w, fail(403, "forbidden", "administrator role required"))
				return
			}
		} else if ok, retry := s.api.allow("ip:" + clientIP(r)); !ok {
			w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
			writeErr(w, fail(429, "rate_limited", "too many requests"))
			return
		}
		rt.Handler(w, r, p)
	}
}

func (s *Server) authenticate(r *http.Request) (*principal, error) {
	auth := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || tok == "" {
		return nil, fail(401, "unauthorized", "missing bearer token")
	}
	var p principal
	var exp sql.NullInt64
	var disabled int
	err := s.db.QueryRow(`SELECT t.id,t.kind,t.expires_at,u.id,u.username,u.role,u.disabled FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.token_hash=?`, HashToken(tok)).
		Scan(&p.TokenID, &p.Kind, &exp, &p.UserID, &p.Username, &p.Role, &disabled)
	if err != nil || disabled == 1 || (exp.Valid && exp.Int64 < time.Now().Unix()) {
		return nil, fail(401, "unauthorized", "invalid or expired token")
	}
	_, _ = s.db.Exec(`UPDATE tokens SET last_used=? WHERE id=?`, time.Now().Unix(), p.TokenID)
	return &p, nil
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fail(400, "bad_request", "invalid JSON body: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail(400, "bad_request", "unexpected data after JSON body")
	}
	return nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
var userRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{2,63}$`)

// projectRole returns the caller's effective role in a project ("" = no access).
func (s *Server) projectRole(p *principal, projectID string) string {
	if p.Role == "admin" {
		return "owner"
	}
	var role string
	if err := s.db.QueryRow(`SELECT role FROM project_members WHERE project_id=? AND user_id=?`, projectID, p.UserID).Scan(&role); err != nil {
		return ""
	}
	if p.Role == "viewer" {
		return "reader" // global viewers never write
	}
	return role
}

func (s *Server) needProject(p *principal, id, min string) (string, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM projects WHERE id=?`, id).Scan(&exists); err != nil {
		return "", fail(404, "not_found", "project not found")
	}
	role := s.projectRole(p, id)
	rank := map[string]int{"": 0, "reader": 1, "writer": 2, "owner": 3}
	if rank[role] == 0 {
		return "", fail(404, "not_found", "project not found") // do not reveal existence
	}
	if rank[role] < rank[min] {
		return "", fail(403, "forbidden", "requires %s access to this project", min)
	}
	return role, nil
}

func (s *Server) audit(r *http.Request, p *principal, action, subject, detail string) {
	actor := "anonymous"
	if p != nil {
		actor = p.Username
	}
	s.auditRaw(actor, clientIP(r), action, subject, detail)
}

// receiptSummary is a stored receipt's index entry.
type receiptSummary struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	ExecutionID     string    `json:"executionId"`
	Task            string    `json:"task"`
	Status          string    `json:"status"`
	Verification    string    `json:"verification"`
	Reproducibility string    `json:"reproducibility"`
	Isolation       string    `json:"isolation"`
	Signer          string    `json:"signer,omitempty"`
	RuntideVersion  string    `json:"runtideVersion"`
	SHA256          string    `json:"sha256"`
	Size            int64     `json:"size"`
	ReceiptCreated  time.Time `json:"receiptCreatedAt"`
	UploadedAt      time.Time `json:"uploadedAt"`
}

func (s *Server) artifactPath(sha string) string {
	return filepath.Join(s.opts.DataDir, "artifacts", sha+receipts.Extension)
}

func scanSummary(row interface{ Scan(...any) error }) (*receiptSummary, error) {
	var x receiptSummary
	var rc, up int64
	if err := row.Scan(&x.ID, &x.ProjectID, &x.ExecutionID, &x.Task, &x.Status, &x.Verification, &x.Reproducibility, &x.Isolation, &x.Signer, &x.RuntideVersion, &x.SHA256, &x.Size, &rc, &up); err != nil {
		return nil, err
	}
	x.ReceiptCreated, x.UploadedAt = time.Unix(rc, 0), time.Unix(up, 0)
	return &x, nil
}

const summaryCols = `id,project_id,execution_id,task,status,verification,reproducibility,isolation,signer,runtide_version,receipt_sha,size,receipt_created_at,uploaded_at`
