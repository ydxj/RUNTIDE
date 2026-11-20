package execution

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/files"
)

var commitRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CreateProject registers an existing directory as a project.
func (m *Manager) CreateProject(name, root, description string) (*domain.Project, error) {
	if !config.ValidName(name) {
		return nil, fmt.Errorf("invalid project name %q (letters, digits, '.', '_', '-'; max 63)", name)
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("project root %q must be an absolute path", root)
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("project root %q is not an existing directory", root)
	}
	p := &domain.Project{Name: name, Root: filepath.Clean(root), Description: description}
	if g := files.Git(root); g.Repo {
		p.GitRoot = g.Root
	}
	if err := m.St.CreateProject(p); err != nil {
		return nil, err
	}
	m.St.Audit("local", "project.create", p.ID, p.Name)
	return p, nil
}

// WorkspaceOptions describe a workspace to create.
type WorkspaceOptions struct {
	Project     string // id or name
	Name        string
	Task        string
	Isolation   string // "" = from config or managed
	ConfigYAML  string // "" = default template
	Dir         string // managed isolation: directory to run in (default: project root)
	AcceptDirty bool   // allow creating a worktree/copy while the source has uncommitted changes
	Commit      string // worktree isolation: check out this commit (detached) instead of HEAD
}

// WorkspacePlan is shown to the user before anything is created.
type WorkspacePlan struct {
	Project   string   `json:"project"`
	Name      string   `json:"name"`
	Isolation string   `json:"isolation"`
	Dir       string   `json:"dir"`
	Creates   []string `json:"creates"`
	Warnings  []string `json:"warnings"`
	Blockers  []string `json:"blockers"`

	cfg     *Config
	iso     domain.Isolation
	src     string
	p       *domain.Project
	cfgYAML string
	commit  string
}

// PlanWorkspace validates the request and describes exactly what creating it
// will do. It changes nothing.
func (m *Manager) PlanWorkspace(o WorkspaceOptions) (*WorkspacePlan, error) {
	p, err := m.St.GetProject(o.Project)
	if err != nil {
		return nil, fmt.Errorf("project %q: %w", o.Project, err)
	}
	if !config.ValidName(o.Name) {
		return nil, fmt.Errorf("invalid workspace name %q", o.Name)
	}
	y := o.ConfigYAML
	if strings.TrimSpace(y) == "" {
		y = DefaultConfigYAML(o.Task)
	}
	cfg, err := ParseConfig(y)
	if err != nil {
		return nil, err
	}
	if o.Isolation != "" {
		cfg.Isolation = o.Isolation
	}
	iso, err := cfg.IsolationLevel()
	if err != nil {
		return nil, err
	}
	src := p.Root
	if o.Dir != "" {
		if !filepath.IsAbs(o.Dir) {
			return nil, fmt.Errorf("directory %q must be absolute", o.Dir)
		}
		src = filepath.Clean(o.Dir)
	}
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("directory %q does not exist", src)
	}
	plan := &WorkspacePlan{Project: p.Name, Name: o.Name, Isolation: iso.Name(), cfg: cfg, iso: iso, src: src, p: p, commit: o.Commit}
	plan.Dir = src
	g := files.Git(src)
	switch iso {
	case domain.IsolationWorktree:
		plan.Dir = filepath.Join(m.Home, "worktrees", p.Name, o.Name)
		if _, err := os.Stat(plan.Dir); err == nil {
			plan.Blockers = append(plan.Blockers, plan.Dir+" already exists")
		}
		if g.Repo {
			plan.Creates = append(plan.Creates, fmt.Sprintf("git worktree at %s (detached HEAD at %.8s; no branch is created and your current branch is not touched)", plan.Dir, g.Head))
			if g.Dirty {
				msg := fmt.Sprintf("%s has uncommitted changes (%d paths). They are NOT part of the worktree and are never modified.", src, len(g.Status))
				plan.Warnings = append(plan.Warnings, msg)
				if !o.AcceptDirty {
					plan.Blockers = append(plan.Blockers, "source has uncommitted changes: acknowledge with --allow-dirty to create the worktree from the last commit")
				}
			}
		} else {
			plan.Creates = append(plan.Creates, fmt.Sprintf("a filtered copy of %s at %s (not a git repository: node_modules, build output and secret files such as .env are not copied)", src, plan.Dir))
		}
	case domain.IsolationManaged:
		plan.Creates = append(plan.Creates, "no files: commands run directly in "+src+" (the project itself)")
		plan.Warnings = append(plan.Warnings, "managed isolation does not separate files: commands can modify the project directly")
		if g.Repo && g.Dirty {
			plan.Warnings = append(plan.Warnings, "the working tree already has uncommitted changes; they will appear in this execution's file changes only if modified again")
		}
	case domain.IsolationDocker:
		plan.Creates = append(plan.Creates, "no files: commands run in a container with "+src+" mounted at /workspace (read-write)")
		plan.Warnings = append(plan.Warnings, "the mounted workspace directory is writable from the container")
	case domain.IsolationWSL:
		plan.Creates = append(plan.Creates, "no files: commands run in WSL distribution "+fmt.Sprint(cfg.WSL)+" against "+src)
		plan.Warnings = append(plan.Warnings, "WSL2 is not a security boundary: Windows drives are mounted and writable inside the distribution")
	}
	plan.Creates = append(plan.Creates, "a RUNTIDE workspace record (state CONFIGURING then READY)")
	if err := cfg.Validate(plan.Dir); err != nil {
		return nil, err
	}
	out, _ := yamlOf(cfg)
	plan.cfgYAML = out
	return plan, nil
}

