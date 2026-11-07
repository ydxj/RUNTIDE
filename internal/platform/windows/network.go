//go:build windows

package winapi

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modIphlpapi        = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCP = modIphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUDP = modIphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	afInet                   = 2
	afInet6                  = 23
	tcpTableOwnerPIDListener = 3
	tcpTableOwnerPIDAll      = 5
	udpTableOwnerPID         = 1
)

// Endpoint is one TCP/UDP socket owned by a process.
type Endpoint struct {
	Protocol string // tcp or udp
	Family   string // IPv4 or IPv6
	Addr     netip.Addr
	Port     uint16
	PID      uint32
	State    string // LISTEN, ESTABLISHED... (tcp only)
}

var tcpStates = map[uint32]string{
	1: "CLOSED", 2: "LISTEN", 3: "SYN_SENT", 4: "SYN_RCVD", 5: "ESTABLISHED",
	6: "FIN_WAIT1", 7: "FIN_WAIT2", 8: "CLOSE_WAIT", 9: "CLOSING", 10: "LAST_ACK",
	11: "TIME_WAIT", 12: "DELETE_TCB",
}

func fetchTable(proc *windows.LazyProc, family, class uint32) ([]byte, error) {
	size := uint32(0)
	var buf []byte
	for i := 0; i < 8; i++ {
		var p uintptr
		if len(buf) > 0 {
			p = uintptr(unsafe.Pointer(&buf[0]))
		}
		r, _, _ := proc.Call(p, uintptr(unsafe.Pointer(&size)), 1, uintptr(family), uintptr(class), 0)
		switch windows.Errno(r) {
		case 0:
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			buf = make([]byte, size+4096) // table may grow between calls
			size = uint32(len(buf))
		default:
			return nil, fmt.Errorf("socket table query failed: %w", windows.Errno(r))
		}
	}
	return nil, fmt.Errorf("socket table kept changing; giving up")
}

func port(b []byte) uint16 { return binary.BigEndian.Uint16(b[:2]) }

// ListTCP returns TCP endpoints. When listenOnly is true only LISTEN sockets
// are returned.
func ListTCP(listenOnly bool) ([]Endpoint, error) {
	class := uint32(tcpTableOwnerPIDAll)
	if listenOnly {
		class = tcpTableOwnerPIDListener
	}
	var out []Endpoint
	for _, fam := range []uint32{afInet, afInet6} {
		buf, err := fetchTable(procGetExtendedTCP, fam, class)
		if err != nil {
			return out, err
		}
		if len(buf) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(buf[:4]))
		rows := buf[4:]
		if fam == afInet {
			const rs = 24 // state, laddr, lport, raddr, rport, pid
			for i := 0; i < n && (i+1)*rs <= len(rows); i++ {
				r := rows[i*rs:]
				a, _ := netip.AddrFromSlice(r[4:8])
				out = append(out, Endpoint{Protocol: "tcp", Family: "IPv4", Addr: a, Port: port(r[8:10]),
					PID: binary.LittleEndian.Uint32(r[20:24]), State: tcpStates[binary.LittleEndian.Uint32(r[0:4])]})
			}
		} else {
			const rs = 56 // laddr[16], scope, lport, raddr[16], scope, rport, state, pid
			for i := 0; i < n && (i+1)*rs <= len(rows); i++ {
				r := rows[i*rs:]
				a, _ := netip.AddrFromSlice(r[0:16])
				out = append(out, Endpoint{Protocol: "tcp", Family: "IPv6", Addr: a, Port: port(r[20:22]),
					PID: binary.LittleEndian.Uint32(r[52:56]), State: tcpStates[binary.LittleEndian.Uint32(r[48:52])]})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// ListUDP returns bound UDP endpoints.
func ListUDP() ([]Endpoint, error) {
	var out []Endpoint
	for _, fam := range []uint32{afInet, afInet6} {
		buf, err := fetchTable(procGetExtendedUDP, fam, udpTableOwnerPID)
		if err != nil {
			return out, err
		}
		if len(buf) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(buf[:4]))
		rows := buf[4:]
		if fam == afInet {
			const rs = 12
			for i := 0; i < n && (i+1)*rs <= len(rows); i++ {
				r := rows[i*rs:]
				a, _ := netip.AddrFromSlice(r[0:4])
				out = append(out, Endpoint{Protocol: "udp", Family: "IPv4", Addr: a, Port: port(r[4:6]), PID: binary.LittleEndian.Uint32(r[8:12])})
			}
		} else {
			const rs = 28 // laddr[16], scope, lport, pid
			for i := 0; i < n && (i+1)*rs <= len(rows); i++ {
				r := rows[i*rs:]
				a, _ := netip.AddrFromSlice(r[0:16])
				out = append(out, Endpoint{Protocol: "udp", Family: "IPv6", Addr: a, Port: port(r[20:22]), PID: binary.LittleEndian.Uint32(r[24:28])})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}
