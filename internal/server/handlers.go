package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/receiptcore"
)

type handler = func(w http.ResponseWriter, r *http.Request, p *principal)

func (s *Server) buildRoutes() []route {
	return []route{
		{"GET", "/healthz", "Liveness and readiness (no secrets)", "public", s.health},
		{"GET", "/api/v1/openapi.json", "OpenAPI description of this API", "public", s.openapi},
		{"POST", "/api/v1/auth/login", "Exchange username and password for a session token", "public", s.login},
		{"POST", "/api/v1/auth/logout", "Revoke the calling session token", "user", s.logout},
		{"GET", "/api/v1/me", "The authenticated user", "user", s.me},
		{"GET", "/api/v1/users", "List users", "admin", s.listUsers},
		{"POST", "/api/v1/users", "Create a user", "admin", s.createUser},
		{"PATCH", "/api/v1/users/{id}", "Change a user's role or disabled flag", "admin", s.patchUser},
		{"DELETE", "/api/v1/users/{id}", "Delete a user", "admin", s.deleteUser},
		{"POST", "/api/v1/users/{id}/password", "Set a password (admin, or self with the current password)", "user", s.setPassword},
		{"GET", "/api/v1/tokens", "List the caller's API tokens", "user", s.listTokens},
		{"POST", "/api/v1/tokens", "Create an API token (shown once)", "user", s.createToken},
		{"DELETE", "/api/v1/tokens/{id}", "Revoke a token", "user", s.deleteToken},
		{"GET", "/api/v1/projects", "List projects the caller can access", "user", s.listProjects},
		{"POST", "/api/v1/projects", "Create a project", "user", s.createProject},
		{"GET", "/api/v1/projects/{id}", "Get a project", "user", s.getProject},
		{"DELETE", "/api/v1/projects/{id}", "Delete a project and its receipts", "user", s.deleteProject},
		{"GET", "/api/v1/projects/{id}/members", "List project members", "user", s.listMembers},
		{"PUT", "/api/v1/projects/{id}/members/{userId}", "Add or change a project member", "user", s.putMember},
		{"DELETE", "/api/v1/projects/{id}/members/{userId}", "Remove a project member", "user", s.deleteMember},
		{"GET", "/api/v1/projects/{id}/executions", "List stored executions of a project", "user", s.listExecutions},
		{"POST", "/api/v1/projects/{id}/receipts", "Upload a Run Receipt (application/zip body); it is verified before it is stored", "user", s.uploadReceipt},
		{"GET", "/api/v1/executions/{id}", "Inspect a stored execution (parsed from its verified receipt)", "user", s.getExecution},
		{"GET", "/api/v1/executions/{id}/receipt", "Download the receipt file", "user", s.downloadReceipt},
		{"GET", "/api/v1/executions/{id}/events", "Timeline events (filter with type, q)", "user", s.executionEvents},
		{"GET", "/api/v1/executions/{id}/logs/{seq}", "Recorded output of one command", "user", s.executionLog},
		{"GET", "/api/v1/executions/{id}/report", "Self-contained HTML report", "user", s.executionReport},
		{"DELETE", "/api/v1/executions/{id}", "Delete a stored execution (project owners)", "user", s.deleteExecution},
		{"GET", "/api/v1/compare", "Compare two stored executions (query a, b)", "user", s.compare},
		{"GET", "/api/v1/audit", "Recent audit events", "admin", s.auditLog},
	}
}