// CreateWorkspace creates the workspace described by plan (from PlanWorkspace).
func (m *Manager) CreateWorkspace(plan *WorkspacePlan, task string) (*domain.Workspace, error) {
	if len(plan.Blockers) > 0 {
		return nil, fmt.Errorf("cannot create workspace: %s", strings.Join(plan.Blockers, "; "))
	}
	ws := &domain.Workspace{ProjectID: plan.p.ID, Name: plan.Name, Task: task, Isolation: plan.iso, Dir: plan.Dir, SourceDir: plan.src, ConfigYAML: plan.cfgYAML}
	if ws.Task == "" {
		ws.Task = plan.cfg.Task
	}
	if err := m.St.CreateWorkspace(ws); err != nil {
		return nil, err
	}
	fail := func(err error) (*domain.Workspace, error) {
		_ = m.St.TransitionWorkspace(ws.ID, domain.StateConfiguring, "")
		_ = m.St.TransitionWorkspace(ws.ID, domain.StateFailed, err.Error())
		return nil, err
	}
	if err := m.St.TransitionWorkspace(ws.ID, domain.StateConfiguring, ""); err != nil {
		return nil, err
	}
	if plan.iso == domain.IsolationWorktree {
		if err := m.materialise(plan); err != nil {
			return fail(err)
		}
		ws.Worktree = true
		_, _ = m.St.DB.Exec(`UPDATE task_workspaces SET worktree=1 WHERE id=?`, ws.ID)
	}
	if err := m.St.TransitionWorkspace(ws.ID, domain.StateReady, ""); err != nil {
		return nil, err
	}
	m.St.Audit("local", "workspace.create", ws.ID, plan.Project+"/"+plan.Name)
	return m.Workspace(ws.ID)
}

