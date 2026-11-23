package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/runtide/runtide/internal/config"
	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/verification"
)

// UpdateProject changes a project's description and per-project defaults.
func (m *Manager) UpdateProject(ref, description string, st domain.ProjectSettings) (*domain.Project, error) {
	p, err := m.St.GetProject(ref)
	if err != nil {
		return nil, fmt.Errorf("project %q: %w", ref, err)
	}
	if st.DefaultIsolation != "" {
		if _, err := domain.ParseIsolation(st.DefaultIsolation); err != nil {
			return nil, err
		}
	}
	if st.DefaultProfile != "" {
		if _, err := m.St.GetProfile(st.DefaultProfile); err != nil {
			return nil, fmt.Errorf("verification profile %q does not exist", st.DefaultProfile)
		}
	}
	if len(description) > 500 {
		return nil, errors.New("description is limited to 500 characters")
	}
	if err := m.St.UpdateProject(p.ID, description, st); err != nil {
		return nil, err
	}
	m.St.Audit("local", "project.update", p.ID, p.Name)
	return m.St.GetProject(p.ID)
}

// ArchiveProject hides a project (or restores it). It never deletes anything:
// workspaces, executions and receipts are kept. A project with unfinished
// executions cannot be archived.
func (m *Manager) ArchiveProject(ref string, archived bool) (*domain.Project, error) {
	p, err := m.St.GetProject(ref)
	if err != nil {
		return nil, fmt.Errorf("project %q: %w", ref, err)
	}
	if archived {
		n, err := m.St.ActiveExecutionCount(p.ID)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("project %s has %d execution(s) in progress; cancel or finish them first", p.Name, n)
		}
	}
	if err := m.St.SetProjectArchived(p.ID, archived); err != nil {
		return nil, err
	}
	action := "project.archive"
	if !archived {
		action = "project.unarchive"
	}
	m.St.Audit("local", action, p.ID, p.Name)
	return m.St.GetProject(p.ID)
}

// ---- verification profiles

// ParseProfile validates profile YAML and returns the pipeline.
func ParseProfile(yaml string) (*verification.Pipeline, error) {
	pl, err := verification.Parse([]byte(yaml))
	if err != nil {
		return nil, err
	}
	if err := pl.Validate(); err != nil {
		return nil, err
	}
	if len(pl.Checks) == 0 {
		return nil, errors.New("a verification profile needs at least one check")
	}
	return pl, nil
}

// SaveProfile validates and stores a reusable verification profile. Saving an
// existing name replaces it; verification already recorded is not affected.
func (m *Manager) SaveProfile(name, description, yaml string) (*domain.VerificationProfile, error) {
	if !config.ValidName(name) {
		return nil, fmt.Errorf("invalid profile name %q (letters, digits, '.', '_', '-'; max 63)", name)
	}
	if _, err := ParseProfile(yaml); err != nil {
		return nil, fmt.Errorf("profile %s: %w", name, err)
	}
	p := &domain.VerificationProfile{Name: name, Description: strings.TrimSpace(description), Pipeline: yaml}
	if err := m.St.SaveProfile(p); err != nil {
		return nil, err
	}
	m.St.Audit("local", "profile.save", p.ID, p.Name)
	return p, nil
}

// DeleteProfile removes a profile and clears it from projects that used it as a default.
func (m *Manager) DeleteProfile(ref string) error {
	p, err := m.St.GetProfile(ref)
	if err != nil {
		return fmt.Errorf("profile %q: %w", ref, err)
	}
	projects, err := m.St.ListProjects()
	if err != nil {
		return err
	}
	for _, pr := range projects {
		if pr.Settings.DefaultProfile == p.Name {
			s := pr.Settings
			s.DefaultProfile = ""
			if err := m.St.UpdateProject(pr.ID, pr.Description, s); err != nil {
				return err
			}
		}
	}
	if err := m.St.DeleteProfile(p.ID); err != nil {
		return err
	}
	m.St.Audit("local", "profile.delete", p.ID, p.Name)
	return nil
}

// profilePipeline loads a stored profile as a pipeline.
func (m *Manager) profilePipeline(ref string) (*verification.Pipeline, error) {
	p, err := m.St.GetProfile(ref)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("verification profile %q does not exist", ref)
		}
		return nil, err
	}
	return ParseProfile(p.Pipeline)
}

// VerifyWithProfile runs a stored profile against a running execution.
func (m *Manager) VerifyWithProfile(ctx context.Context, execID, profile string) ([]domain.VerificationResult, verification.Summary, error) {
	pl, err := m.profilePipeline(profile)
	if err != nil {
		return nil, verification.Summary{}, err
	}
	return m.Verify(ctx, execID, pl)
}

// projectDefaultPipeline returns the project's default profile, if it has one.
func (m *Manager) projectDefaultPipeline(projectID string) *verification.Pipeline {
	pr, err := m.St.GetProject(projectID)
	if err != nil || pr.Settings.DefaultProfile == "" {
		return nil
	}
	pl, err := m.profilePipeline(pr.Settings.DefaultProfile)
	if err != nil {
		return nil
	}
	return pl
}
