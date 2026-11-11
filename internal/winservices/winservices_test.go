package winservices

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListIsReadOnlyAndReal(t *testing.T) {
	svcs, err := List() // works for a standard user: no elevation involved
	require.NoError(t, err)
	require.Greater(t, len(svcs), 20)
	running, protected := 0, 0
	for _, s := range svcs {
		assert.NotEmpty(t, s.Name)
		if s.State == "running" {
			running++
		}
		if s.Protected {
			protected++
			assert.False(t, s.CanStop, s.Name)
		}
	}
	assert.Greater(t, running, 5)
	assert.Greater(t, protected, 0)
}

func TestControlRefusesDangerousOrInvalid(t *testing.T) {
	assert.ErrorContains(t, Control("RpcSs", "stop"), "protected")
	assert.ErrorContains(t, Control("a/b", "stop"), "invalid")
	assert.ErrorContains(t, Control("Spooler", "restart"), "start or stop")
}