func (m *Manager) materialise(plan *WorkspacePlan) error {
	if err := os.MkdirAll(filepath.Dir(plan.Dir), 0o700); err != nil {
		return err
	}
	g := files.Git(plan.src)
	if g.Repo {
		rev := "HEAD"
		if plan.commit != "" {
			if !commitRe.MatchString(plan.commit) {
				return fmt.Errorf("invalid commit id %q", plan.commit)
			}
			if out, err := git(plan.src, "cat-file", "-e", plan.commit+"^{commit}"); err != nil {
				return fmt.Errorf("commit %.10s does not exist in %s: %s", plan.commit, plan.src, strings.TrimSpace(out))
			}
			rev = plan.commit
		}
		out, err := git(plan.src, "worktree", "add", "--detach", plan.Dir, rev)
		if err != nil {
			return fmt.Errorf("git worktree add failed: %w: %s", err, strings.TrimSpace(out))
		}
		return nil
	}
	return copyFiltered(plan.src, plan.Dir)
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// copyFiltered copies a non-git directory, skipping generated and sensitive content and bounding size.
func copyFiltered(src, dst string) error {
	const maxFiles, maxBytes = 20000, 512 << 20
	var n int
	var total int64
	snap, err := files.Take(files.Options{Root: src})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for rel, info := range snap.Files {
		n++
		total += info.Size
		if n > maxFiles || total > maxBytes {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("directory is too large to copy (more than %d files or %d MiB); use a git repository for worktree isolation", maxFiles, maxBytes>>20)
		}
		from := filepath.Join(src, filepath.FromSlash(rel))
		to := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := copyFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// RemoveWorkspaceDir deletes the directory a worktree-isolated workspace owns.
// It only ever removes paths below <home>/worktrees, refuses when the
// directory holds uncommitted work (unless force) and never touches the
// original project.
func (m *Manager) RemoveWorkspaceDir(wsID string, force bool) error {
	ws, err := m.St.GetWorkspaceRow(wsID)
	if err != nil {
		return err
	}
	if !ws.Worktree {
		return fmt.Errorf("workspace %s does not own a directory (isolation %s); nothing to remove", ws.Name, ws.Isolation.Name())
	}
	root := filepath.Join(m.Home, "worktrees")
	rel, err := filepath.Rel(root, ws.Dir)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to remove %s: it is not inside %s", ws.Dir, root)
	}
	if m.workspaceBusy(ws.ID) {
		return ErrBusy
	}
	if _, err := os.Stat(ws.Dir); err != nil {
		return nil // already gone
	}
	if !force {
		if g := files.Git(ws.Dir); g.Repo && g.Dirty {
			return fmt.Errorf("%s has uncommitted changes (%d paths); review them or pass force to discard", ws.Dir, len(g.Status))
		}
		if g := files.Git(ws.Dir); !g.Repo {
			return fmt.Errorf("%s is a plain copy; its changes cannot be checked against a commit: pass force to discard it", ws.Dir)
		}
	}
	if g := files.Git(ws.SourceDir); g.Repo {
		args := []string{"worktree", "remove", ws.Dir}
		if force {
			args = append(args, "--force")
		}
		if out, err := git(ws.SourceDir, args...); err != nil {
			return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(out))
		}
	} else if err := os.RemoveAll(ws.Dir); err != nil {
		return err
	}
	m.St.Audit("local", "workspace.remove-dir", ws.ID, ws.Dir)
	return nil
}

func (m *Manager) workspaceBusy(wsID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.active {
		if a.ws.ID == wsID {
			return true
		}
	}
	return false
}

// ApproveWorkspace records that the user reviewed the commands of the current configuration.
func (m *Manager) ApproveWorkspace(wsID string) error {
	ws, err := m.St.GetWorkspaceRow(wsID)
	if err != nil {
		return err
	}
	cfg, err := ParseConfig(ws.ConfigYAML)
	if err != nil {
		return err
	}
	if err := cfg.Validate(ws.Dir); err != nil {
		return err
	}
	h, err := cfg.Hash(ws.Dir)
	if err != nil {
		return err
	}
	m.St.Audit("local", "workspace.approve", ws.ID, h[:12])
	return m.St.SetApproval(wsID, h)
}

// ReconfigureWorkspace replaces the configuration (approval is revoked).
func (m *Manager) ReconfigureWorkspace(wsID, yaml string) error {
	ws, err := m.St.GetWorkspaceRow(wsID)
	if err != nil {
		return err
	}
	if m.workspaceBusy(wsID) {
		return ErrBusy
	}
	cfg, err := ParseConfig(yaml)
	if err != nil {
		return err
	}
	if err := cfg.Validate(ws.Dir); err != nil {
		return err
	}
	canon, _ := yamlOf(cfg)
	m.St.Audit("local", "workspace.configure", ws.ID, "")
	return m.St.UpdateWorkspaceConfig(wsID, canon)
}

// ReopenWorkspace makes a finished workspace READY again for another execution.
func (m *Manager) ReopenWorkspace(wsID string) error {
	if m.workspaceBusy(wsID) {
		return ErrBusy
	}
	return m.St.TransitionWorkspace(wsID, domain.StateReady, "")
}