func (s *Server) h(f func(w http.ResponseWriter, r *http.Request, p *principal) error) handler {
	return func(w http.ResponseWriter, r *http.Request, p *principal) {
		if err := f(w, r, p); err != nil {
			writeErr(w, err)
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request, _ *principal) {
	status, code := "ok", 200
	if err := s.db.Ping(); err != nil {
		status, code = "degraded", 503
	}
	writeJSON(w, code, map[string]string{"status": status, "version": s.opts.Version})
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request, _ *principal) {
	writeJSON(w, 200, s.Spec())
}

type userView struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Role      string     `json:"role"`
	Disabled  bool       `json:"disabled"`
	CreatedAt time.Time  `json:"createdAt"`
	LastLogin *time.Time `json:"lastLogin,omitempty"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, _ *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, _ *principal) error {
		var in struct{ Username, Password string }
		if err := decode(r, &in); err != nil {
			return err
		}
		ip := clientIP(r)
		if ok, retry := s.loginIP.allow(ip); !ok {
			w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
			return fail(429, "rate_limited", "too many login attempts")
		}
		ukey := strings.ToLower(in.Username)
		if s.loginUser.count(ukey) >= 5 {
			s.auditRaw(in.Username, ip, "login.locked", "", "too many failures")
			w.Header().Set("Retry-After", "300")
			return fail(429, "rate_limited", "too many failed attempts for this account; try again later")
		}
		var id, hash, role string
		var disabled int
		err := s.db.QueryRow(`SELECT id,password_hash,role,disabled FROM users WHERE username=?`, in.Username).Scan(&id, &hash, &role, &disabled)
		if err != nil {
			CheckPassword(in.Password, dummyHash) // equalise timing
		}
		if err != nil || disabled == 1 || !CheckPassword(in.Password, hash) {
			s.loginUser.allow(ukey) // count the failure
			s.auditRaw(in.Username, ip, "login.failed", "", "")
			return fail(401, "invalid_credentials", "invalid username or password")
		}
		s.loginUser.reset(ukey)
		tok, th := NewToken("rts")
		exp := time.Now().Add(12 * time.Hour)
		if _, err := s.db.Exec(`INSERT INTO tokens(id,user_id,kind,name,token_hash,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, domain.NewID("tk"), id, "session", "login", th, time.Now().Unix(), exp.Unix()); err != nil {
			return err
		}
		_, _ = s.db.Exec(`UPDATE users SET last_login=? WHERE id=?`, time.Now().Unix(), id)
		s.auditRaw(in.Username, ip, "login", "", "")
		writeJSON(w, 200, map[string]any{"token": tok, "expiresAt": exp, "user": map[string]string{"id": id, "username": in.Username, "role": role}})
		return nil
	})(w, r, nil)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, p *principal) {
	_, _ = s.db.Exec(`DELETE FROM tokens WHERE id=? AND kind='session'`, p.TokenID)
	s.audit(r, p, "logout", "", "")
	w.WriteHeader(204)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, p *principal) {
	writeJSON(w, 200, map[string]string{"id": p.UserID, "username": p.Username, "role": p.Role, "tokenKind": p.Kind})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		rows, err := s.db.Query(`SELECT id,username,role,disabled,created_at,last_login FROM users ORDER BY username`)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []userView{}
		for rows.Next() {
			var u userView
			var c int64
			var ll sql.NullInt64
			var d int
			if err := rows.Scan(&u.ID, &u.Username, &u.Role, &d, &c, &ll); err != nil {
				return err
			}
			u.Disabled, u.CreatedAt = d == 1, time.Unix(c, 0)
			if ll.Valid {
				t := time.Unix(ll.Int64, 0)
				u.LastLogin = &t
			}
			out = append(out, u)
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct{ Username, Password, Role string }
		if err := decode(r, &in); err != nil {
			return err
		}
		if !userRe.MatchString(in.Username) {
			return fail(400, "invalid", "username must be 3-64 characters: letters, digits, . _ @ -")
		}
		if in.Role != "admin" && in.Role != "member" && in.Role != "viewer" {
			return fail(400, "invalid", "role must be admin, member or viewer")
		}
		h, err := HashPassword(in.Password)
		if err != nil {
			return fail(400, "invalid", "%v", err)
		}
		id := domain.NewID("us")
		if _, err := s.db.Exec(`INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)`, id, in.Username, h, in.Role, time.Now().Unix()); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fail(409, "conflict", "username already exists")
			}
			return err
		}
		s.audit(r, p, "user.create", in.Username, in.Role)
		writeJSON(w, 201, userView{ID: id, Username: in.Username, Role: in.Role, CreatedAt: time.Now()})
		return nil
	})(w, r, p)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Role     *string `json:"role"`
			Disabled *bool   `json:"disabled"`
		}
		if err := decode(r, &in); err != nil {
			return err
		}
		id := r.PathValue("id")
		if id == p.UserID && (in.Disabled != nil && *in.Disabled || in.Role != nil && *in.Role != "admin") {
			return fail(400, "invalid", "you cannot demote or disable your own account")
		}
		if in.Role != nil {
			if *in.Role != "admin" && *in.Role != "member" && *in.Role != "viewer" {
				return fail(400, "invalid", "role must be admin, member or viewer")
			}
			if _, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, *in.Role, id); err != nil {
				return err
			}
		}
		if in.Disabled != nil {
			v := 0
			if *in.Disabled {
				v = 1
			}
			if _, err := s.db.Exec(`UPDATE users SET disabled=? WHERE id=?`, v, id); err != nil {
				return err
			}
			if v == 1 {
				_, _ = s.db.Exec(`DELETE FROM tokens WHERE user_id=?`, id)
			}
		}
		s.audit(r, p, "user.update", id, "")
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if id == p.UserID {
			return fail(400, "invalid", "you cannot delete your own account")
		}
		var owned int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE created_by=?`, id).Scan(&owned)
		if owned > 0 {
			return fail(409, "conflict", "user created %d projects; disable the account instead", owned)
		}
		if _, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
			return fail(409, "conflict", "user has uploaded receipts; disable the account instead")
		}
		s.audit(r, p, "user.delete", id, "")
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) setPassword(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct{ CurrentPassword, NewPassword string }
		if err := decode(r, &in); err != nil {
			return err
		}
		id := r.PathValue("id")
		if p.Role != "admin" && id != p.UserID {
			return fail(403, "forbidden", "you can only change your own password")
		}
		if id == p.UserID {
			var hash string
			if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, id).Scan(&hash); err != nil || !CheckPassword(in.CurrentPassword, hash) {
				return fail(403, "forbidden", "current password is incorrect")
			}
		}
		h, err := HashPassword(in.NewPassword)
		if err != nil {
			return fail(400, "invalid", "%v", err)
		}
		if res, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, h, id); err != nil {
			return err
		} else if n, _ := res.RowsAffected(); n == 0 {
			return fail(404, "not_found", "user not found")
		}
		_, _ = s.db.Exec(`DELETE FROM tokens WHERE user_id=? AND id<>?`, id, p.TokenID) // end other sessions
		s.audit(r, p, "user.password", id, "")
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		rows, err := s.db.Query(`SELECT id,kind,name,created_at,expires_at,last_used FROM tokens WHERE user_id=? ORDER BY created_at DESC`, p.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, kind, name string
			var c int64
			var e, l sql.NullInt64
			if err := rows.Scan(&id, &kind, &name, &c, &e, &l); err != nil {
				return err
			}
			m := map[string]any{"id": id, "kind": kind, "name": name, "createdAt": time.Unix(c, 0)}
			if e.Valid {
				m["expiresAt"] = time.Unix(e.Int64, 0)
			}
			if l.Valid {
				m["lastUsed"] = time.Unix(l.Int64, 0)
			}
			out = append(out, m)
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		var in struct {
			Name          string `json:"name"`
			ExpiresInDays int    `json:"expiresInDays"`
		}
		if err := decode(r, &in); err != nil {
			return err
		}
		if len(in.Name) == 0 || len(in.Name) > 64 {
			return fail(400, "invalid", "name is required (max 64 characters)")
		}
		if in.ExpiresInDays < 0 || in.ExpiresInDays > 3650 {
			return fail(400, "invalid", "expiresInDays must be between 0 and 3650")
		}
		tok, th := NewToken("rtk")
		var exp any
		if in.ExpiresInDays > 0 {
			exp = time.Now().AddDate(0, 0, in.ExpiresInDays).Unix()
		}
		id := domain.NewID("tk")
		if _, err := s.db.Exec(`INSERT INTO tokens(id,user_id,kind,name,token_hash,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, id, p.UserID, "api", in.Name, th, time.Now().Unix(), exp); err != nil {
			return err
		}
		s.audit(r, p, "token.create", id, in.Name)
		writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "token": tok, "note": "store this token now; it cannot be shown again"})
		return nil
	})(w, r, p)
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request, p *principal) {
	q := `DELETE FROM tokens WHERE id=? AND user_id=?`
	args := []any{r.PathValue("id"), p.UserID}
	if p.Role == "admin" {
		q, args = `DELETE FROM tokens WHERE id=?`, []any{r.PathValue("id")}
	}
	res, err := s.db.Exec(q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, fail(404, "not_found", "token not found"))
		return
	}
	s.audit(r, p, "token.delete", r.PathValue("id"), "")
	w.WriteHeader(204)
}

