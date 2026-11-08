//go:build windows

package procutil

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHideSetsNoWindowFlags(t *testing.T) {
	cmd := Hide(exec.Command("cmd.exe", "/c", "exit", "0"))
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.HideWindow)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&createNoWindow)
}

func TestHideKeepsExistingFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe")
	Hide(cmd)
	cmd.SysProcAttr.CreationFlags |= 0x00000200 // CREATE_NEW_PROCESS_GROUP
	Hide(cmd)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&createNoWindow)
	assert.NotZero(t, cmd.SysProcAttr.CreationFlags&0x00000200, "other flags are preserved")
}

func TestHiddenCommandStillRuns(t *testing.T) {
	out, err := Hide(exec.Command("cmd.exe", "/c", "echo", "ok")).Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), "ok")
}
