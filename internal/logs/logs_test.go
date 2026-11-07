package logs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRingIsBounded(t *testing.T) {
	h := NewHub(100, nil)
	for i := 0; i < 250; i++ {
		h.Append("svc", "stdout", "line")
	}
	r := h.Recent(0, "svc")
	assert.Len(t, r, 100)
	assert.Equal(t, uint64(250), r[len(r)-1].ID)
}

func TestRedactionAndLevels(t *testing.T) {
	h := NewHub(100, nil)
	e := h.Append("a", "stderr", "ERROR: connect failed password=secret123")
	assert.NotContains(t, e.Line, "secret123")
	assert.Equal(t, "error", e.Level)
	assert.Equal(t, "warn", h.Append("a", "stdout", "warning: deprecated api").Level)
	assert.Equal(t, "", h.Append("a", "stdout", "ready").Level)
}

func TestCaptureTruncatesLongLines(t *testing.T) {
	h := NewHub(100, nil)
	long := strings.Repeat("x", 200*1024)
	h.Capture("a", "stdout", strings.NewReader("first\n"+long+"\nlast"))
	r := h.Recent(0, "a")
	require.Len(t, r, 3)
	assert.Equal(t, "first", r[0].Line)
	assert.LessOrEqual(t, len(r[1].Line), MaxLineBytes+40)
	assert.Equal(t, "last", r[2].Line)
}

func TestSubscribeFiltersAndDoesNotBlock(t *testing.T) {
	h := NewHub(100, nil)
	ch, cancel := h.Subscribe("a")
	defer cancel()
	h.Append("b", "stdout", "ignored")
	h.Append("a", "stdout", "seen")
	select {
	case e := <-ch:
		assert.Equal(t, "seen", e.Line)
	case <-time.After(time.Second):
		t.Fatal("no entry")
	}
	for i := 0; i < 5000; i++ { // overflow a slow consumer: must not deadlock
		h.Append("a", "stdout", "x")
	}
	assert.Greater(t, h.Dropped(), uint64(0))
}

func TestPersistenceAndQuery(t *testing.T) {
	db, _, err := database.Open(filepath.Join(t.TempDir(), "l.db"))
	require.NoError(t, err)
	defer db.Close()
	h := NewHub(100, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := h.StartPersistence(ctx, db.DB)
	h.Append("api", "stdout", "hello 100%_literal")
	h.Append("api", "stderr", "boom error")
	h.Append("web", "stdout", "other")
	cancel()
	<-done
	got, err := QueryDB(db.DB, Query{Services: []string{"api"}})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	got, _ = QueryDB(db.DB, Query{Search: "100%_"})
	assert.Len(t, got, 1, "LIKE wildcards must be escaped")
	got, _ = QueryDB(db.DB, Query{Level: "error"})
	assert.Len(t, got, 1)
}