// ---- projects ----

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		q := `SELECT p.id,p.name,p.description,p.created_at,COALESCE(m.role,'') FROM projects p LEFT JOIN project_members m ON m.project_id=p.id AND m.user_id=?`
		args := []any{p.UserID}
		if p.Role != "admin" {
			q += ` WHERE m.user_id IS NOT NULL`
		}
		rows, err := s.db.Query(q+` ORDER BY p.name`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, desc, role string
			var c int64
			if err := rows.Scan(&id, &name, &desc, &c, &role); err != nil {
				return err
			}
			if p.Role == "admin" {
				role = "owner"
			}
			out = append(out, map[string]any{"id": id, "name": name, "description": desc, "createdAt": time.Unix(c, 0), "role": role})
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		if p.Role == "viewer" {
			return fail(403, "forbidden", "viewers cannot create projects")
		}
		var in struct{ Name, Description string }
		if err := decode(r, &in); err != nil {
			return err
		}
		if !nameRe.MatchString(in.Name) {
			return fail(400, "invalid", "name must be 1-63 characters: letters, digits, . _ -")
		}
		if len(in.Description) > 500 {
			return fail(400, "invalid", "description is too long")
		}
		id := domain.NewID("pr")
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`INSERT INTO projects(id,name,description,created_by,created_at) VALUES(?,?,?,?,?)`, id, in.Name, in.Description, p.UserID, time.Now().Unix()); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fail(409, "conflict", "project name already exists")
			}
			return err
		}
		if _, err := tx.Exec(`INSERT INTO project_members VALUES(?,?,?)`, id, p.UserID, "owner"); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		s.audit(r, p, "project.create", id, in.Name)
		writeJSON(w, 201, map[string]any{"id": id, "name": in.Name, "description": in.Description})
		return nil
	})(w, r, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		role, err := s.needProject(p, id, "reader")
		if err != nil {
			return err
		}
		var name, desc string
		var c int64
		var n int
		_ = s.db.QueryRow(`SELECT name,description,created_at FROM projects WHERE id=?`, id).Scan(&name, &desc, &c)
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM executions WHERE project_id=?`, id).Scan(&n)
		writeJSON(w, 200, map[string]any{"id": id, "name": name, "description": desc, "createdAt": time.Unix(c, 0), "role": role, "executions": n})
		return nil
	})(w, r, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if _, err := s.needProject(p, id, "owner"); err != nil {
			return err
		}
		shas := s.projectShas(id)
		if _, err := s.db.Exec(`DELETE FROM projects WHERE id=?`, id); err != nil {
			return err
		}
		for _, sha := range shas {
			s.removeArtifactIfUnused(sha)
		}
		s.audit(r, p, "project.delete", id, "")
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) projectShas(projectID string) []string {
	rows, err := s.db.Query(`SELECT receipt_sha FROM executions WHERE project_id=?`, projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sha string
		if rows.Scan(&sha) == nil {
			out = append(out, sha)
		}
	}
	return out
}

func (s *Server) removeArtifactIfUnused(sha string) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM executions WHERE receipt_sha=?`, sha).Scan(&n); err == nil && n == 0 {
		_ = os.Remove(s.artifactPath(sha))
	}
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if _, err := s.needProject(p, id, "reader"); err != nil {
			return err
		}
		rows, err := s.db.Query(`SELECT u.id,u.username,m.role FROM project_members m JOIN users u ON u.id=m.user_id WHERE m.project_id=? ORDER BY u.username`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var uid, un, role string
			if err := rows.Scan(&uid, &un, &role); err != nil {
				return err
			}
			out = append(out, map[string]string{"userId": uid, "username": un, "role": role})
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) putMember(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if _, err := s.needProject(p, id, "owner"); err != nil {
			return err
		}
		var in struct{ Role string }
		if err := decode(r, &in); err != nil {
			return err
		}
		if in.Role != "owner" && in.Role != "writer" && in.Role != "reader" {
			return fail(400, "invalid", "role must be owner, writer or reader")
		}
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM users WHERE id=? AND disabled=0`, r.PathValue("userId")).Scan(&exists); err != nil {
			return fail(404, "not_found", "user not found")
		}
		if _, err := s.db.Exec(`INSERT INTO project_members VALUES(?,?,?) ON CONFLICT(project_id,user_id) DO UPDATE SET role=excluded.role`, id, r.PathValue("userId"), in.Role); err != nil {
			return err
		}
		s.audit(r, p, "member.put", id+"/"+r.PathValue("userId"), in.Role)
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if _, err := s.needProject(p, id, "owner"); err != nil {
			return err
		}
		var owners int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM project_members WHERE project_id=? AND role='owner' AND user_id<>?`, id, r.PathValue("userId")).Scan(&owners)
		if owners == 0 {
			return fail(409, "conflict", "a project needs at least one owner")
		}
		_, err := s.db.Exec(`DELETE FROM project_members WHERE project_id=? AND user_id=?`, id, r.PathValue("userId"))
		s.audit(r, p, "member.delete", id+"/"+r.PathValue("userId"), "")
		w.WriteHeader(204)
		return err
	})(w, r, p)
}

