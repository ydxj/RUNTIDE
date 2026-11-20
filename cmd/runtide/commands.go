package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/app"
	"github.com/runtide/runtide/internal/diagnostics"
	"github.com/runtide/runtide/internal/envfile"
	"github.com/runtide/runtide/internal/ports"
	"github.com/runtide/runtide/internal/process"
	"github.com/runtide/runtide/internal/tools"
	"github.com/runtide/runtide/internal/workspaces"
	"github.com/spf13/cobra"
)

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "runtide",
		Short: "RUNTIDE: Everything running. Under control.",
		Long: `RUNTIDE is a local-first control center for everything running on your
Windows development machine: processes, ports, services, workspaces, logs,
health, environment and diagnostics.

Run without arguments for a status summary.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, args []string) error { return runStatus() },
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return &exitError{exitUsage, err.Error()} })
	root.AddCommand(
		versionCmd(), statusCmd(), processesCmd(), killCmd(), portsCmd(), servicesCmd(),
		startStopCmd("start", "Start a service"), startStopCmd("stop", "Stop a service"), startStopCmd("restart", "Restart a service"),
		logsCmd(), stacksCmd(), stackCmd(), projectCmd(), workspaceCmd(), runCmd(), receiptCmd(), serverCmd(), doctorCmd(), envCmd(), toolsCmd(), configCmd(), daemonCmd(),
		dockerCmd(), wslCmd(),
	)
	return root
}

func call[T any](b backend, method string, params any) (T, error) {
	var v T
	err := b.Call(method, params, &v)
	return v, err
}

func withBackend(f func(b backend) error) error {
	b, err := connect()
	if err != nil {
		return err
	}
	defer b.Close()
	return f(b)
}

func versionCmd() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version information", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if jsonOut {
			return printJSON(map[string]string{"version": app.Version})
		}
		fmt.Println("runtide", app.Version)
		return nil
	}}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Summarize machine, services and workspaces", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return runStatus() }}
}

func runStatus() error {
	return withBackend(func(b backend) error {
		ov, err := call[app.Overview](b, "overview", nil)
		if err != nil {
			return err
		}
		if jsonOut {
			return printJSON(ov)
		}
		fmt.Printf("Processes: %d   Listening ports: %d\n", ov.Processes, ov.Listening)
		if s := ov.Resources.Sample; s != nil {
			fmt.Printf("CPU: %.0f%%   Memory: %s / %s (%.0f%%)\n", s.CPUPercent, bytesStr(s.MemUsed), bytesStr(s.MemTotal), s.MemPercent)
		}
		for _, d := range ov.Resources.Disks {
			fmt.Printf("Disk %s  %s free of %s\n", d.Path, bytesStr(d.Free), bytesStr(d.Total))
		}
		fmt.Printf("Services: %d defined, %d running, %d failed, %d unhealthy\n", ov.Services.Total, ov.Services.Running, ov.Services.Failed, ov.Services.Unhealthy)
		for _, w := range ov.Workspaces {
			fmt.Printf("Workspace %-20s %-9s %d/%d running\n", w.Name, w.Health, w.Running, w.Total)
		}
		for _, w := range ov.Warnings {
			fmt.Println(color("33", "warning: ") + w)
		}
		return nil
	})
}

func processesCmd() *cobra.Command {
	var query string
	var tree, cmdline bool
	c := &cobra.Command{Use: "processes [filter]", Aliases: []string{"ps"}, Short: "List running processes", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				query = args[0]
			}
			return withBackend(func(b backend) error {
				if tree {
					nodes, err := call[[]*process.Node](b, "processes.tree", nil)
					if err != nil {
						return err
					}
					if jsonOut {
						return printJSON(nodes)
					}
					var walk func(ns []*process.Node, depth int)
					walk = func(ns []*process.Node, depth int) {
						for _, n := range ns {
							fmt.Printf("%s%s [%d]\n", strings.Repeat("  ", depth), n.Name, n.PID)
							walk(n.Children, depth+1)
						}
					}
					walk(nodes, 0)
					return nil
				}
				ps, err := call[[]process.Process](b, "processes.list", map[string]any{"query": query, "cmdLine": cmdline, "wait": true})
				if err != nil {
					return err
				}
				sort.Slice(ps, func(i, j int) bool { return ps[i].MemoryBytes > ps[j].MemoryBytes })
				if jsonOut {
					return printJSON(ps)
				}
				rows := [][]string{}
				for _, p := range ps {
					row := []string{strconv.Itoa(int(p.PID)), strconv.Itoa(int(p.PPID)), trunc(p.Name, 28), fmt.Sprintf("%.1f", p.CPUPercent), bytesStr(p.MemoryBytes), dash(p.Protection)}
					if cmdline {
						row = append(row, trunc(p.CmdLine, 80))
					}
					rows = append(rows, row)
				}
				h := []string{"PID", "PPID", "NAME", "CPU%", "MEMORY", "PROTECTION"}
				if cmdline {
					h = append(h, "COMMAND LINE")
				}
				table(h, rows)
				fmt.Printf("\n%d processes\n", len(ps))
				return nil
			})
		}}
	c.Flags().BoolVar(&tree, "tree", false, "show the parent/child tree")
	c.Flags().BoolVar(&cmdline, "cmdline", false, "include command lines where permitted")
	return c
}

func killCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{Use: "kill <pid>", Short: "Terminate a process (asks for confirmation; refuses protected processes)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return &exitError{exitUsage, "pid must be a number"}
			}
			return withBackend(func(b backend) error {
				ps, err := call[[]process.Process](b, "processes.list", nil)
				if err != nil {
					return err
				}
				var target *process.Process
				for i := range ps {
					if ps[i].PID == uint32(pid) {
						target = &ps[i]
					}
				}
				if target == nil {
					return fmt.Errorf("no process with pid %d", pid)
				}
				if target.Protection == process.ProtProtected {
					return fmt.Errorf("refusing: %s (%d) is protected: %s", target.Name, pid, target.ProtectionNote)
				}
				if !yes {
					msg := fmt.Sprintf("Terminate %s (pid %d)? Unsaved work in it will be lost.", target.Name, pid)
					if target.ProtectionNote != "" {
						msg += " WARNING: " + target.ProtectionNote
					}
					ok, err := confirm(msg)
					if err != nil {
						return err
					}
					if !ok {
						return &exitError{exitFailure, "cancelled"}
					}
				}
				res, err := call[process.TerminateResult](b, "processes.kill", map[string]any{"pid": pid, "startTime": target.StartTime})
				if jsonOut {
					return printJSON(res)
				}
				if err != nil {
					return fmt.Errorf("%s", res.Detail+" ("+err.Error()+")")
				}
				fmt.Printf("Terminated %s (pid %d).\n", res.Name, res.PID)
				return nil
			})
		}}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return c
}

func portsCmd() *cobra.Command {
	var udp bool
	c := &cobra.Command{Use: "ports [filter]", Short: "List listening TCP ports and their owning processes", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = args[0]
			}
			return withBackend(func(b backend) error {
				ls, err := call[[]ports.Listener](b, "ports.list", map[string]any{"query": q, "udp": udp})
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(ls)
				}
				rows := [][]string{}
				for _, l := range ls {
					note := l.Conflict
					if l.Managed != "" {
						note = "managed by " + l.Managed
					}
					rows = append(rows, []string{l.Protocol, strconv.Itoa(int(l.Port)), l.Address, l.Family, strconv.Itoa(int(l.PID)), l.ProcessName, l.Exposure, dash(note)})
				}
				table([]string{"PROTO", "PORT", "ADDRESS", "FAMILY", "PID", "PROCESS", "EXPOSURE", "NOTES"}, rows)
				return nil
			})
		}}
	c.Flags().BoolVar(&udp, "udp", false, "include UDP bindings")
	return c
}

func servicesCmd() *cobra.Command {
	list := func(cmd *cobra.Command, args []string) error {
		return withBackend(func(b backend) error {
			vs, err := call[[]app.ServiceView](b, "services.list", nil)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(vs)
			}
			rows := [][]string{}
			for _, v := range vs {
				state, pid, up, hl := "stopped", "-", "-", "-"
				if v.Runtime != nil {
					state, hl = string(v.Runtime.State), string(v.Runtime.Health)
					if v.Runtime.PID > 0 {
						pid, up = strconv.Itoa(v.Runtime.PID), durStr(v.Runtime.UptimeSec)
					}
				}
				rows = append(rows, []string{v.Key, v.Spec.Type, state, hl, pid, up})
			}
			table([]string{"SERVICE", "TYPE", "STATE", "HEALTH", "PID", "UPTIME"}, rows)
			if len(rows) == 0 {
				fmt.Println("No services defined. Create a workspace file and run `runtide stack import`.")
			}
			return nil
		})
	}
	c := &cobra.Command{Use: "services", Short: "List managed services and their state", Args: cobra.NoArgs, RunE: list}
	return c
}

func startStopCmd(verb, short string) *cobra.Command {
	return &cobra.Command{Use: verb + " <service>", Short: short, Long: short + ". Use `workspace/service` when a name is ambiguous.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(jsonOut)
			if err != nil {
				return err
			}
			if err := c.Call("services."+verb, map[string]string{"key": args[0]}, nil); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(map[string]string{"service": args[0], "action": verb, "result": "ok"})
			}
			past := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[verb]
			fmt.Printf("%s %s.\n", args[0], past)
			return nil
		}}
}

func logsCmd() *cobra.Command {
	var follow bool
	var n int
	var level, search string
	var persisted bool
	c := &cobra.Command{Use: "logs [service...]", Short: "Show captured service logs (stdout/stderr of RUNTIDE-launched processes)", Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(true)
			if err != nil {
				return err
			}
			keys := []string{}
			for _, a := range args {
				var v app.ServiceView
				vs, err := call[[]app.ServiceView](&remoteBackend{c}, "services.list", nil)
				if err != nil {
					return err
				}
				found := false
				for _, x := range vs {
					if x.Key == a || x.Name == a {
						v, found = x, true
						break
					}
				}
				if !found {
					return fmt.Errorf("unknown service %q", a)
				}
				keys = append(keys, v.Key)
			}
			type entry struct {
				Service, Stream, Level, Line string
				Time                         time.Time
			}
			print := func(e entry) {
				if level != "" && e.Level != level {
					return
				}
				if search != "" && !strings.Contains(strings.ToLower(e.Line), strings.ToLower(search)) {
					return
				}
				if jsonOut {
					_ = printJSON(e)
					return
				}
				line := e.Line
				switch e.Level {
				case "error":
					line = color("31", line)
				case "warn":
					line = color("33", line)
				}
				fmt.Printf("%s %-14s %s\n", e.Time.Format("15:04:05.000"), trunc(e.Service, 14), line)
			}
			method := "logs.recent"
			params := map[string]any{"services": keys, "n": n}
			if persisted {
				method = "logs.query"
				params = map[string]any{"services": keys, "limit": n, "search": search, "level": level}
			}
			var es []entry
			if err := c.Call(method, params, &es); err != nil {
				return err
			}
			for _, e := range es {
				print(e)
			}
			if !follow {
				return nil
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			want := map[string]bool{}
			for _, k := range keys {
				want[k] = true
			}
			return c.Events(ctx, func(topic string, data json.RawMessage) {
				if topic != "log" {
					return
				}
				var e entry
				if json.Unmarshal(data, &e) == nil && (len(want) == 0 || want[e.Service]) {
					print(e)
				}
			})
		}}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new output")
	c.Flags().IntVarP(&n, "lines", "n", 200, "number of lines to show")
	c.Flags().StringVar(&level, "level", "", "only show this level (error, warn, info, debug)")
	c.Flags().StringVar(&search, "search", "", "only lines containing this text")
	c.Flags().BoolVar(&persisted, "history", false, "read persisted history from earlier sessions")
	return c
}

func doctorCmd() *cobra.Command {
	var fix string
	var yes bool
	c := &cobra.Command{Use: "doctor", Short: "Diagnose common problems (read-only unless --fix is used)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				if fix != "" {
					plan, err := call[diagnostics.FixPlan](b, "doctor.plan", map[string]string{"fixId": fix})
					if err != nil {
						return err
					}
					fmt.Println(plan.Description)
					for _, ch := range plan.Changes {
						fmt.Println("  -", ch)
					}
					if !yes {
						ok, err := confirm("Apply this fix?")
						if err != nil {
							return err
						}
						if !ok {
							return &exitError{exitFailure, "cancelled"}
						}
					}
					if _, err := call[diagnostics.FixPlan](b, "doctor.apply", map[string]any{"fixId": fix, "confirm": true}); err != nil {
						return err
					}
					fmt.Println("Fix applied.")
					return nil
				}
				rep, err := call[diagnostics.Report](b, "doctor.run", nil)
				if err != nil {
					return err
				}
				if jsonOut {
					if err := printJSON(rep); err != nil {
						return err
					}
				} else {
					for _, f := range rep.Findings {
						fmt.Printf("[%s] %s  (%s)\n", sev(f.Severity), f.Title, f.Component)
						fmt.Println("    " + f.Description)
						for _, e := range f.Evidence {
							if e != "" {
								fmt.Println("    evidence:", trunc(e, 160))
							}
						}
						fmt.Println("    fix:", f.Resolution)
						if f.FixAvailable {
							fmt.Printf("    automatic fix available: runtide doctor --fix %q\n", f.FixID)
						}
						fmt.Println()
					}
					fmt.Printf("%d error(s), %d warning(s), %d note(s)\n", rep.Errors, rep.Warnings, rep.Infos)
				}
				if rep.Errors > 0 {
					return &exitError{exitFindings, ""}
				}
				return nil
			})
		}}
	c.Flags().StringVar(&fix, "fix", "", "apply the automatic fix with this id (shown in the report); previews first")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return c
}

func envCmd() *cobra.Command {
	env := &cobra.Command{Use: "env", Short: "Inspect environment files (.env) safely; secrets are redacted"}
	inspect := &cobra.Command{Use: "inspect [dir]", Short: "Analyze .env files: duplicates, invalid lines, missing variables, exposed secrets", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			if len(args) == 1 {
				dir, _ = filepath.Abs(args[0])
			}
			rep, err := envfile.Inspect(dir)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(rep)
			}
			if len(rep.Files) == 0 {
				fmt.Println("No .env files found in", dir)
				return nil
			}
			for _, f := range rep.Files {
				fmt.Printf("%s  (%d variables)\n", f.Path, len(f.Entries))
				for _, e := range f.Entries {
					fmt.Printf("  %-32s %s\n", e.Key, trunc(e.Display, 60))
				}
				for _, d := range f.Duplicates {
					fmt.Println(color("33", "  duplicate: ") + d)
				}
				for _, i := range f.Invalid {
					fmt.Printf("  %s line %d: %s\n", color("31", "invalid:"), i.Line, i.Reason)
				}
				for _, s := range f.Secrets {
					fmt.Printf("  %s %s (line %d): %s\n", color("33", "secret:"), s.Key, s.Line, s.Reason)
				}
			}
			for _, m := range rep.Missing {
				fmt.Printf("%s %s lacks: %s\n", color("33", "missing:"), filepath.Base(m.File), strings.Join(m.Keys, ", "))
			}
			for _, h := range rep.Hints {
				fmt.Println("hint:", h)
			}
			return nil
		}}
	compare := &cobra.Command{Use: "compare <fileA> <fileB>", Short: "Show variables that differ between two env files (values redacted)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _ := filepath.Abs(args[0])
			b, _ := filepath.Abs(args[1])
			return withBackend(func(be backend) error {
				d, err := call[envfile.Diff](be, "env.compare", map[string]string{"a": a, "b": b})
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(d)
				}
				fmt.Println("only in A:", strings.Join(d.OnlyInA, ", "))
				fmt.Println("only in B:", strings.Join(d.OnlyInB, ", "))
				for _, c := range d.Changed {
					fmt.Printf("differs: %-30s %s  ->  %s\n", c.Key, c.A, c.B)
				}
				return nil
			})
		}}
	var write, keep, yes bool
	example := &cobra.Command{Use: "example <env-file>", Short: "Generate .env.example from an env file (preview by default)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, _ := filepath.Abs(args[0])
			return withBackend(func(be backend) error {
				type res struct {
					Content, Destination, Backup string
					Written                      bool
				}
				r, err := call[res](be, "env.generateExample", map[string]any{"source": src, "keepSafe": keep, "write": false})
				if err != nil {
					return err
				}
				if !write {
					if jsonOut {
						return printJSON(r)
					}
					fmt.Print(r.Content)
					fmt.Fprintf(os.Stderr, "\n(preview only; use --write to save to %s. An existing file is backed up first.)\n", r.Destination)
					return nil
				}
				if !yes {
					ok, err := confirm("Write " + r.Destination + "?")
					if err != nil {
						return err
					}
					if !ok {
						return &exitError{exitFailure, "cancelled"}
					}
				}
				r, err = call[res](be, "env.generateExample", map[string]any{"source": src, "keepSafe": keep, "write": true})
				if err != nil {
					return err
				}
				fmt.Println("Wrote", r.Destination)
				if r.Backup != "" {
					fmt.Println("Backup:", r.Backup)
				}
				return nil
			})
		}}
	example.Flags().BoolVar(&write, "write", false, "save the file (default is preview)")
	example.Flags().BoolVar(&keep, "keep-safe-values", false, "keep non-sensitive values instead of blanking all")
	example.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	env.AddCommand(inspect, compare, example)
	return env
}

func toolsCmd() *cobra.Command {
	return &cobra.Command{Use: "tools", Short: "Detect installed developer tools and versions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBackend(func(b backend) error {
				ts, err := call[[]tools.Tool](b, "tools.list", nil)
				if err != nil {
					return err
				}
				if jsonOut {
					return printJSON(ts)
				}
				rows := [][]string{}
				for _, t := range ts {
					st := "not installed"
					if t.Installed {
						st = "installed"
					}
					op := "-"
					if t.Installed {
						op = map[bool]string{true: "yes", false: "no"}[t.OnPath]
					}
					extra := ""
					if len(t.Others) > 0 {
						extra = fmt.Sprintf(" (+%d more)", len(t.Others))
					}
					rows = append(rows, []string{t.Name, st, dash(t.Version) + extra, op, dash(t.Method), dash(t.Path)})
				}
				table([]string{"TOOL", "STATUS", "VERSION", "ON PATH", "DETECTED VIA", "LOCATION"}, rows)
				return nil
			})
		}}
}

var _ = workspaces.ErrNotFound
