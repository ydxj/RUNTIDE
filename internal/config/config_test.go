package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = `
name: fullstack
services:
  - name: db
    command: mysqld
  - name: backend
    command: php
    args: [artisan, serve]
    working_directory: ./backend
    depends_on: [db]
    healthcheck:
      type: http
      url: http://localhost:8000
  - name: frontend
    command: npm
    args: [run, dev]
    working_directory: frontend
    depends_on: [backend]
  - name: worker
    command: php
    args: [artisan, queue:work]
    depends_on: [db]
`

func TestParseAndOrder(t *testing.T) {
	w, err := Parse([]byte(sample))
	require.NoError(t, err)
	require.NoError(t, w.Validate())
	layers, err := w.StartOrder()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"db"}, {"backend", "worker"}, {"frontend"}}, layers)
	stop, _ := w.StopOrder()
	assert.Equal(t, []string{"frontend"}, stop[0])
	assert.Equal(t, []string{"db"}, stop[2])
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte("name: x\nservices: []\nbogus: 1\n"))
	assert.Error(t, err)
}

func TestCycle(t *testing.T) {
	w, _ := Parse([]byte("name: c\nservices:\n - {name: a, command: x, depends_on: [b]}\n - {name: b, command: x, depends_on: [c]}\n - {name: c, command: x, depends_on: [a]}\n"))
	err := w.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependency cycle")
}

func TestUnknownDependency(t *testing.T) {
	w, _ := Parse([]byte("name: c\nservices:\n - {name: a, command: x, depends_on: [zzz]}\n"))
	assert.ErrorContains(t, w.Validate(), "unknown service")
}

func TestPathTraversal(t *testing.T) {
	for _, bad := range []string{`..\evil`, `../evil`, `a/../../evil`, `C:\Windows`, `\\server\share`, `/etc`} {
		_, err := ResolveDir(`C:\proj`, bad)
		assert.Error(t, err, bad)
	}
	d, err := ResolveDir(`C:\proj`, `./sub/dir`)
	require.NoError(t, err)
	assert.Contains(t, d, "sub")
}

func TestHealthValidation(t *testing.T) {
	assert.False(t, IsLoopbackHost("example.com"))
	assert.True(t, IsLoopbackHost("[::1]"))
	w, _ := Parse([]byte("name: h\nservices:\n - name: a\n   command: x\n"))
	_ = w
	w2, _ := Parse([]byte("name: h\nservices:\n  - name: a\n    command: x\n    healthcheck: {type: http, url: 'http://example.com/'}\n"))
	assert.ErrorContains(t, w2.Validate(), "not loopback")
}

func TestBadNamesAndEnv(t *testing.T) {
	w, _ := Parse([]byte("name: ok\nservices:\n  - name: 'bad name!'\n    command: x\n    env: {'1BAD': v}\n"))
	err := w.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid name")
	assert.Contains(t, err.Error(), "environment variable")
}

func TestDefaults(t *testing.T) {
	s := ServiceSpec{Name: "a", Command: "x", Restart: RestartSpec{Policy: "on-failure"}}
	s.ApplyDefaults()
	assert.Equal(t, 3, s.Restart.MaxRetries)
	assert.Equal(t, 10, s.StopTimeoutSec)
}
