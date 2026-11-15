package verification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnv struct {
	dir      string
	runs     []string
	exit     map[string]int
	services map[string]string // name -> health ("" = stopped)
}

func (f *fakeEnv) Dir() string { return f.dir }
func (f *fakeEnv) RunCommand(_ context.Context, name, exe string, _ []string, _ string, _ time.Duration) (CommandOutcome, error) {
	f.runs = append(f.runs, name)
	return CommandOutcome{ExitCode: f.exit[name], Output: "output of " + name, CommandID: "cm_" + name}, nil
}
func (f *fakeEnv) ServiceHealth(n string) (bool, string, bool) {
	h, ok := f.services[n]
	return h != "", h, ok
}
func (f *fakeEnv) RunningServices() []string {
	var out []string
	for n, h := range f.services {
		if h != "" {
			out = append(out, n)
		}
	}
	return out
}

func parse(t *testing.T, y string) *Pipeline {
	p, err := Parse([]byte(y))
	require.NoError(t, err)
	return p
}

func byName(rs []domain.VerificationResult) map[string]domain.VerificationResult {
	m := map[string]domain.VerificationResult{}
	for _, r := range rs {
		m[r.Name] = r
	}
	return m
}

func TestFailedPrerequisiteSkipsDependentsAndFailsOverall(t *testing.T) {
	p := parse(t, `
name: backend
checks:
  - {name: build, command: go, args: [build]}
  - {name: unit-tests, command: go, args: [test], needs: [build]}
  - {name: static, command: go, args: [vet], required: false}
`)
	env := &fakeEnv{dir: t.TempDir(), exit: map[string]int{"build": 2}}
	rs, sum := p.Run(context.Background(), env)
	m := byName(rs)
	assert.Equal(t, domain.VFailed, m["build"].Status)
	assert.Equal(t, domain.VSkipped, m["unit-tests"].Status)
	assert.Contains(t, m["unit-tests"].Error, `prerequisite "build"`)
	assert.Equal(t, domain.VPassed, m["static"].Status)
	assert.Equal(t, "failed", sum.Overall, "a skipped required check must not look like success")
	assert.NotContains(t, env.runs, "unit-tests", "dependent must not execute")
}

func TestAllPassAndOptionalFailureDoesNotFail(t *testing.T) {
	p := parse(t, "name: x\nchecks:\n  - {name: a, command: c}\n  - {name: b, command: c, required: false}\n")
	env := &fakeEnv{dir: t.TempDir(), exit: map[string]int{"b": 1}}
	rs, sum := p.Run(context.Background(), env)
	assert.Equal(t, "passed", sum.Overall)
	assert.Equal(t, domain.VFailed, byName(rs)["b"].Status, "failure is still reported honestly")
	assert.Equal(t, 1, sum.Counts[domain.VFailed])
}

func TestExpectedExitAndOutputRegexp(t *testing.T) {
	p := parse(t, "name: x\nchecks:\n  - {name: neg, command: c, expected_exit: 3}\n  - {name: out, command: c, expect_output: 'output of (out)'}\n  - {name: badout, command: c, expect_output: 'nope'}\n")
	rs, _ := p.Run(context.Background(), &fakeEnv{dir: t.TempDir(), exit: map[string]int{"neg": 3}})
	m := byName(rs)
	assert.Equal(t, domain.VPassed, m["neg"].Status)
	assert.Equal(t, domain.VPassed, m["out"].Status)
	assert.Equal(t, domain.VFailed, m["badout"].Status)
	assert.Equal(t, []string{"command:cm_out"}, m["out"].Evidence)
}

