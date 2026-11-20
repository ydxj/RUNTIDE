// Command runtide-mcp is RUNTIDE's Model Context Protocol server (stdio). It
// connects to a running RUNTIDE engine (desktop app or `runtide daemon`) and
// exposes tools at the permission level chosen by the person who launches it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runtide/runtide/internal/api"
	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/mcpserver"
	"github.com/runtide/runtide/internal/paths"
)

func main() {
	level := flag.String("level", mcpserver.LevelControlled, "permission level: read | controlled | sensitive")
	version := flag.Bool("version", false, "print version")
	flag.Parse()
	if *version {
		fmt.Println("runtide-mcp", app.Version)
		return
	}
	home, err := paths.Home()
	if err != nil {
		fatal(err)
	}
	c, err := api.Connect(home)
	if err != nil {
		// Start the background engine next to this binary if needed.
		self, _ := os.Executable()
		cli := filepath.Join(filepath.Dir(self), "runtide.exe")
		if _, serr := os.Stat(cli); serr != nil {
			fatal(fmt.Errorf("no running RUNTIDE engine and runtide.exe was not found next to %s: start the RUNTIDE app or `runtide daemon start`", self))
		}
		if out, rerr := exec.Command(cli, "daemon", "start").CombinedOutput(); rerr != nil {
			fatal(fmt.Errorf("could not start the RUNTIDE engine: %v: %s", rerr, out))
		}
		for i := 0; i < 50 && c == nil; i++ {
			time.Sleep(200 * time.Millisecond)
			c, err = api.Connect(home)
		}
		if c == nil {
			fatal(fmt.Errorf("RUNTIDE engine did not become ready: %v", err))
		}
	}
	srv, err := mcpserver.New(mcpserver.Config{Backend: c, Level: *level, Version: app.Version})
	if err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "runtide-mcp %s: level %s, engine pid %d\n", app.Version, *level, c.Info.PID)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "runtide-mcp:", err)
	os.Exit(1)
}
