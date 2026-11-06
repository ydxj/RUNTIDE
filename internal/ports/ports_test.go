package ports

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindsOwnListenerV4AndV6(t *testing.T) {
	l4, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer l4.Close()
	l6, err6 := net.Listen("tcp6", "[::1]:0")
	if err6 == nil {
		defer l6.Close()
	}
	procs, err := process.NewExplorer(nil).Snapshot(false)
	require.NoError(t, err)
	ls, err := List(procs, false)
	require.NoError(t, err)

	p4 := uint16(l4.Addr().(*net.TCPAddr).Port)
	hit := Find(ls, p4)
	require.NotEmpty(t, hit)
	assert.Equal(t, uint32(os.Getpid()), hit[0].PID)
	assert.Equal(t, "IPv4", hit[0].Family)
	assert.Equal(t, "loopback", hit[0].Exposure)
	assert.False(t, hit[0].Stale)
	assert.NotEmpty(t, hit[0].ProcessName)

	if err6 == nil {
		p6 := uint16(l6.Addr().(*net.TCPAddr).Port)
		h6 := Find(ls, p6)
		require.NotEmpty(t, h6)
		assert.Equal(t, "IPv6", h6[0].Family)
		assert.Equal(t, uint32(os.Getpid()), h6[0].PID)
	}
}

func TestIsFreeAndClose(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	p := uint16(l.Addr().(*net.TCPAddr).Port)
	free, hits, err := IsFree(p)
	require.NoError(t, err)
	assert.False(t, free)
	assert.NotEmpty(t, hits)
	l.Close()
	time.Sleep(100 * time.Millisecond)
	free, _, _ = IsFree(p)
	assert.True(t, free)
}

func TestUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc.Close()
	procs, _ := process.NewExplorer(nil).Snapshot(false)
	ls, err := List(procs, true)
	require.NoError(t, err)
	port := uint16(pc.LocalAddr().(*net.UDPAddr).Port)
	found := false
	for _, l := range Find(ls, port) {
		if l.Protocol == "udp" && l.PID == uint32(os.Getpid()) {
			found = true
		}
	}
	assert.True(t, found)
}

func TestConflictMarking(t *testing.T) {
	ls := []Listener{
		{Protocol: "tcp", Port: 3000, PID: 1, Address: "0.0.0.0"},
		{Protocol: "tcp", Port: 3000, PID: 2, Address: "127.0.0.1"},
		{Protocol: "tcp", Port: 80, PID: 3},
	}
	markConflicts(ls)
	assert.NotEmpty(t, ls[0].Conflict)
	assert.NotEmpty(t, ls[1].Conflict)
	assert.Empty(t, ls[2].Conflict)
	assert.Equal(t, "all-interfaces", exposure("::"))
	assert.Equal(t, "network", exposure("192.168.1.5"))
}
