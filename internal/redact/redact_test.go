package redact

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineRedactsSecrets(t *testing.T) {
	r := New()
	cases := []string{
		"DB_PASSWORD=hunter2",
		`{"api_key": "abcd1234"}`,
		"connecting to postgres://admin:s3cr3t@localhost:5432/db",
		"Authorization: Bearer abcdefghijklmnop",
		"token ghp_" + strings.Repeat("a", 36),
		"AKIAABCDEFGHIJKLMNOP",
	}
	for _, c := range cases {
		out := r.Line(c)
		assert.Contains(t, out, Mask, c)
		assert.NotContains(t, out, "hunter2")
		assert.NotContains(t, out, "s3cr3t")
		assert.NotContains(t, out, "abcdefghijklmnop")
	}
}

func TestLineKeepsNormalText(t *testing.T) {
	r := New()
	in := "Server listening on http://localhost:3000 (pid 1234)"
	assert.Equal(t, in, r.Line(in))
}

func TestDisabled(t *testing.T) {
	r := New()
	r.Enabled = false
	assert.Equal(t, "PASSWORD=x", r.Line("PASSWORD=x"))
}

func TestEnvAndKeys(t *testing.T) {
	assert.True(t, IsSensitiveKey("STRIPE_SECRET_KEY"))
	assert.False(t, IsSensitiveKey("PORT"))
	out := Env([]string{"PORT=3000", "JWT_SECRET=abc"})
	assert.Equal(t, "PORT=3000", out[0])
	assert.Equal(t, "JWT_SECRET="+Mask, out[1])
}

func TestExtraPattern(t *testing.T) {
	r := New(`internal-\d+`)
	assert.Equal(t, "id "+Mask, r.Line("id internal-42"))
}
