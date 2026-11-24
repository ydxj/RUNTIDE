//go:build !windows

package config

import "path/filepath"

func realPath(p string) (string, error) { return filepath.EvalSymlinks(p) }
