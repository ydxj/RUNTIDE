//go:build windows

// Package winservices lists native Windows services and can start or stop
// them on explicit request. This is deliberately separate from RUNTIDE-managed
// development services: it talks to the Service Control Manager, requires the
// user to already hold the right to control the service, and never elevates.
package winservices

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Service is a native Windows service.
type Service struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
	PID         uint32 `json:"pid"`
	CanStop     bool   `json:"canStop"`
	Protected   bool   `json:"protected"`
}

var modAdvapi = windows.NewLazySystemDLL("advapi32.dll")
var procEnum = modAdvapi.NewProc("EnumServicesStatusExW")

type statusProcess struct {
	ServiceType, CurrentState, ControlsAccepted, Win32ExitCode, ServiceSpecificExitCode, CheckPoint, WaitHint, ProcessID, ServiceFlags uint32
}
type enumEntry struct {
	Name, Display *uint16
	Status        statusProcess
}

var states = map[uint32]string{1: "stopped", 2: "start-pending", 3: "stop-pending", 4: "running", 5: "continue-pending", 6: "pause-pending", 7: "paused"}

// protectedServices cannot be stopped through RUNTIDE: stopping them breaks
// Windows or its security stack.
var protectedServices = map[string]bool{
	"rpcss": true, "dcomlaunch": true, "lsm": true, "eventlog": true, "winmgmt": true, "samss": true,
	"cryptsvc": true, "trustedinstaller": true, "windefend": true, "mpssvc": true, "bfe": true, "wuauserv": true,
	"rpceptmapper": true, "profsvc": true, "schedule": true, "power": true, "plugplay": true, "sens": true,
	"gpsvc": true, "usermanager": true, "coreuiregistration": true, "sysmain": true, "securityhealthservice": true,
}

// List enumerates Win32 services with only read access.
func List() ([]Service, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, fmt.Errorf("open service manager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)
	var needed, count, resume uint32
	var buf []byte
	for i := 0; i < 4; i++ {
		var p uintptr
		if len(buf) > 0 {
			p = uintptr(unsafe.Pointer(&buf[0]))
		}
		r, _, e := procEnum.Call(uintptr(scm), 0, uintptr(0x30) /*Win32 own+share*/, 3 /*ALL*/, p, uintptr(len(buf)),
			uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&resume)), 0)
		if r != 0 {
			break
		}
		if errors.Is(e, windows.ERROR_MORE_DATA) || len(buf) == 0 {
			buf = make([]byte, needed+4096)
			resume = 0
			continue
		}
		return nil, e
	}
	var out []Service
	sz := unsafe.Sizeof(enumEntry{})
	for i := uint32(0); i < count && len(buf) > 0; i++ {
		en := (*enumEntry)(unsafe.Pointer(&buf[uintptr(i)*sz]))
		name := windows.UTF16PtrToString(en.Name)
		out = append(out, Service{
			Name: name, DisplayName: windows.UTF16PtrToString(en.Display),
			State: states[en.Status.CurrentState], PID: en.Status.ProcessID,
			CanStop:   en.Status.ControlsAccepted&1 != 0 && !protectedServices[strings.ToLower(name)],
			Protected: protectedServices[strings.ToLower(name)],
		})
	}
	return out, nil
}

// Control starts or stops a service. Errors from Windows (typically access
// denied for non-administrators) are returned unchanged and explained.
func Control(name, action string) error {
	if name == "" || strings.ContainsAny(name, `\/`) {
		return errors.New("invalid service name")
	}
	if action != "start" && action != "stop" {
		return errors.New("action must be start or stop")
	}
	if action == "stop" && protectedServices[strings.ToLower(name)] {
		return fmt.Errorf("%s is a protected Windows service and cannot be stopped from RUNTIDE", name)
	}
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return explain(err)
	}
	defer windows.CloseServiceHandle(scm)
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenService(scm, n, windows.SERVICE_START|windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return explain(err)
	}
	defer windows.CloseServiceHandle(h)
	if action == "start" {
		return explain(windows.StartService(h, 0, nil))
	}
	var st windows.SERVICE_STATUS
	return explain(windows.ControlService(h, windows.SERVICE_CONTROL_STOP, &st))
}

func explain(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return errors.New("access denied by Windows: controlling this service requires administrator rights, and RUNTIDE never elevates itself")
	}
	return err
}
