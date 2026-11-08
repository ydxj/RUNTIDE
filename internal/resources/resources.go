// Package resources samples machine-level CPU, memory, disk and network data
// with a single shared sampler and a bounded history.
package resources

import (
	"context"
	"net"
	"sync"
	"time"

	winapi "github.com/runtide/runtide/internal/platform/windows"
)

// Sample is one point in time.
type Sample struct {
	Time         time.Time `json:"time"`
	CPUPercent   float64   `json:"cpuPercent"`
	MemTotal     uint64    `json:"memTotal"`
	MemAvailable uint64    `json:"memAvailable"`
	MemUsed      uint64    `json:"memUsed"`
	MemPercent   float64   `json:"memPercent"`
	CommitTotal  uint64    `json:"commitTotal"`
	CommitUsed   uint64    `json:"commitUsed"`
}

// Disk is volume information.
type Disk struct {
	Path       string  `json:"path"`
	Label      string  `json:"label"`
	FileSystem string  `json:"fileSystem"`
	Total      uint64  `json:"total"`
	Free       uint64  `json:"free"`
	UsedPct    float64 `json:"usedPercent"`
}

// Iface is a network interface summary (counters are not exposed because the
// stable Win32 API for 64-bit counters is not used; see docs).
type Iface struct {
	Name  string   `json:"name"`
	MTU   int      `json:"mtu"`
	Up    bool     `json:"up"`
	Addrs []string `json:"addrs"`
}

// Sampler keeps a ring of samples. It only runs while started, and the
// interval can be changed at runtime.
type Sampler struct {
	mu       sync.RWMutex
	hist     []Sample
	capacity int
	interval time.Duration
	last     winapi.CPUTimes
	haveLast bool
	cancel   context.CancelFunc
	reset    chan struct{}
}

// NewSampler returns a stopped sampler keeping capacity samples.
func NewSampler(capacity int, interval time.Duration) *Sampler {
	if capacity < 10 {
		capacity = 10
	}
	return &Sampler{capacity: capacity, interval: interval, reset: make(chan struct{}, 1)}
}

// Start launches the sampling goroutine. Calling Start twice is a no-op.
func (s *Sampler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()
	go s.loop(ctx)
}

// Stop halts sampling.
func (s *Sampler) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
}

// SetInterval changes the interval (clamped to 1s..5min).
func (s *Sampler) SetInterval(d time.Duration) {
	if d < time.Second {
		d = time.Second
	}
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	s.mu.Lock()
	s.interval = d
	s.mu.Unlock()
	select {
	case s.reset <- struct{}{}:
	default:
	}
}

// Interval returns the current interval.
func (s *Sampler) Interval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.interval
}

func (s *Sampler) loop(ctx context.Context) {
	s.sample()
	t := time.NewTimer(s.Interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.reset:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(s.Interval())
		case <-t.C:
			s.sample()
			t.Reset(s.Interval())
		}
	}
}

func (s *Sampler) sample() {
	cur, err := winapi.SystemCPUTimes()
	m, merr := winapi.Memory()
	if err != nil && merr != nil {
		return
	}
	sm := Sample{Time: time.Now()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		if s.haveLast {
			idle := cur.Idle - s.last.Idle
			total := (cur.Kernel - s.last.Kernel) + (cur.User - s.last.User)
			if total > 0 {
				sm.CPUPercent = float64(total-idle) / float64(total) * 100
			}
		}
		s.last, s.haveLast = cur, true
	}
	if merr == nil {
		sm.MemTotal, sm.MemAvailable = m.TotalPhys, m.AvailPhys
		sm.MemUsed = m.TotalPhys - m.AvailPhys
		if m.TotalPhys > 0 {
			sm.MemPercent = float64(sm.MemUsed) / float64(m.TotalPhys) * 100
		}
		sm.CommitTotal, sm.CommitUsed = m.TotalPageFile, m.TotalPageFile-m.AvailPageFile
	}
	s.hist = append(s.hist, sm)
	if len(s.hist) > s.capacity {
		s.hist = append(s.hist[:0], s.hist[len(s.hist)-s.capacity:]...)
	}
}

// History returns up to n most recent samples (oldest first).
func (s *Sampler) History(n int) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.hist
	if n > 0 && len(h) > n {
		h = h[len(h)-n:]
	}
	return append([]Sample(nil), h...)
}

// Latest returns the newest sample; ok is false before the first sample.
func (s *Sampler) Latest() (Sample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.hist) == 0 {
		return Sample{}, false
	}
	return s.hist[len(s.hist)-1], true
}

// Disks returns fixed volumes.
func Disks() []Disk {
	var out []Disk
	for _, d := range winapi.Disks() {
		x := Disk{Path: d.Path, Label: d.Label, FileSystem: d.FileSystem, Total: d.Total, Free: d.Free}
		if d.Total > 0 {
			x.UsedPct = float64(d.Total-d.Free) / float64(d.Total) * 100
		}
		out = append(out, x)
	}
	return out
}

// Interfaces lists network interfaces.
func Interfaces() []Iface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Iface
	for _, i := range ifs {
		x := Iface{Name: i.Name, MTU: i.MTU, Up: i.Flags&net.FlagUp != 0}
		if as, err := i.Addrs(); err == nil {
			for _, a := range as {
				x.Addrs = append(x.Addrs, a.String())
			}
		}
		out = append(out, x)
	}
	return out
}
