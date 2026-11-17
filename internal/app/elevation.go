package app

import winapi "github.com/runtide/runtide/internal/platform/windows"

func isElevated() bool { return winapi.IsElevated() }
