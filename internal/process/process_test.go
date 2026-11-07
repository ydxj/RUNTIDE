package process

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	win := `C:\Windows`
	l, _ := Classify(Process{PID: 4, Name: "System"}, 999, win)
	assert.Equal(t, ProtProtected, l)
	l, _ = Classify(Process{PID: 500, Name: "lsass.exe", Accessible: true}, 999, win)
	assert.Equal(t, ProtProtected, l)
	l, _ = Classify(Process{PID: 500, Name: "foo.exe", Path: `C:\Windows\System32\foo.exe`, Accessible: true}, 999, win)
	assert.Equal(t, ProtProtected, l)
	l, _ = Classify(Process{PID: 999, Name: "runtide.exe", Accessible: true}, 999, win)
	assert.Equal(t, ProtProtected, l)
	l, _ = Classify(Process{PID: 7, Name: "explorer.exe", Accessible: true}, 999, win)
	assert.Equal(t, ProtCaution, l)
	l, _ = Classify(Process{PID: 7, Name: "node.exe", Path: `C:\Program Files\nodejs\node.exe`, Accessible: true}, 999, win)
	assert.Equal(t, ProtNone, l)
}

func TestBuildTreeAndFilter(t *testing.T) {
	now := time.Now()
	ps := []Process{
		{PID: 1, PPID: 0, Name: "root", StartTime: now},
		{PID: 2, PPID: 1, Name: "child", StartTime: now.Add(time.Second)},
		{PID: 3, PPID: 2, Name: "grandchild", StartTime: now.Add(2 * time.Second)},
		{PID: 4, PPID: 99, Name: "orphan", StartTime: now},
		{PID: 5, PPID: 6, Name: "reused", StartTime: now}, // parent PID started later: PID reuse
		{PID: 6, PPID: 1, Name: "newer", StartTime: now.Add(time.Hour)},
	}
	roots := BuildTree(ps)
	names := map[string]bool{}
	for _, r := range roots {
		names[r.Name] = true
	}
	assert.True(t, names["root"] && names["orphan"] && names["reused"])
	assert.Len(t, Filter(ps, "child"), 2)
	assert.Len(t, Filter(ps, "3"), 1)
}

func TestSnapshotFindsSelf(t *testing.T) {
	e := NewExplorer(nil)
	ps, err := e.SnapshotWithCPU(200*time.Millisecond, true)
	require.NoError(t, err)
	var self *Process
	for i := range ps {
		if ps[i].PID == uint32(os.Getpid()) {
			self = &ps[i]
		}
	}
	require.NotNil(t, self)
	assert.NotEmpty(t, self.Path)
	assert.Greater(t, self.MemoryBytes, uint64(0))
	assert.NotEmpty(t, self.CmdLine, "own command line must be readable")
	assert.Equal(t, ProtProtected, self.Protection)
}

// TestHelperSleep is a re-exec target: it only sleeps when asked to.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("RUNTIDE_HELPER_SLEEP") == "1" {
		time.Sleep(60 * time.Second)
	}
}

func TestTerminateOwnChildOnly(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "RUNTIDE_HELPER_SLEEP=1")
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()
	e := NewExplorer(nil)
	res, err := e.Terminate(uint32(cmd.Process.Pid), time.Time{})
	require.NoError(t, err)
	assert.True(t, res.Terminated)
	// Refuses itself.
	res, err = e.Terminate(uint32(os.Getpid()), time.Time{})
	assert.ErrorIs(t, err, ErrProtected)
	assert.False(t, res.Terminated)
}
