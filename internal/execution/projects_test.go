package execution

import (
	"context"
	"fmt"
	"testing"

	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noVerifyYAML(file string) string {
	return fmt.Sprintf(`version: 1
task: build
start:
  - {name: build, command: '%s', env: {EXEC_HELPER: write, HELPER_FILE: %s}}
`, exe(), file)
}

const artifactProfile = `name: has-artifact
checks:
  - {name: artifact, type: file, path: out.txt, contains: built}
`

func (e *env) runWith(t *testing.T, ws *domain.Workspace) *domain.Execution {
	t.Helper()
	require.NoError(t, e.m.ApproveWorkspace(ws.ID))
	x, err := e.m.NewExecution(ws.ID, "")
	require.NoError(t, err)
	require.NoError(t, e.m.Start(context.Background(), x.ID))
	return x
}

func TestReusableProfileVerifiesDifferentWorkspaces(t *testing.T) {
	e := setup(t)
	_, err := e.m.SaveProfile("bad name!", "", artifactProfile)
	assert.Error(t, err, "names are validated")
	_, err = e.m.SaveProfile("has-artifact", "needs out.txt", "name: x\nchecks: []\n")
	assert.Error(t, err, "a profile without checks is rejected")
	_, err = e.m.SaveProfile("has-artifact", "needs out.txt", "name: x\nchecks:\n  - {name: a, type: file, path: p, needs: [ghost]}\n")
	assert.Error(t, err, "dependency errors are caught when saving, not at run time")
	pr, err := e.m.SaveProfile("has-artifact", "needs out.txt", artifactProfile)
	require.NoError(t, err)

	ws1 := e.workspace(t, noVerifyYAML("out.txt"))
	x1 := e.runWith(t, ws1)
	// No verify section and no project default: refusal explains the options.
	_, _, err = e.m.Verify(context.Background(), x1.ID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no verification defined")
	_, sum, err := e.m.VerifyWithProfile(context.Background(), x1.ID, pr.Name)
	require.NoError(t, err)
	assert.Equal(t, "passed", sum.Overall)

	// The same profile applies to a second, unrelated execution and can fail there.
	plan, err := e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "second", Task: "t", Dir: t.TempDir(), ConfigYAML: noVerifyYAML("other.txt")})
	require.NoError(t, err)
	ws2, err := e.m.CreateWorkspace(plan, "t")
	require.NoError(t, err)
	x2 := e.runWith(t, ws2)
	_, sum, err = e.m.VerifyWithProfile(context.Background(), x2.ID, "has-artifact")
	require.NoError(t, err)
	assert.Equal(t, "failed", sum.Overall, "this workspace wrote other.txt, so the shared profile (out.txt) fails here")
}

func TestProjectDefaultProfileAndIsolationDefaults(t *testing.T) {
	e := setup(t)
	_, err := e.m.SaveProfile("has-artifact", "", artifactProfile)
	require.NoError(t, err)
	ws := e.workspace(t, noVerifyYAML("out.txt"))
	pr, err := e.m.UpdateProject("shop", "the shop", domain.ProjectSettings{DefaultProfile: "has-artifact"})
	require.NoError(t, err)
	assert.Equal(t, "has-artifact", pr.Settings.DefaultProfile)
	_, err = e.m.UpdateProject("shop", "", domain.ProjectSettings{DefaultProfile: "ghost"})
	assert.Error(t, err, "a default profile must exist")
	_, err = e.m.UpdateProject("shop", "", domain.ProjectSettings{DefaultIsolation: "quantum"})
	assert.Error(t, err)

	x := e.runWith(t, ws)
	_, sum, err := e.m.Verify(context.Background(), x.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "passed", sum.Overall, "the project's default profile was used")

	// Deleting a profile clears it from projects instead of leaving a dangling default.
	require.NoError(t, e.m.DeleteProfile("has-artifact"))
	got, _ := e.st.GetProject("shop")
	assert.Empty(t, got.Settings.DefaultProfile)
	assert.Error(t, e.m.DeleteProfile("has-artifact"))

	// A project default isolation applies only when the workspace names none.
	_, err = e.m.UpdateProject("shop", "", domain.ProjectSettings{DefaultIsolation: "worktree"})
	require.NoError(t, err)
	plan, err := e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "iso", Task: "t", ConfigYAML: "version: 1\ntask: t\n"})
	if err == nil {
		assert.Equal(t, "worktree", plan.Isolation, "the project default is used and shown in the plan")
	} else {
		assert.Contains(t, err.Error(), "git", "a non-git project cannot use worktree isolation; the error says why instead of downgrading")
	}
	plan, err = e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "iso2", Task: "t", ConfigYAML: "version: 1\ntask: t\nisolation: managed\n"})
	require.NoError(t, err)
	assert.Equal(t, "managed", plan.Isolation, "an explicit isolation wins over the project default")
}

func TestArchivingKeepsDataAndBlocksNewWork(t *testing.T) {
	e := setup(t)
	ws := e.workspace(t, noVerifyYAML("out.txt"))
	x := e.runWith(t, ws)
	_, err := e.m.ArchiveProject("shop", true)
	require.Error(t, err, "a project with a running execution cannot be archived")
	require.NoError(t, e.m.Finish(x.ID))

	p, err := e.m.ArchiveProject("shop", true)
	require.NoError(t, err)
	assert.True(t, p.Archived)
	require.NotNil(t, p.ArchivedAt)
	_, err = e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "new", Task: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "archived")
	got, err := e.st.GetExecution(x.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateCompleted, got.State, "history is untouched")

	p, err = e.m.ArchiveProject("shop", false)
	require.NoError(t, err)
	assert.False(t, p.Archived)
	_, err = e.m.PlanWorkspace(WorkspaceOptions{Project: "shop", Name: "new", Task: "t"})
	assert.NoError(t, err)
}
