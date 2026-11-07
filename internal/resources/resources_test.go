package resources

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSamplerCollectsRealData(t *testing.T) {
	s := NewSampler(5, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Start(ctx) // idempotent
	require.Eventually(t, func() bool { return len(s.History(0)) >= 2 }, 5*time.Second, 100*time.Millisecond)
	l, ok := s.Latest()
	require.True(t, ok)
	assert.Greater(t, l.MemTotal, uint64(0))
	assert.LessOrEqual(t, l.MemPercent, 100.0)
	assert.GreaterOrEqual(t, l.CPUPercent, 0.0)
	s.SetInterval(time.Hour)
	assert.Equal(t, 5*time.Minute, s.Interval())
	s.Stop()
}

func TestHistoryBounded(t *testing.T) {
	s := NewSampler(10, time.Second)
	for i := 0; i < 25; i++ {
		s.sample()
	}
	assert.Len(t, s.History(0), 10)
	assert.Len(t, s.History(3), 3)
}

func TestDisksAndInterfaces(t *testing.T) {
	d := Disks()
	require.NotEmpty(t, d)
	assert.Greater(t, d[0].Total, uint64(0))
	assert.NotEmpty(t, Interfaces())
}
