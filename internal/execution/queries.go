package execution

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/verification"
	"gopkg.in/yaml.v3"
)

func yamlOf(c *Config) (string, error) {
	b, err := yaml.Marshal(c)
	return string(b), err
}

func (m *Manager) toWorkspace(r *store.WorkspaceRow) *domain.Workspace {
	w := r.Workspace
	if cfg, err := ParseConfig(r.ConfigYAML); err == nil {
		if h, err := cfg.Hash(r.Dir); err == nil && r.ApprovedHash != "" && r.ApprovedHash == h {
			w.Approved = true
		}
	}
	return &w
}

// Workspace loads a workspace by id, "project/name" or unique name, with Approved computed.
func (m *Manager) Workspace(ref string) (*domain.Workspace, error) {
	r, err := m.St.FindWorkspace(ref)
	if err != nil {
		return nil, err
	}
	return m.toWorkspace(r), nil
}

// Workspaces lists workspaces of a project (all when empty).
func (m *Manager) Workspaces(project string) ([]*domain.Workspace, error) {
	pid := ""
	if project != "" {
		p, err := m.St.GetProject(project)
		if err != nil {
			return nil, err
		}
		pid = p.ID
	}
	rows, err := m.St.ListWorkspaces(pid)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Workspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, m.toWorkspace(r))
	}
	return out, nil
}

// Detail is everything known about an execution (the basis of receipts and the UI).
type Detail struct {
	Execution    *domain.Execution            `json:"execution"`
	Workspace    *domain.Workspace            `json:"workspace"`
	Project      *domain.Project              `json:"project"`
	Environment  json.RawMessage              `json:"environment"`
	Commands     []domain.Command             `json:"commands"`
	Processes    []store.ManagedProcessRecord `json:"processes"`
	Services     []store.ServiceRecord        `json:"services"`
	Verification []domain.VerificationResult  `json:"verification"`
	Summary      verification.Summary         `json:"verificationSummary"`
	FileChanges  []domain.FileChange          `json:"fileChanges"`
	EventCount   int                          `json:"eventCount"`
	Active       bool                         `json:"active"`
	DurationMs   int64                        `json:"durationMs"`
}

// Detail assembles the full view of an execution.
func (m *Manager) Detail(execID string) (*Detail, error) {
	x, err := m.St.GetExecution(execID)
	if err != nil {
		return nil, err
	}
	d := &Detail{Execution: x, Active: m.IsActive(x.ID), Environment: json.RawMessage(x.Environment)}
	if r, err := m.St.GetWorkspaceRow(x.WorkspaceID); err == nil {
		d.Workspace = m.toWorkspace(r)
	}
	if p, err := m.St.GetProject(x.ProjectID); err == nil {
		d.Project = p
	}
	if d.Commands, err = m.St.ListCommands(x.ID); err != nil {
		return nil, err
	}
	if d.Processes, err = m.St.ListManagedProcesses(x.ID); err != nil {
		return nil, err
	}
	if d.Services, err = m.St.ListServices(x.ID); err != nil {
		return nil, err
	}
	if d.Verification, err = m.St.ListVerification(x.ID, false); err != nil {
		return nil, err
	}
	d.Summary = verification.Summarize(d.Verification)
	if d.FileChanges, err = m.St.ListFileChanges(x.ID); err != nil {
		return nil, err
	}
	evs, err := m.St.ListEvents(x.ID, store.EventFilter{Limit: 50000})
	if err != nil {
		return nil, err
	}
	d.EventCount = len(evs)
	if x.StartedAt != nil {
		end := time.Now()
		if x.EndedAt != nil {
			end = *x.EndedAt
		}
		d.DurationMs = end.Sub(*x.StartedAt).Milliseconds()
	}
	if len(d.Environment) == 0 {
		d.Environment = json.RawMessage("{}")
	}
	return d, nil
}

// ReplayDir is where receipts of replays are written.
func ReplayDir(home string) string { return filepath.Join(home, "receipts", "replays") }
