package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, rec, err := Open(filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	assert.Empty(t, rec)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrationsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	db, _, err := Open(p)
	require.NoError(t, err)
	v, _ := db.SchemaVersion()
	assert.Equal(t, len(migrations), v)
	db.Close()
	db, _, err = Open(p)
	require.NoError(t, err)
	defer db.Close()
	v2, _ := db.SchemaVersion()
	assert.Equal(t, v, v2)
}

func TestConstraints(t *testing.T) {
	db := open(t)
	_, err := db.Exec(`INSERT INTO logs(service,ts,stream,line) VALUES('a',1,'bogus','x')`)
	assert.Error(t, err, "stream CHECK must reject unknown values")
	_, err = db.Exec(`INSERT INTO services(name,spec_json,created_at,updated_at) VALUES('a','{}',1,1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO services(name,spec_json,created_at,updated_at) VALUES('a','{}',1,1)`)
	assert.Error(t, err, "name must be unique")
}

func TestBackupRestore(t *testing.T) {
	db := open(t)
	_, err := db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('k','v',1)`)
	require.NoError(t, err)
	bk := filepath.Join(t.TempDir(), "bk.db")
	require.NoError(t, db.Backup(bk))
	require.NoError(t, Validate(bk))
	_, err = db.Exec(`DELETE FROM settings`)
	require.NoError(t, err)
	live := db.Path
	db.Close()
	keep, err := Restore(live, bk)
	require.NoError(t, err)
	assert.NotEmpty(t, keep)
	db2, _, err := Open(live)
	require.NoError(t, err)
	defer db2.Close()
	var v string
	require.NoError(t, db2.QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v))
	assert.Equal(t, "v", v)
}

func TestCorruptionRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.db")
	require.NoError(t, os.WriteFile(p, []byte("this is not a sqlite database at all, just garbage bytes......................"), 0o600))
	db, rec, err := Open(p)
	require.NoError(t, err)
	defer db.Close()
	assert.Contains(t, rec, "corrupt")
	matches, _ := filepath.Glob(p + ".corrupt-*")
	assert.Len(t, matches, 1)
}

func TestValidateRejectsForeignFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	require.NoError(t, os.WriteFile(p, []byte("nope"), 0o600))
	assert.Error(t, Validate(p))
}

func TestPruneAndReset(t *testing.T) {
	db := open(t)
	old := time.Now().AddDate(0, 0, -40).Unix()
	_, err := db.Exec(`INSERT INTO logs(service,ts,stream,line) VALUES('a',?, 'stdout','old'),('a',?, 'stdout','new')`, old, time.Now().Unix())
	require.NoError(t, err)
	n, err := db.Prune(Retention{LogDays: 30})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	require.NoError(t, db.Reset(t.Context()))
	var c int
	db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&c)
	assert.Zero(t, c)
}
