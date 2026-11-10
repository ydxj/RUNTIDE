package docker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests replace the CLI runner with canned output. That is the only
// place mocks are used; production code always calls the real docker CLI.

func TestParsePS(t *testing.T) {
	out := []byte(`{"ID":"abcdef0123456789","Names":"web","Image":"nginx","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:8080->80/tcp, :::8080->80/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=web"}
{"ID":"1234","Names":"db","Image":"mysql","State":"exited","Status":"Exited (0)","Ports":"","Labels":""}
`)
	cs, err := parsePS(out)
	require.NoError(t, err)
	require.Len(t, cs, 2)
	assert.Equal(t, "abcdef012345", cs[0].ID)
	assert.Len(t, cs[0].Ports, 2)
	assert.Equal(t, "shop", cs[0].ComposeProject)
	assert.Equal(t, "web", cs[0].ComposeService)
	assert.Contains(t, ComposeProjects(cs), "shop")
	_, err = parsePS([]byte("garbage"))
	assert.Error(t, err)
}

func TestStatusWhenDaemonDown(t *testing.T) {
	old := runner
	defer func() { runner = old }()
	runner = func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, errors.New("cannot connect to the Docker daemon")
	}
	s := GetStatus(context.Background())
	if s.CLIInstalled {
		assert.False(t, s.DaemonAvailable)
		assert.Contains(t, s.Message, "not reachable")
	} else {
		assert.Contains(t, s.Message, "not found")
	}
}

func TestStatusWhenDaemonUp(t *testing.T) {
	old := runner
	defer func() { runner = old }()
	runner = func(ctx context.Context, args ...string) ([]byte, error) { return []byte("27.0.1\n"), nil }
	s := GetStatus(context.Background())
	if s.CLIInstalled {
		assert.True(t, s.DaemonAvailable)
		assert.Equal(t, "27.0.1", s.ServerVersion)
	}
}

func TestActRejectsFlagInjection(t *testing.T) {
	assert.Error(t, Start(context.Background(), "--privileged"))
	assert.Error(t, Stop(context.Background(), "a b"))
	assert.True(t, ValidID("my-container_1.x"))
	var called []string
	old := runner
	defer func() { runner = old }()
	runner = func(ctx context.Context, args ...string) ([]byte, error) { called = args; return nil, nil }
	require.NoError(t, Stop(context.Background(), "web"))
	assert.Equal(t, []string{"stop", "web"}, called)
}
