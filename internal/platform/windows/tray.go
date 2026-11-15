//go:build windows

package winapi

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32  = windows.NewLazySystemDLL("user32.dll")
	modShell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassEx     = modUser32.NewProc("RegisterClassExW")
	procCreateWindowEx      = modUser32.NewProc("CreateWindowExW")
	procDefWindowProc       = modUser32.NewProc("DefWindowProcW")
	procGetMessage          = modUser32.NewProc("GetMessageW")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessage     = modUser32.NewProc("DispatchMessageW")
	procPostMessage         = modUser32.NewProc("PostMessageW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procRegisterWindowMsg   = modUser32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu     = modUser32.NewProc("CreatePopupMenu")
	procAppendMenu          = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu      = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu         = modUser32.NewProc("DestroyMenu")
	procGetCursorPos        = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procCreateIconFromRes   = modUser32.NewProc("CreateIconFromResourceEx")
	procShellNotifyIcon     = modShell32.NewProc("Shell_NotifyIconW")
)

const (
	wmUser       = 0x0400
	wmTrayEvent  = wmUser + 1
	wmClose      = 0x0010
	wmDestroy    = 0x0002
	wmLButtonUp  = 0x0202
	wmRButtonUp  = 0x0205
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nifMessage   = 1
	nifIcon      = 2
	nifTip       = 4
	nifInfo      = 0x10
	niifInfo     = 1
	mfString     = 0
	mfSeparator  = 0x800
	tpmReturnCmd = 0x100
	tpmRightBtn  = 0x2
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UTimeout         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type point struct{ X, Y int32 }
type msg struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// TrayItem is one menu entry.
type TrayItem struct {
	Label     string
	Separator bool
	OnClick   func()
}

// Tray is a notification-area icon with a context menu and native balloon
// notifications. It owns a hidden window and a dedicated OS thread.
type Tray struct {
	hwnd      windows.Handle
	icon      windows.Handle
	tip       string
	items     []TrayItem
	onClick   func()
	taskbarCr uint32
	ready     chan error
	closed    chan struct{}
	once      sync.Once
}

var (
	trayMu      sync.Mutex
	trayByHwnd  = map[windows.Handle]*Tray{}
	trayWndProc = syscall.NewCallback(wndProc)
)

func wndProc(hwnd windows.Handle, m uint32, wp, lp uintptr) uintptr {
	trayMu.Lock()
	t := trayByHwnd[hwnd]
	trayMu.Unlock()
	switch {
	case t != nil && m == wmTrayEvent:
		switch uint32(lp) & 0xFFFF {
		case wmLButtonUp:
			if t.onClick != nil {
				go t.onClick()
			}
		case wmRButtonUp:
			t.showMenu()
		}
		return 0
	case t != nil && m == t.taskbarCr && m != 0:
		t.shell(nimAdd) // Explorer restarted: re-add the icon
		return 0
	case m == wmDestroy:
		if t != nil {
			t.shell(nimDelete)
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(m), wp, lp)
	return r
}

// NewTray creates the icon. iconPNG is a PNG (32x32 or larger) used as the icon.
func NewTray(tooltip string, iconPNG []byte, onClick func(), items []TrayItem) (*Tray, error) {
	t := &Tray{tip: tooltip, items: items, onClick: onClick, ready: make(chan error, 1), closed: make(chan struct{})}
	if len(iconPNG) > 0 {
		h, _, _ := procCreateIconFromRes.Call(uintptr(unsafe.Pointer(&iconPNG[0])), uintptr(len(iconPNG)), 1, 0x00030000, 32, 32, 0)
		t.icon = windows.Handle(h)
	}
	go t.loop()
	if err := <-t.ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Tray) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.closed)
	ih, _, _ := modKernel32.NewProc("GetModuleHandleW").Call(0)
	inst := windows.Handle(ih)
	cls, _ := windows.UTF16PtrFromString("RuntideTrayWindow")
	wc := wndClassEx{LpfnWndProc: trayWndProc, HInstance: inst, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	name, _ := windows.UTF16PtrFromString("RUNTIDE tray")
	// HWND_MESSAGE would not receive TaskbarCreated broadcasts, so use a hidden top-level window.
	h, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, uintptr(inst), 0)
	if h == 0 {
		t.ready <- errors.New("create tray window: " + err.Error())
		return
	}
	t.hwnd = windows.Handle(h)
	tb, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(tb)))
	t.taskbarCr = uint32(r)
	trayMu.Lock()
	trayByHwnd[t.hwnd] = t
	trayMu.Unlock()
	if !t.shell(nimAdd) {
		t.ready <- errors.New("Shell_NotifyIcon failed")
		return
	}
	t.ready <- nil
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	trayMu.Lock()
	delete(trayByHwnd, t.hwnd)
	trayMu.Unlock()
}

func (t *Tray) data(flags uint32) notifyIconData {
	d := notifyIconData{HWnd: t.hwnd, UID: 1, UFlags: flags, UCallbackMessage: wmTrayEvent, HIcon: t.icon}
	d.CbSize = uint32(unsafe.Sizeof(d))
	copy(d.SzTip[:], windows.StringToUTF16(t.tip))
	return d
}

func (t *Tray) shell(op uintptr) bool {
	d := t.data(nifMessage | nifIcon | nifTip)
	r, _, _ := procShellNotifyIcon.Call(op, uintptr(unsafe.Pointer(&d)))
	return r != 0
}

// Notify shows a native balloon/toast notification.
func (t *Tray) Notify(title, body string) {
	d := t.data(nifInfo)
	copy(d.SzInfoTitle[:63], windows.StringToUTF16(truncRunes(title, 60)))
	copy(d.SzInfo[:255], windows.StringToUTF16(truncRunes(body, 250)))
	d.DwInfoFlags = niifInfo
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (t *Tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	for i, it := range t.items {
		if it.Separator {
			procAppendMenu.Call(menu, mfSeparator, 0, 0)
			continue
		}
		lbl, _ := windows.UTF16PtrFromString(it.Label)
		procAppendMenu.Call(menu, mfString, uintptr(i+1), uintptr(unsafe.Pointer(lbl)))
	}
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(t.hwnd)) // required so the menu dismisses correctly
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightBtn, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(t.hwnd), 0)
	if cmd > 0 && int(cmd) <= len(t.items) {
		if f := t.items[cmd-1].OnClick; f != nil {
			go f()
		}
	}
}

// Close removes the icon and stops the message loop.
func (t *Tray) Close() {
	t.once.Do(func() {
		procPostMessage.Call(uintptr(t.hwnd), wmClose, 0, 0)
		// WM_CLOSE -> default proc -> DestroyWindow
		<-t.closed
	})
}

// SetAutostart adds or removes an HKCU Run entry. Opt-in only; per-user, no
// administrator rights needed.
func SetAutostart(name, command string, enabled bool) error {
	return setRunValue(name, command, enabled)
}
