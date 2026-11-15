//go:build windows

package winapi

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func setRunValue(name, command string, enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return k.SetStringValue(name, command)
}

// AutostartEnabled reports whether the Run entry exists and returns its command.
func AutostartEnabled(name string) (bool, string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return err == nil, v
}