// ---- executions / receipts ----

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		id := r.PathValue("id")
		if _, err := s.needProject(p, id, "reader"); err != nil {
			return err
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		rows, err := s.db.Query(`SELECT `+summaryCols+` FROM executions WHERE project_id=? ORDER BY uploaded_at DESC, id DESC LIMIT ?`, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []*receiptSummary{}
		for rows.Next() {
			x, err := scanSummary(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) uploadReceipt(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		pid := r.PathValue("id")
		if _, err := s.needProject(p, pid, "writer"); err != nil {
			return err
		}
		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/zip") && !strings.HasPrefix(ct, "application/octet-stream") {
			return fail(415, "unsupported_media_type", "send the receipt as application/zip")
		}
		tmp, err := os.CreateTemp(filepath.Join(s.opts.DataDir, "tmp"), "upload-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		body := http.MaxBytesReader(w, r.Body, s.opts.MaxUploadBytes)
		n, err := io.Copy(tmp, body)
		tmp.Close()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return fail(413, "too_large", "receipt exceeds the %d MiB upload limit", s.opts.MaxUploadBytes>>20)
			}
			return fail(400, "bad_request", "upload failed: %v", err)
		}
		v, err := receiptcore.Verify(tmp.Name(), receiptcore.VerifyOptions{})
		if err != nil {
			return err
		}
		if !v.Valid {
			writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "invalid_receipt", "message": "receipt failed verification"}, "problems": v.Problems})
			return nil
		}
		m := v.Manifest
		dst := s.artifactPath(v.SHA256)
		if _, err := os.Stat(dst); err != nil {
			// Another upload of identical content may win the race to create the
			// file; the artifact is content-addressed, so either copy is correct.
			if err := os.Rename(tmp.Name(), dst); err != nil {
				if _, statErr := os.Stat(dst); statErr != nil {
					return err
				}
			}
		}
		existingSummary := func() (*receiptSummary, bool) {
			var existing string
			if err := s.db.QueryRow(`SELECT id FROM executions WHERE project_id=? AND receipt_sha=?`, pid, v.SHA256).Scan(&existing); err != nil {
				return nil, false
			}
			x, err := scanSummary(s.db.QueryRow(`SELECT `+summaryCols+` FROM executions WHERE id=?`, existing))
			return x, err == nil
		}
		if x, ok := existingSummary(); ok {
			writeJSON(w, 200, x)
			return nil
		}
		signer := ""
		if v.Signature != nil && v.Signature.Valid {
			signer = v.Signature.Fingerprint
		}
		id := domain.NewID("sx")
		if _, err := s.db.Exec(`INSERT INTO executions(id,project_id,execution_id,receipt_sha,task,status,verification,reproducibility,isolation,signer,runtide_version,size,receipt_created_at,uploaded_at,uploaded_by) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, pid, m.ExecutionID, v.SHA256, m.Task, m.Status, m.Verification, m.Reproducibility, m.Isolation, signer, m.RuntideVersion, n, m.CreatedAt.Unix(), time.Now().Unix(), p.UserID); err != nil {
			// A concurrent upload of the same receipt inserted first: report that one.
			if x, ok := existingSummary(); ok {
				writeJSON(w, 200, x)
				return nil
			}
			return err
		}
		s.audit(r, p, "receipt.upload", id, v.SHA256[:12])
		x, _ := scanSummary(s.db.QueryRow(`SELECT `+summaryCols+` FROM executions WHERE id=?`, id))
		writeJSON(w, 201, x)
		return nil
	})(w, r, p)
}

// execFor loads a stored execution and enforces access.
func (s *Server) execFor(p *principal, id, min string) (*receiptSummary, error) {
	x, err := scanSummary(s.db.QueryRow(`SELECT `+summaryCols+` FROM executions WHERE id=?`, id))
	if err != nil {
		return nil, fail(404, "not_found", "execution not found")
	}
	if _, err := s.needProject(p, x.ProjectID, min); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == 404 {
			return nil, fail(404, "not_found", "execution not found")
		}
		return nil, err
	}
	return x, nil
}

func (s *Server) loadStored(p *principal, id string) (*receiptSummary, *receiptcore.Receipt, *receiptcore.Verification, error) {
	x, err := s.execFor(p, id, "reader")
	if err != nil {
		return nil, nil, nil, err
	}
	rc, v, err := receiptcore.Load(s.artifactPath(x.SHA256), receiptcore.VerifyOptions{})
	if err != nil {
		return nil, nil, nil, fail(500, "corrupt_artifact", "stored receipt failed re-verification: %v", err)
	}
	return x, rc, v, nil
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		x, rc, v, err := s.loadStored(p, r.PathValue("id"))
		if err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{
			"summary": x, "manifest": rc.Manifest, "environment": rc.Environment, "commands": rc.Commands, "services": rc.Services,
			"fileChanges": rc.FileChanges, "verification": rc.Verification, "reproduction": rc.Reproduction, "signature": v.Signature,
			"eventCount": len(rc.Events), "authenticity": v.Authenticity,
		})
		return nil
	})(w, r, p)
}

func safeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Server) downloadReceipt(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		x, err := s.execFor(p, r.PathValue("id"), "reader")
		if err != nil {
			return err
		}
		f, err := os.Open(s.artifactPath(x.SHA256))
		if err != nil {
			return fail(404, "not_found", "artifact missing")
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeFileName(x.ExecutionID)+receiptcore.Extension+`"`)
		s.audit(r, p, "receipt.download", x.ID, "")
		http.ServeContent(w, r, "", time.Time{}, f)
		return nil
	})(w, r, p)
}

