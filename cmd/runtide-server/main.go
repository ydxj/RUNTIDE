// Command runtide-server is the self-hosted RUNTIDE server: accounts, projects,
// receipt storage and a versioned HTTP API. It binds loopback by default and
// never executes commands.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/server"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	var o server.Options
	fs := flag.NewFlagSet("runtide-server", flag.ExitOnError)
	fs.StringVar(&o.Addr, "addr", envOr("RUNTIDE_SERVER_ADDR", "127.0.0.1:8484"), "listen address (loopback by default)")
	fs.StringVar(&o.DataDir, "data", envOr("RUNTIDE_SERVER_DATA", "runtide-server-data"), "data directory (database and receipt artifacts)")
	fs.StringVar(&o.TLSCert, "tls-cert", os.Getenv("RUNTIDE_SERVER_TLS_CERT"), "TLS certificate (PEM)")
	fs.StringVar(&o.TLSKey, "tls-key", os.Getenv("RUNTIDE_SERVER_TLS_KEY"), "TLS private key (PEM)")
	fs.BoolVar(&o.AllowRemote, "allow-remote", os.Getenv("RUNTIDE_SERVER_ALLOW_REMOTE") == "1", "allow binding a non-loopback address")
	fs.BoolVar(&o.BehindTLSProxy, "behind-tls-proxy", os.Getenv("RUNTIDE_SERVER_BEHIND_TLS_PROXY") == "1", "acknowledge that a TLS-terminating proxy protects this listener")
	fs.Int64Var(&o.MaxUploadBytes, "max-upload-mb", 256, "maximum receipt upload size in MiB")
	version := fs.Bool("version", false, "print the version and exit")
	_ = fs.Parse(os.Args[1:])
	o.MaxUploadBytes <<= 20
	o.Version = app.Version
	o.AdminPassword = os.Getenv("RUNTIDE_ADMIN_PASSWORD")
	if *version {
		fmt.Println("runtide-server", app.Version)
		return
	}
	s, err := server.New(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtide-server:", err)
		os.Exit(1)
	}
	defer func() { _ = s.Close() }()
	if s.BootstrapPassword != "" {
		fmt.Fprintf(os.Stderr, "\nFirst start: administrator account created.\n  username: admin\n  password: %s\nThis password is shown once. Change it with POST /api/v1/users/{id}/password.\n\n", s.BootstrapPassword)
	}
	scheme := "http"
	if o.TLSCert != "" {
		scheme = "https"
	}
	fmt.Fprintf(os.Stderr, "runtide-server %s listening on %s://%s (data: %s)\n", app.Version, scheme, o.Addr, o.DataDir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := s.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "runtide-server:", err)
		os.Exit(1)
	}
}
