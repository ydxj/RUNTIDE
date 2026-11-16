package winapi

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemInfo(t *testing.T) {
	m, err := Memory()
	require.NoError(t, err)
	assert.Greater(t, m.TotalPhys, m.AvailPhys)
	assert.LessOrEqual(t, m.LoadPercent, uint32(100))
	a, err := SystemCPUTimes()
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	b, err := SystemCPUTimes()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, b.Kernel+b.User, a.Kernel+a.User)
	d := Disks()
	require.NotEmpty(t, d)
	assert.Greater(t, d[0].Total, d[0].Free/2)
	assert.NotEmpty(t, WindowsDir())
}

func TestListProcessesIncludesSelfWithDetails(t *testing.T) {
	ps, err := ListProcesses(true)
	require.NoError(t, err)
	var self *ProcessInfo
	for i := range ps {
		if ps[i].PID == uint32(os.Getpid()) {
			self = &ps[i]
		}
	}
	require.NotNil(t, self)
	assert.Equal(t, uint32(os.Getppid()), self.PPID)
	assert.NotEmpty(t, self.Path)
	assert.NotEmpty(t, self.CmdLine)
	assert.False(t, self.StartTime.IsZero())
	assert.True(t, IsRunning(self.PID))
	assert.False(t, IsRunning(0x7ffffff0))
}

func TestTerminateRejectsPIDReuse(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()
	res, err := TerminateProcess(uint32(cmd.Process.Pid), time.Now().Add(-48*time.Hour))
	assert.Error(t, err)
	assert.False(t, res.Terminated)
	assert.Contains(t, res.Detail, "reused")
}

func TestJobObjectKillsMembers(t *testing.T) {
	j, err := NewJob()
	require.NoError(t, err)
	defer j.Close()
	cmd := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 >nul")
	require.NoError(t, cmd.Start())
	require.NoError(t, j.Assign(uint32(cmd.Process.Pid)))
	n, err := j.ActiveProcesses()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, uint32(1))
	require.NoError(t, j.Terminate(1))
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("job member survived TerminateJobObject")
	}
}

func TestEndpointsForDialedSocket(t *testing.T) {
	tcp, err := ListTCP(false)
	require.NoError(t, err)
	assert.NotEmpty(t, tcp)
	_, err = ListUDP()
	assert.NoError(t, err)
}