func (s *Server) executionEvents(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		_, rc, _, err := s.loadStored(p, r.PathValue("id"))
		if err != nil {
			return err
		}
		types := map[string]bool{}
		for _, t := range r.URL.Query()["type"] {
			types[t] = true
		}
		q := strings.ToLower(r.URL.Query().Get("q"))
		out := []domain.ExecEvent{}
		for _, e := range rc.Events {
			if len(types) > 0 && !types[e.Type] {
				continue
			}
			if q != "" {
				b, _ := json.Marshal(e)
				if !strings.Contains(strings.ToLower(string(b)), q) {
					continue
				}
			}
			out = append(out, e)
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

func (s *Server) executionLog(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		x, rc, _, err := s.loadStored(p, r.PathValue("id"))
		if err != nil {
			return err
		}
		seq, err := strconv.Atoi(r.PathValue("seq"))
		if err != nil {
			return fail(400, "bad_request", "seq must be a number")
		}
		for _, c := range rc.Commands {
			if c.Seq == seq {
				name := rc.LogFiles[c.ID]
				txt, err := receiptcore.ReadLog(s.artifactPath(x.SHA256), name)
				if err != nil {
					return fail(404, "not_found", "no log for that command")
				}
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = io.WriteString(w, txt)
				return nil
			}
		}
		return fail(404, "not_found", "no such command")
	})(w, r, p)
}

