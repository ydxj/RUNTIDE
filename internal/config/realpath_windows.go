//go:build windows

package config

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// realPath returns the path the OS actually resolves for p, following symbolic
// links AND directory junctions (filepath.EvalSymlinks does not follow
// junctions on current Go versions). It fails if p does not exist.
func realPath(p string) (string, error) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) <= len(buf) {
			s := windows.UTF16ToString(buf[:n])
			s = strings.TrimPrefix(s, `\?\UNC\`) // keep UNC shares recognisable
			if strings.HasPrefix(s, `\?\`) {
				s = s[4:]
			}
			return filepath.Clean(s), nil
		}
		buf = make([]uint16, n)
	}
}
