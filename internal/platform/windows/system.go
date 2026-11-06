//go:build windows

package winapi

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MemoryStatus mirrors MEMORYSTATUSEX.
type MemoryStatus struct {
	TotalPhys, AvailPhys, TotalPageFile, AvailPageFile uint64
	LoadPercent                                        uint32
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// Memory returns physical memory counters.
func Memory() (MemoryStatus, error) {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return MemoryStatus{}, err
	}
	return MemoryStatus{m.TotalPhys, m.AvailPhys, m.TotalPageFile, m.AvailPageFile, m.MemoryLoad}, nil
}

// CPUTimes holds cumulative system CPU time in 100ns units.
type CPUTimes struct{ Idle, Kernel, User uint64 }

func ft(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

// SystemCPUTimes returns cumulative times; Kernel includes Idle.
func SystemCPUTimes() (CPUTimes, error) {
	var i, k, u windows.Filetime
	if err := windows.GetSystemTimes(&i, &k, &u); err != nil {
		return CPUTimes{}, err
	}
	return CPUTimes{ft(i), ft(k), ft(u)}, nil
}

// Disk describes a fixed volume.
type Disk struct {
	Path       string
	Total      uint64
	Free       uint64
	FileSystem string
	Label      string
}

// Disks lists fixed local drives.
func Disks() []Disk {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []Disk
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, _ := windows.UTF16PtrFromString(root)
		if windows.GetDriveType(p) != windows.DRIVE_FIXED {
			continue
		}
		var free, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
			continue
		}
		d := Disk{Path: root, Total: total, Free: free}
		var label, fs [64]uint16
		if err := windows.GetVolumeInformation(p, &label[0], 64, nil, nil, nil, &fs[0], 64); err == nil {
			d.Label = windows.UTF16ToString(label[:])
			d.FileSystem = windows.UTF16ToString(fs[:])
		}
		out = append(out, d)
	}
	return out
}

// Hostname returns the machine name.
func Hostname() string { h, _ := os.Hostname(); return h }