func (s *Server) executionReport(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		_, rc, v, err := s.loadStored(p, r.PathValue("id"))
		if err != nil {
			return err
		}
		h, err := receiptcore.HTML(rc, v)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, h)
		return nil
	})(w, r, p)
}

func (s *Server) deleteExecution(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		x, err := s.execFor(p, r.PathValue("id"), "owner")
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(`DELETE FROM executions WHERE id=?`, x.ID); err != nil {
			return err
		}
		s.removeArtifactIfUnused(x.SHA256)
		s.audit(r, p, "execution.delete", x.ID, "")
		w.WriteHeader(204)
		return nil
	})(w, r, p)
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		xa, ra, _, err := s.loadStored(p, r.URL.Query().Get("a"))
		if err != nil {
			return err
		}
		xb, rb, _, err := s.loadStored(p, r.URL.Query().Get("b"))
		if err != nil {
			return err
		}
		writeJSON(w, 200, receiptcore.Compare(ra, rb, s.artifactPath(xa.SHA256), s.artifactPath(xb.SHA256)))
		return nil
	})(w, r, p)
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request, p *principal) {
	s.h(func(w http.ResponseWriter, r *http.Request, p *principal) error {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 1000 {
			limit = 100
		}
		rows, err := s.db.Query(`SELECT ts,actor,ip,action,subject,detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var ts int64
			var a, ip, act, sub, det string
			if err := rows.Scan(&ts, &a, &ip, &act, &sub, &det); err != nil {
				return err
			}
			out = append(out, map[string]any{"time": time.Unix(ts, 0), "actor": a, "ip": ip, "action": act, "subject": sub, "detail": det})
		}
		writeJSON(w, 200, out)
		return nil
	})(w, r, p)
}

// Spec generates the OpenAPI 3.0 document from the route table, so the
// published contract can never drift from the implemented routes.
func (s *Server) Spec() map[string]any {
	paths := map[string]any{}
	for _, rt := range s.routes {
		item, _ := paths[rt.Pattern].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[rt.Pattern] = item
		}
		op := map[string]any{"summary": rt.Summary, "responses": map[string]any{"200": map[string]string{"description": "Success"}, "401": map[string]string{"description": "Missing or invalid token"}, "403": map[string]string{"description": "Not permitted"}, "404": map[string]string{"description": "Not found"}, "429": map[string]string{"description": "Rate limited"}}}
		if rt.Access == "public" {
			op["security"] = []any{}
		} else {
			op["security"] = []any{map[string][]string{"bearerAuth": {}}}
		}
		if rt.Access == "admin" {
			op["x-required-role"] = "admin"
		}
		var params []any
		for _, seg := range strings.Split(rt.Pattern, "/") {
			if strings.HasPrefix(seg, "{") {
				params = append(params, map[string]any{"name": strings.Trim(seg, "{}"), "in": "path", "required": true, "schema": map[string]string{"type": "string"}})
			}
		}
		if rt.Pattern == "/api/v1/compare" {
			params = append(params, map[string]any{"name": "a", "in": "query", "required": true, "schema": map[string]string{"type": "string"}}, map[string]any{"name": "b", "in": "query", "required": true, "schema": map[string]string{"type": "string"}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		item[strings.ToLower(rt.Method)] = op
	}
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return map[string]any{
		"openapi":    "3.0.3",
		"info":       map[string]any{"title": "RUNTIDE Server API", "version": "v1", "description": "Stores and serves Run Receipts. The server never executes commands."},
		"servers":    []any{map[string]string{"url": "/"}},
		"paths":      paths,
		"components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]string{"type": "http", "scheme": "bearer"}}},
	}
}
