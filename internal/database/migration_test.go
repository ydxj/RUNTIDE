package database

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1DatabaseMigratesWithoutLosingData(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "old.db")
	// Build a genuine v1 database: apply only migration 1.
	raw, err := openRaw(p)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`)
	require.NoError(t, err)
	_, err = raw.Exec(migrations[0].sql)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO schema_migrations VALUES (1,'initial',1)`)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO workspaces(name,root,description,yaml,approved_hash,created_at,updated_at) VALUES('legacy','C:/x','d','y','h',1,1)`)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO services(name,spec_json,created_at,updated_at) VALUES('svc','{}',1,1)`)
	require.NoError(t, err)
	raw.Close()

	db, note, err := Open(p)
	require.NoError(t, err)
	defer db.Close()
	assert.Contains(t, note, "previous database saved")
	v, _ := db.SchemaVersion()
	assert.Equal(t, migrations[len(migrations)-1].version, v)
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE name='legacy'`).Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM services WHERE name='svc'`).Scan(&n))
	assert.Equal(t, 1, n)
	bks, _ := filepath.Glob(filepath.Join(dir, "backups", "pre-migration-v1-*.db"))
	require.Len(t, bks, 1)
	require.NoError(t, Validate(bks[0]))
}

func TestV2Constraints(t *testing.T) {
	db := open(t)
	_, err := db.Exec(`INSERT INTO projects(id,name,root,created_at,updated_at) VALUES('p1','a','C:/a',1,1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO task_workspaces(id,project_id,name,state,isolation,dir,source_dir,config_yaml,created_at,updated_at) VALUES('w1','p1','t','BOGUS',1,'d','s','y',1,1)`)
	assert.Error(t, err, "state CHECK")
	_, err = db.Exec(`INSERT INTO task_workspaces(id,project_id,name,state,isolation,dir,source_dir,config_yaml,created_at,updated_at) VALUES('w1','nope','t','READY',1,'d','s','y',1,1)`)
	assert.Error(t, err, "foreign key")
}
