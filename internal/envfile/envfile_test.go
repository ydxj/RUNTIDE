package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = "# db\nDB_HOST=localhost\nDB_PASSWORD=hunter2hunter2\nexport APP_KEY=\"base64:abc\"\nPORT=3000 # inline\nPORT=4000\n1BAD=x\nnoequals\nAPI_TOKEN=changeme\n"

func TestParse(t *testing.T) {
	f, err := ParseReader(".env", strings.NewReader(sample))
	require.NoError(t, err)
	assert.Equal(t, []string{"PORT"}, f.Duplicates)
	assert.Len(t, f.Invalid, 2)
	v, _ := f.value("PORT")
	assert.Equal(t, "4000", v, "last definition wins")
	// Values of sensitive keys never appear in the serialisable display field.
	for _, e := range f.Entries {
		if e.Key == "DB_PASSWORD" {
			assert.NotContains(t, e.Display, "hunter2")
			assert.True(t, e.Sensitive)
		}
	}
	require.Len(t, f.Secrets, 1) // DB_PASSWORD only; placeholder API_TOKEN must not be flagged
	for _, s := range f.Secrets {
		assert.NotEqual(t, "API_TOKEN", s.Key)
	}
}

func TestCompareAndMissing(t *testing.T) {
	a, _ := ParseReader(".env", strings.NewReader("A=1\nB=2\nSECRET_KEY=abc\n"))
	b, _ := ParseReader(".env.example", strings.NewReader("A=1\nB=3\nC=\nSECRET_KEY=zzz\n"))
	d := Compare(a, b)
	assert.Equal(t, []string{"C"}, d.OnlyInB)
	assert.Empty(t, d.OnlyInA)
	require.Len(t, d.Changed, 2)
	for _, c := range d.Changed {
		if c.Key == "SECRET_KEY" {
			assert.NotContains(t, c.A+c.B, "abc")
		}
	}
	assert.Equal(t, []string{"C"}, Missing(b, a))
}

func TestGenerateExampleAndBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(src, []byte("PORT=3000\nDB_PASSWORD=s3cret\nPORT=1\n"), 0o600))
	ex, err := GenerateExample(src, true)
	require.NoError(t, err)
	assert.Contains(t, ex, "PORT=3000")
	assert.Contains(t, ex, "DB_PASSWORD=\n")
	assert.NotContains(t, ex, "s3cret")
	assert.Equal(t, 1, strings.Count(ex, "PORT="), "duplicates collapsed")

	dst := filepath.Join(dir, ".env.example")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))
	bk, err := WriteWithBackup(dst, ex)
	require.NoError(t, err)
	require.NotEmpty(t, bk)
	old, _ := os.ReadFile(bk)
	assert.Equal(t, "old", string(old))
	got, _ := os.ReadFile(dst)
	assert.Equal(t, ex, string(got))
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\n"), 0o600)
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("A=\nB=\n"), 0o600)
	r, err := Inspect(dir)
	require.NoError(t, err)
	require.Len(t, r.Missing, 1)
	assert.Equal(t, []string{"B"}, r.Missing[0].Keys)
}
