package receipts

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestructiveHeuristicFlagsRiskyCommandsAndStaysQuietOtherwise(t *testing.T) {
	for _, c := range []struct {
		exe  string
		args []string
		want bool
	}{
		{`C:\Windows\System32\cmd.exe`, []string{"/c", "del", "x"}, true},
		{"rm", []string{"-rf", "build"}, true},
		{"git", []string{"clean", "-fdx"}, true},
		{"git", []string{"reset", "--hard"}, true},
		{"docker", []string{"system", "prune", "-f"}, true},
		{"npm", []string{"publish"}, true},
		{"powershell", []string{"-File", "x.ps1"}, true},
		{"npm", []string{"test"}, false},
		{"go", []string{"build", "./..."}, false},
		{"git", []string{"status"}, false},
		{"node", []string{"server.js"}, false},
	} {
		got := destructiveReasons(c.exe, c.args)
		assert.Equal(t, c.want, len(got) > 0, "%s %v -> %v", c.exe, c.args, got)
	}
}

func cmp(statusA, statusB, vA, vB string) *Comparison {
	c := &Comparison{}
	c.A.Status, c.B.Status = statusA, statusB
	c.Verification.Overall = [2]string{vA, vB}
	return c
}

func TestOutcomeClassification(t *testing.T) {
	full := &ReplayPlan{Class: ReproFull}
	partial := &ReplayPlan{Class: ReproPartial}

	o, _ := classifyOutcome(full, "COMPLETED", cmp("COMPLETED", "COMPLETED", "passed", "passed"), nil)
	assert.Equal(t, OutcomeReproduced, o)

	o, why := classifyOutcome(partial, "COMPLETED", cmp("COMPLETED", "COMPLETED", "passed", "passed"), nil)
	assert.Equal(t, OutcomePartial, o, "known-missing inputs mean equal results are not proof of a faithful reproduction")
	assert.NotEmpty(t, why)

	c := cmp("COMPLETED", "COMPLETED", "passed", "passed")
	c.Commands.Changed = []CommandChange{{Key: "start:build"}}
	o, _ = classifyOutcome(full, "COMPLETED", c, nil)
	assert.Equal(t, OutcomePartial, o, "same verdict but different command outcomes")

	o, _ = classifyOutcome(full, "FAILED", cmp("COMPLETED", "FAILED", "passed", "failed"), nil)
	assert.Equal(t, OutcomeCouldNot, o, "a different result is not a reproduction")

	o, _ = classifyOutcome(full, "FAILED", cmp("FAILED", "FAILED", "failed", "failed"), nil)
	assert.Equal(t, OutcomeReproduced, o, "reproducing a failure is a reproduction")

	c = cmp("COMPLETED", "COMPLETED", "passed", "passed")
	c.Verification.StatusChanges = []StatusChange{{Name: "x"}}
	o, _ = classifyOutcome(full, "COMPLETED", c, nil)
	assert.Equal(t, OutcomeCouldNot, o)

	o, _ = classifyOutcome(full, "", nil, nil)
	assert.Equal(t, OutcomeCouldNot, o)
	o, _ = classifyOutcome(full, "FAILED", cmp("COMPLETED", "COMPLETED", "passed", "passed"), errors.New("boom"))
	assert.Equal(t, OutcomeCouldNot, o)
}

func TestPlanNeverSerialisesNullLists(t *testing.T) {
	// A metadata-only plan returns early; every list must still be [] not null,
	// because the desktop UI iterates them.
	r := &Receipt{}
	r.Reproduction.Class = ReproMetadataOnly
	p := PlanReplay(r, ReplayOptions{})
	b, err := json.Marshal(p)
	require.NoError(t, err)
	for _, k := range []string{"reasons", "steps", "needsEnv", "toolIssues", "missing", "blockers", "dependencies", "destructive", "permissions"} {
		assert.Contains(t, string(b), `"`+k+`":[`, "%s must be an array", k)
	}
	assert.NotContains(t, string(b), ":null")
}
