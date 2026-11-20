package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewIDUniqueAndSortable(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < 2000; i++ {
		id := NewID(PrefixExecution)
		assert.False(t, seen[id])
		seen[id] = true
		assert.Regexp(t, `^ex_[0-9a-f]{24}$`, id)
		if prev != "" && id[:15] < prev[:15] {
			t.Fatalf("ids not time sortable: %s < %s", id, prev)
		}
		prev = id
	}
}

func TestExecutionLifecycle(t *testing.T) {
	path := []State{StateCreated, StateConfiguring, StateReady, StateRunning, StateVerifying, StateCompleted}
	for i := 0; i+1 < len(path); i++ {
		assert.True(t, CanTransitionExecution(path[i], path[i+1]), "%s -> %s", path[i], path[i+1])
	}
	// Impossible transitions.
	for _, bad := range [][2]State{
		{StateCreated, StateRunning}, {StateCreated, StateCompleted}, {StateReady, StateVerifying},
		{StateCompleted, StateRunning}, {StateCompleted, StateReady}, {StateCancelled, StateReady},
		{StateFailed, StateRunning}, {StateVerifying, StateRunning}, {StateRunning, StateReady},
	} {
		assert.False(t, CanTransitionExecution(bad[0], bad[1]), "%s -> %s must be rejected", bad[0], bad[1])
	}
	// Terminal states have no exits for executions.
	for _, s := range AllStates {
		if s.Terminal() {
			for _, to := range AllStates {
				assert.False(t, CanTransitionExecution(s, to), "%s -> %s", s, to)
			}
		}
	}
}

func TestWorkspaceCanBeReopened(t *testing.T) {
	assert.True(t, CanTransitionWorkspace(StateCompleted, StateReady))
	assert.True(t, CanTransitionWorkspace(StateInterrupted, StateReady))
	assert.True(t, CanTransitionWorkspace(StateRequiresAttention, StateReady))
	assert.True(t, CanTransitionWorkspace(StateCancelled, StateReady), "a cancelled run does not destroy the workspace")
	assert.False(t, CanTransitionExecution(StateCancelled, StateReady), "but executions never come back")
	assert.False(t, CanTransitionWorkspace(StateCompleted, StateRunning))
	assert.False(t, CanTransitionWorkspace(StateCreated, StateReady))
}

func TestIsolationParse(t *testing.T) {
	for in, want := range map[string]Isolation{"": IsolationManaged, "managed": IsolationManaged, "worktree": IsolationWorktree, "docker": IsolationDocker, "wsl2": IsolationWSL} {
		got, err := ParseIsolation(in)
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := ParseIsolation("vm")
	assert.Error(t, err)
	assert.Equal(t, "worktree", IsolationWorktree.Name())
}