func TestFileHttpTcpChecksUseRealResources(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "report.txt"), []byte("coverage: 91%"), 0o600))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) }))
	defer srv.Close()
	y := "name: x\nchecks:\n" +
		"  - {name: f1, type: file, path: report.txt, contains: 'coverage: 91'}\n" +
		"  - {name: f2, type: file, path: report.txt, contains: 'coverage: 99'}\n" +
		"  - {name: f3, type: file, path: nope.txt}\n" +
		"  - {name: f4, type: file, path: nope.txt, exists: false}\n" +
		"  - {name: h1, type: http, url: '" + srv.URL + "', expect_body: 'ok', timeout_sec: 2}\n" +
		"  - {name: h2, type: http, url: '" + srv.URL + "', expected_status: 404, timeout_sec: 1}\n" +
		"  - {name: t1, type: tcp, port: 1, timeout_sec: 1}\n"
	rs, sum := parse(t, y).Run(context.Background(), &fakeEnv{dir: dir})
	m := byName(rs)
	assert.Equal(t, domain.VPassed, m["f1"].Status)
	assert.Equal(t, domain.VFailed, m["f2"].Status)
	assert.Equal(t, domain.VFailed, m["f3"].Status)
	assert.Equal(t, domain.VPassed, m["f4"].Status)
	assert.Equal(t, domain.VPassed, m["h1"].Status)
	assert.Equal(t, domain.VTimedOut, m["h2"].Status)
	assert.Equal(t, domain.VTimedOut, m["t1"].Status)
	assert.Equal(t, "failed", sum.Overall)
}

func TestServicesChecksAndWhenFile(t *testing.T) {
	y := "name: x\nchecks:\n  - {name: up, type: services_healthy, services: [api], timeout_sec: 1}\n  - {name: leftovers, type: no_running_services, services: [db]}\n  - {name: js, command: npm, when_file: package.json}\n"
	env := &fakeEnv{dir: t.TempDir(), services: map[string]string{"api": "healthy", "db": "healthy", "worker": "healthy"}}
	rs, sum := parse(t, y).Run(context.Background(), env)
	m := byName(rs)
	assert.Equal(t, domain.VPassed, m["up"].Status)
	assert.Equal(t, domain.VFailed, m["leftovers"].Status)
	assert.Contains(t, m["leftovers"].Error, "worker")
	assert.Equal(t, domain.VNotApplicable, m["js"].Status)
	assert.Equal(t, "failed", sum.Overall)
	env.services["api"] = "unhealthy"
	rs, _ = parse(t, y).Run(context.Background(), env)
	assert.Equal(t, domain.VTimedOut, byName(rs)["up"].Status)
}

func TestFailFast(t *testing.T) {
	p := parse(t, "name: x\nfail_fast: true\nchecks:\n  - {name: a, command: c}\n  - {name: b, command: c}\n")
	env := &fakeEnv{dir: t.TempDir(), exit: map[string]int{"a": 1}}
	rs, _ := p.Run(context.Background(), env)
	assert.Equal(t, domain.VSkipped, byName(rs)["b"].Status)
	assert.Equal(t, []string{"a"}, env.runs)
}

func TestValidationRejectsBadPipelines(t *testing.T) {
	bad := map[string]string{
		"cycle":        "name: x\nchecks:\n  - {name: a, command: c, needs: [b]}\n  - {name: b, command: c, needs: [a]}\n",
		"unknown":      "name: x\nchecks:\n  - {name: a, command: c, needs: [zzz]}\n",
		"dup":          "name: x\nchecks:\n  - {name: a, command: c}\n  - {name: a, command: c}\n",
		"type":         "name: x\nchecks:\n  - {name: a, type: magic}\n",
		"remote":       "name: x\nchecks:\n  - {name: a, type: http, url: 'http://example.com'}\n",
		"escape":       "name: x\nchecks:\n  - {name: a, type: file, path: ../../secret}\n",
		"regexp":       "name: x\nchecks:\n  - {name: a, command: c, expect_output: '('}\n",
		"empty":        "name: x\nchecks: []\n",
		"unknownfield": "name: x\nchecks:\n  - {name: a, command: c, bogus: 1}\n",
	}
	for name, y := range bad {
		_, err := Parse([]byte(y))
		assert.Error(t, err, name)
	}
}

func TestSummaryNoneWhenNothingRan(t *testing.T) {
	assert.Equal(t, "none", Summarize(nil).Overall)
	assert.Equal(t, "none", Summarize([]domain.VerificationResult{{Status: domain.VNotApplicable}}).Overall)
}
