// Package version holds the product version. It has no dependencies so that
// every binary (including the Linux-buildable server) can use it. The release
// build overrides Version with -ldflags "-X github.com/runtide/runtide/internal/version.Version=X.Y.Z".
package version

// Version is the RUNTIDE version.
var Version = "0.9.1-dev"
