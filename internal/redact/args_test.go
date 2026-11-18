package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArgs(t *testing.T) {
	r := New()
	out, changed := Args(r, []string{"deploy", "--password", "hunter2", "--api-key=abc123", "--region", "eu", "DB_PASSWORD=x", "https://u:pw@host/x", "-v"})
	assert.True(t, changed)
	assert.Equal(t, []string{"deploy", "--password", Mask, "--api-key=" + Mask, "--region", "eu", "DB_PASSWORD=" + Mask, "https://u:" + Mask + "@host/x", "-v"}, out)
	out, changed = Args(r, []string{"go", "test", "./...", "-run", "TestX"})
	assert.False(t, changed)
	assert.Equal(t, []string{"go", "test", "./...", "-run", "TestX"}, out)
}
