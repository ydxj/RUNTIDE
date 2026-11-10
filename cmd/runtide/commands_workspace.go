package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/runtide/runtide/internal/api"
	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/docker"
	"github.com/runtide/runtide/internal/paths"
	"github.com/runtide/runtide/internal/workspaces"
	"github.com/runtide/runtide/internal/wsl"
	"github.com/spf13/cobra"
)

type wsRow struct {
	workspaces.Record
	Health  string `json:"health"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
}

func workspacesCmd() *cobra.Command {
	return &cobra.Command{Use: "workspaces", Short: "List workspaces", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return withBackend(func(b backend) error {
			ws, err := call[[]wsRow](b, "workspaces.list", nil)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(ws)
			}
			rows := [][]string{}
			for _, w := range ws {
				ap := "no"
				if w.Approved {
					ap = "yes"
				}
				rows = append(rows, []string{w.Name, w.Health, fmt.Sprintf("%d/%d", w.Running, w.Total), ap, w.Root})
			}
			table([]string{"WORKSPACE", "HEALTH", "RUNNING", "APPROVED", "ROOT"}, rows)
			if len(rows) == 0 {
				fmt.Println("No workspaces yet. Import one with `runtide workspace import <file.yaml>`.")
			}
			return nil
		})
	}}
}

func printProgress(p workspaces.Progress) {
	switch p.Phase {
	case "starting":
		fmt.Printf("  %s %-18s %s\n", color("36", "starting"), p.Service, trunc(p.Detail, 70))
	case "ready":
		fmt.Printf("  %s %-18s %s\n", color("32", "ready   "), p.Service, p.Detail)
	case "failed":
		fmt.Printf("  %s %-18s %s\n", color("31", "failed  "), p.Service, trunc(p.Detail, 100))
	case "stopping":
		fmt.Printf("  stopping %-18s\n", p.Service)
	case "stopped":
		fmt.Printf("  %s %-18s\n", color("32", "stopped "), p.Service)
	case "rolled-back":
		fmt.Printf("  %s %-18s %s\n", color("33", "rolled back"), p.Service, p.Detail)
	}
}

func runWithProgress(c *api.Client, method, name string, only []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = c.Events(ctx, func(topic string, data json.RawMessage) {
			if topic != "progress" || jsonOut {
				return
			}
			var p workspaces.Progress
			if json.Unmarshal(data, &p) == nil && p.Workspace == name {
				printProgress(p)
			}
		})
	}()
	time.Sleep(150 * time.Millisecond) // let the event stream attach
	return c.Call(method, map[string]any{"name": name, "only": only}, nil)
}

func workspaceCmd() *cobra.Command {
	ws := &cobra.Command{Use: "workspace", Short: "Manage a workspace (a project's whole set of services)"}

	var yes bool
	up := &cobra.Command{Use: "up <name> [service...]", Short: "Start a workspace in dependency order", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(jsonOut)
			if err != nil {
				return err
			}
			name := args[0]
			rec, err := call[workspaces.Record](&remoteBackend{c}, "workspaces.get", map[string]string{"name": name})
			if err != nil {
				return err
			}
			if !rec.Approved {
				fmt.Printf("Workspace %q has not been authorised to run. It will execute these commands on this machine:\n\n", name)
				pv, err := call[workspaces.Preview](&remoteBackend{c}, "workspaces.inspect", map[string]string{"yaml": rec.YAML, "root": rec.Root})
				if err != nil {
					return err
				}
				for _, line := range pv.Commands {
					fmt.Println("  ", line)
				}
				fmt.Println()
				if !yes {
					ok, err := confirm("Authorise and run these commands?")
					if err != nil {
						return err
					}
					if !ok {
						return &exitError{exitFailure, "cancelled; nothing was started"}
					}
				}
				if err := c.Call("workspaces.approve", map[string]string{"name": name}, nil); err != nil {
					return err
				}
			}
			if !jsonOut {
				fmt.Printf("Starting %s...\n", name)
			}
			if err := runWithProgress(c, "workspaces.up", name, args[1:]); err != nil {
				return err
			}
			if !jsonOut {
				fmt.Printf("%s is up.\n", name)
			}
			return nil
		}}
	up.Flags().BoolVarP(&yes, "yes", "y", false, "authorise execution without asking")

	down := &cobra.Command{Use: "down <name>", Short: "Stop a workspace in reverse dependency order", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(jsonOut)
			if err != nil {
				return err
			}
			if err := runWithProgress(c, "workspaces.down", args[0], nil); err != nil {
				return err
			}
			if !jsonOut {
				fmt.Printf("%s is down.\n", args[0])
			}
			return nil
		}}
	restart := &cobra.Command{Use: "restart <name>", Short: "Stop then start a workspace", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(jsonOut)
			if err != nil {
				return err
			}
			return runWithProgress(c, "workspaces.restart", args[0], nil)
		}}

	show := &cobra.Command{Use: "status <name>", Aliases: []string{"show"}, Short: "Show a workspace's services and health", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				st, err := call[workspaces.Status](b, "workspaces.status", map[string]string{"name": args[0]})
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(st)
				}
				fmt.Printf("%s: %s (%d/%d running)\n\n", st.Name, st.Health, st.Running, st.Total)
				rows := [][]string{}
				for _, s := range st.Services {
					state, hl, pid := "stopped", "-", "-"
					if s.Runtime != nil {
						state, hl = string(s.Runtime.State), string(s.Runtime.Health)
						if s.Runtime.PID > 0 {
							pid = strconv.Itoa(s.Runtime.PID)
						}
					}
					rows = append(rows, []string{strconv.Itoa(s.Layer + 1), s.Name, state, hl, pid, strings.Join(s.Spec.DependsOn, ",")})
				}
				table([]string{"ORDER", "SERVICE", "STATE", "HEALTH", "PID", "DEPENDS ON"}, rows)
				return nil
			})
		}}

	var root string
	var replace bool
	imp := &cobra.Command{Use: "import <file.yaml>", Short: "Validate and register a workspace file (does not run anything)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := filepath.Abs(args[0])
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			if root == "" {
				root = filepath.Dir(file)
			}
			root, _ = filepath.Abs(root)
			return withBackend(func(b backend) error {
				pv, err := call[workspaces.Preview](b, "workspaces.inspect", map[string]string{"yaml": string(data), "root": root})
				if err != nil {
					return err
				}
				fmt.Printf("Workspace %q (root %s) defines %d service(s):\n", pv.Name, pv.Root, len(pv.Services))
				for _, c := range pv.Commands {
					fmt.Println("  ", c)
				}
				for _, w := range pv.Warnings {
					fmt.Println(color("33", "warning: ") + w)
				}
				if pv.Exists && !replace {
					return fmt.Errorf("workspace %q already exists; use --replace to overwrite it", pv.Name)
				}
				if !yes {
					ok, err := confirm("Register this workspace? (Nothing will be started.)")
					if err != nil {
						return err
					}
					if !ok {
						return &exitError{exitFailure, "cancelled"}
					}
				}
				if _, err := call[workspaces.Record](b, "workspaces.save", map[string]any{"yaml": string(data), "root": root, "replace": replace}); err != nil {
					return err
				}
				fmt.Printf("Registered. Start it with: runtide workspace up %s\n", pv.Name)
				return nil
			})
		}}
	imp.Flags().StringVar(&root, "root", "", "project root directory (default: the YAML file's folder)")
	imp.Flags().BoolVar(&replace, "replace", false, "overwrite an existing workspace of the same name")
	imp.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	var outFile string
	exp := &cobra.Command{Use: "export <name>", Short: "Print (or save) a workspace's YAML", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				y, err := call[string](b, "workspaces.export", map[string]string{"name": args[0]})
				if err != nil {
					return err
				}
				if outFile == "" {
					fmt.Print(y)
					return nil
				}
				if _, err := os.Stat(outFile); err == nil {
					return fmt.Errorf("%s already exists", outFile)
				}
				return os.WriteFile(outFile, []byte(y), 0o644)
			})
		}}
	exp.Flags().StringVarP(&outFile, "output", "o", "", "write to this file instead of stdout")

	del := &cobra.Command{Use: "delete <name>", Short: "Delete a workspace definition (running services must be stopped)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				ok, err := confirm("Delete workspace " + args[0] + "? Its project files are not touched.")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			c, err := connectRunning(true)
			if err != nil {
				return err
			}
			return c.Call("workspaces.delete", map[string]string{"name": args[0]}, nil)
		}}
	del.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	hist := &cobra.Command{Use: "history [name]", Short: "Show start/stop history", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := ""
			if len(args) == 1 {
				n = args[0]
			}
			return withBackend(func(b backend) error {
				hs, err := call[[]workspaces.History](b, "workspaces.history", map[string]any{"name": n, "limit": 30})
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(hs)
				}
				rows := [][]string{}
				for _, h := range hs {
					rows = append(rows, []string{h.Time.Format("2006-01-02 15:04:05"), h.Workspace, h.Action, h.Outcome, trunc(h.Detail, 60)})
				}
				table([]string{"TIME", "WORKSPACE", "ACTION", "OUTCOME", "DETAIL"}, rows)
				return nil
			})
		}}
	ws.AddCommand(up, down, restart, show, imp, exp, del, hist)
	return ws
}

func configCmd() *cobra.Command {
	cfg := &cobra.Command{Use: "config", Short: "Show settings, paths, and manage the RUNTIDE database", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				s, err := call[app.Settings](b, "settings.get", nil)
				if err != nil {
					return err
				}
				info, _ := call[map[string]any](b, "db.info", nil)
				if jsonOut {
					return printJSON(map[string]any{"settings": s, "database": info})
				}
				m := map[string]any{}
				bs, _ := json.Marshal(s)
				_ = json.Unmarshal(bs, &m)
				for _, k := range sortedKeys(m) {
					fmt.Printf("%-20s %v\n", k, m[k])
				}
				fmt.Printf("\ndata directory: %v\ndatabase:       %v (schema v%v)\n", filepath.Dir(fmt.Sprint(info["path"])), info["path"], info["schemaVersion"])
				return nil
			})
		}}
	set := &cobra.Command{Use: "set <key> <value>", Short: "Change a setting (see `runtide config` for keys)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				cur, err := call[map[string]any](b, "settings.get", nil)
				if err != nil {
					return err
				}
				old, ok := cur[args[0]]
				if !ok {
					return fmt.Errorf("unknown setting %q", args[0])
				}
				var v any = args[1]
				switch reflect.TypeOf(old).Kind() {
				case reflect.Bool:
					bv, err := strconv.ParseBool(args[1])
					if err != nil {
						return fmt.Errorf("%s expects true or false", args[0])
					}
					v = bv
				case reflect.Float64:
					nv, err := strconv.Atoi(args[1])
					if err != nil {
						return fmt.Errorf("%s expects a number", args[0])
					}
					v = nv
				case reflect.Slice:
					v = strings.Split(args[1], ",")
				}
				_, err = call[app.Settings](b, "settings.set", map[string]any{args[0]: v})
				if err == nil {
					fmt.Printf("%s = %v\n", args[0], v)
				}
				return err
			})
		}}
	pathC := &cobra.Command{Use: "path", Short: "Print the data directory", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		h, err := paths.Home()
		fmt.Println(h)
		return err
	}}
	backup := &cobra.Command{Use: "backup", Short: "Back up the database to the backups folder", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return withBackend(func(b backend) error {
				r, err := call[map[string]string](b, "db.backup", nil)
				if err == nil {
					fmt.Println("Backup written to", r["path"])
				}
				return err
			})
		}}
	var yes bool
	restore := &cobra.Command{Use: "restore <backup-file>", Short: "Restore the database from a backup (the engine must not be running)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := paths.Home()
			if err != nil {
				return err
			}
			if _, err := api.Connect(home); err == nil {
				return fmt.Errorf("a RUNTIDE engine is running; stop it first (`runtide daemon stop` or quit the desktop app)")
			}
			if !yes {
				ok, err := confirm("Replace the current database with " + args[0] + "? The current one is kept as a .pre-restore copy.")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			abs, _ := filepath.Abs(args[0])
			keep, err := app.RestoreDatabase(home, abs)
			if err != nil {
				return err
			}
			fmt.Println("Restored. Previous database kept at:", dash(keep))
			return nil
		}}
	restore.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	reset := &cobra.Command{Use: "reset", Short: "Delete all workspaces, services, history and settings (a backup is made first)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				ok, err := confirm("This deletes ALL RUNTIDE data (workspaces, services, logs, settings). A backup is created first. Continue?")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			return withBackend(func(b backend) error {
				r, err := call[map[string]string](b, "db.reset", map[string]bool{"confirm": true})
				if err == nil {
					fmt.Println("Reset complete. Safety backup:", r["backup"])
				}
				return err
			})
		}}
	reset.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cfg.AddCommand(set, pathC, backup, restore, reset)
	return cfg
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}

func daemonCmd() *cobra.Command {
	d := &cobra.Command{Use: "daemon", Short: "Control the background engine that keeps services running after the CLI exits"}
	run := &cobra.Command{Use: "run", Short: "Run the engine in the foreground (used internally by auto-start)", Args: cobra.NoArgs, Hidden: false,
		RunE: func(*cobra.Command, []string) error {
			e, err := app.New(app.Options{})
			if err != nil {
				return err
			}
			srv, err := api.Serve(e, "daemon")
			if err != nil {
				_ = e.Close(false)
				return err
			}
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			select {
			case <-sig:
			case <-e.ShutdownRequested:
			}
			srv.Close()
			return e.Close(true) // gracefully stops every managed service
		}}
	start := &cobra.Command{Use: "start", Short: "Start the background engine", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		c, err := connectRunning(false)
		if err != nil {
			return err
		}
		fmt.Printf("Engine running (pid %d, %s).\n", c.Info.PID, c.Info.Kind)
		return nil
	}}
	stop := &cobra.Command{Use: "stop", Short: "Stop the background engine and every service it manages", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		home, _ := paths.Home()
		c, err := api.Connect(home)
		if err != nil {
			fmt.Println("No engine is running.")
			return nil
		}
		if c.Info.Kind != "daemon" {
			return fmt.Errorf("the running engine belongs to the desktop app; quit it from the app")
		}
		_ = c.Call("system.shutdown", nil, nil)
		for i := 0; i < 100; i++ {
			if _, err := api.Connect(home); err != nil {
				fmt.Println("Engine stopped.")
				return nil
			}
			time.Sleep(300 * time.Millisecond)
		}
		return fmt.Errorf("engine did not stop within 30 seconds")
	}}
	status := &cobra.Command{Use: "status", Short: "Show whether an engine is running", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		home, _ := paths.Home()
		c, err := api.Connect(home)
		if err != nil {
			if jsonOut {
				return printJSON(map[string]bool{"running": false})
			}
			fmt.Println("No engine is running.")
			return &exitError{exitNoEngine, ""}
		}
		if jsonOut {
			return printJSON(map[string]any{"running": true, "pid": c.Info.PID, "kind": c.Info.Kind, "version": c.Info.Version, "started": c.Info.Started})
		}
		fmt.Printf("Engine running: pid %d, %s, version %s, since %s\n", c.Info.PID, c.Info.Kind, c.Info.Version, c.Info.Started.Format(time.RFC3339))
		return nil
	}}
	d.AddCommand(run, start, stop, status)
	return d
}

func dockerCmd() *cobra.Command {
	d := &cobra.Command{Use: "docker", Short: "Show Docker status and containers (Docker is optional)", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return withBackend(func(b backend) error {
				st, err := call[docker.Status](b, "docker.status", nil)
				if err != nil {
					return err
				}
				var cs []docker.Container
				if st.DaemonAvailable {
					cs, err = call[[]docker.Container](b, "docker.list", nil)
					if err != nil {
						return err
					}
				}
				if jsonOut {
					return printJSON(map[string]any{"status": st, "containers": cs})
				}
				fmt.Println(st.Message)
				rows := [][]string{}
				for _, c := range cs {
					rows = append(rows, []string{c.Name, c.ID, c.State, trunc(strings.Join(c.Ports, ", "), 40), dash(c.CPUPercent), dash(c.MemUsage), dash(c.ComposeProject)})
				}
				if len(rows) > 0 {
					table([]string{"NAME", "ID", "STATE", "PORTS", "CPU", "MEMORY", "COMPOSE"}, rows)
				}
				return nil
			})
		}}
	for _, verb := range []string{"start", "stop"} {
		verb := verb
		d.AddCommand(&cobra.Command{Use: verb + " <container>", Short: strings.ToUpper(verb[:1]) + verb[1:] + " a container", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return withBackend(func(b backend) error {
					if err := b.Call("docker."+verb, map[string]string{"id": args[0]}, nil); err != nil {
						return err
					}
					fmt.Printf("%s: %s done.\n", args[0], verb)
					return nil
				})
			}})
	}
	return d
}

func wslCmd() *cobra.Command {
	return &cobra.Command{Use: "wsl", Short: "Show WSL availability and distributions (read-only)", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return withBackend(func(b backend) error {
				st, err := call[wsl.Status](b, "wsl.status", nil)
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(st)
				}
				fmt.Println(st.Message)
				rows := [][]string{}
				for _, d := range st.Distros {
					def := ""
					if d.Default {
						def = "default"
					}
					rows = append(rows, []string{d.Name, d.State, strconv.Itoa(d.Version), def})
				}
				if len(rows) > 0 {
					table([]string{"DISTRIBUTION", "STATE", "WSL", ""}, rows)
				}
				return nil
			})
		}}
}
