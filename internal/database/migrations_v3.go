package database

// RUNTIDE 1.0 schema: project archiving and settings, reusable verification
// profiles. Purely additive (new nullable/defaulted columns, one new table).
func init() {
	migrations = append(migrations, migration{3, "projects archive and settings, verification profiles", `
ALTER TABLE projects ADD COLUMN archived_at INTEGER;
ALTER TABLE projects ADD COLUMN settings_json TEXT NOT NULL DEFAULT '{}';
CREATE TABLE verification_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 63),
  description TEXT NOT NULL DEFAULT '',
  pipeline_yaml TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
`})
}
