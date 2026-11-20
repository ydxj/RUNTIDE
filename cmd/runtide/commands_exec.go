package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
	"github.com/runtide/runtide/internal/execution"
	"github.com/runtide/runtide/internal/receipts"
	"github.com/runtide/runtide/internal/store"
	"github.com/runtide/runtide/internal/verification"
	"github.com/spf13/cobra"
)

// callRunning executes a method on a long-lived engine (started on demand):
// anything that starts or controls an execution must live there so it outlives the CLI.
func callRunning(method string, params, out any) error {
	c, err := connectRunning(jsonOut)
	if err != nil {
		return err
	}
	return c.Call(method, params, out)
}

func callAny(method string, params, out any) error {
	return withBackend(func(b backend) error { return b.Call(method, params, out) })
}

func ts(t time.Time) string { return t.Local().Format("2006-01-02 15:04:05") }

func projectCmd() *cobra.Command {
	p := &cobra.Command{Use: "project", Short: "Manage projects (directories RUNTIDE executes tasks for)"}
	var root, desc string
	create := &cobra.Command{Use: "create <name>", Short: "Register a project directory", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if root == "" {
				root, _ = os.Getwd()
			}
			root, _ = filepath.Abs(root)
			var pr domain.Project
			if err := callAny("projects.create", map[string]string{"name": args[0], "root": root, "description": desc}, &pr); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(pr)
			}
			fmt.Printf("Project %s created (%s)\n  root: %s\n", pr.Name, pr.ID, pr.Root)
			if pr.GitRoot != "" {
				fmt.Println("  git repository detected")
			}
			return nil
		}}
	create.Flags().StringVar(&root, "root", "", "project directory (default: current directory)")
	create.Flags().StringVar(&desc, "description", "", "description")
	list := &cobra.Command{Use: "list", Short: "List projects", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		var ps []domain.Project
		if err := callAny("projects.list", nil, &ps); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(ps)
		}
		rows := [][]string{}
		for _, x := range ps {
			git := "no"
			if x.GitRoot != "" {
				git = "yes"
			}
			rows = append(rows, []string{x.Name, x.ID, git, x.Root})
		}
		table([]string{"PROJECT", "ID", "GIT", "ROOT"}, rows)
		if len(rows) == 0 {
			fmt.Println("No projects yet: runtide project create <name> --root <dir>")
		}
		return nil
	}}
	p.AddCommand(create, list)
	return p
}

func workspaceCmd() *cobra.Command {
	w := &cobra.Command{Use: "workspace", Short: "Manage task workspaces (the environment of one task)"}
	var project, task, isolation, cfgFile, dir string
	var yes, allowDirty bool
	create := &cobra.Command{Use: "create <name>", Short: "Create a task workspace (shows exactly what will be created first)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			yaml := ""
			if cfgFile != "" {
				b, err := os.ReadFile(cfgFile)
				if err != nil {
					return err
				}
				yaml = string(b)
			}
			req := map[string]any{"project": project, "name": args[0], "task": task, "isolation": isolation, "configYaml": yaml, "dir": dir, "acceptDirty": allowDirty}
			var plan execution.WorkspacePlan
			if err := callAny("ws.plan", req, &plan); err != nil {
				return err
			}
			fmt.Printf("Workspace %s/%s, isolation %s\n  directory: %s\n  will create:\n", plan.Project, plan.Name, plan.Isolation, plan.Dir)
			for _, c := range plan.Creates {
				fmt.Println("   -", c)
			}
			for _, x := range plan.Warnings {
				fmt.Println(color("33", "  warning: ") + x)
			}
			for _, x := range plan.Blockers {
				fmt.Println(color("31", "  blocked: ") + x)
			}
			if len(plan.Blockers) > 0 {
				return &exitError{exitFailure, "cannot create the workspace"}
			}
			if !yes {
				ok, err := confirm("Create this workspace?")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			req["confirm"] = true
			var ws domain.Workspace
			if err := callAny("ws.create", req, &ws); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(ws)
			}
			fmt.Printf("Workspace created: %s (%s), state %s\nNext: runtide run create --workspace %s \"<task>\"\n", ws.Name, ws.ID, ws.State, ws.ID)
			return nil
		}}
	create.Flags().StringVar(&project, "project", "", "project name or id (required)")
	create.Flags().StringVar(&task, "task", "", "task description")
	create.Flags().StringVar(&isolation, "isolation", "", "managed | worktree | docker | wsl (default: from config, else managed)")
	create.Flags().StringVar(&cfgFile, "config", "", "workspace configuration YAML file")
	create.Flags().StringVar(&dir, "dir", "", "directory to run in (managed/docker/wsl); default: the project root")
	create.Flags().BoolVar(&allowDirty, "allow-dirty", false, "allow a worktree from the last commit while the source has uncommitted changes")
	create.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	_ = create.MarkFlagRequired("project")

	list := &cobra.Command{Use: "list", Short: "List workspaces", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		var ws []domain.Workspace
		if err := callAny("ws.list", map[string]string{"project": project}, &ws); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(ws)
		}
		rows := [][]string{}
		for _, x := range ws {
			ap := "no"
			if x.Approved {
				ap = "yes"
			}
			rows = append(rows, []string{x.Name, x.ID, string(x.State), domain.Isolation(x.Isolation).Name(), ap, trunc(x.Task, 40)})
		}
		table([]string{"WORKSPACE", "ID", "STATE", "ISOLATION", "APPROVED", "TASK"}, rows)
		return nil
	}}
	list.Flags().StringVar(&project, "project", "", "only this project")

	inspect := &cobra.Command{Use: "inspect <id|project/name>", Short: "Show a workspace and the commands it would run", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct {
				Workspace domain.Workspace `json:"workspace"`
				Commands  []string         `json:"commands"`
			}
			if err := callAny("ws.get", map[string]string{"ref": args[0]}, &out); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(out)
			}
			x := out.Workspace
			fmt.Printf("%s (%s)\n  project:   %s\n  state:     %s\n  isolation: %s\n  directory: %s\n  approved:  %v\n  task:      %s\n", x.Name, x.ID, x.ProjectID, x.State, x.Isolation.Name(), x.Dir, x.Approved, x.Task)
			if len(out.Commands) > 0 {
				fmt.Println("  commands that would run:")
				for _, c := range out.Commands {
					fmt.Println("   ", c)
				}
			}
			fmt.Println("\nConfiguration:\n" + x.ConfigYAML)
			return nil
		}}

	approve := &cobra.Command{Use: "approve <ref>", Short: "Approve the commands in a workspace's configuration (required before it runs)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return approveWorkspace(args[0], yes) }}
	approve.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	configure := &cobra.Command{Use: "configure <ref> <file.yaml>", Short: "Replace a workspace configuration (revokes approval)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			return callAny("ws.configure", map[string]string{"ref": args[0], "yaml": string(b)}, nil)
		}}
	reopen := &cobra.Command{Use: "reopen <ref>", Short: "Make a finished workspace READY for another execution", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callAny("ws.reopen", map[string]string{"ref": args[0]}, nil)
		}}
	var force bool
	removeDir := &cobra.Command{Use: "remove-dir <ref>", Short: "Delete the worktree directory a workspace owns (never the project)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				ok, err := confirm("Delete the workspace's own directory? Uncommitted work in it is lost" + map[bool]string{true: " (FORCED)", false: ""}[force] + ".")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			return callAny("ws.removeDir", map[string]any{"ref": args[0], "force": force, "confirm": true}, nil)
		}}
	removeDir.Flags().BoolVar(&force, "force", false, "discard uncommitted work")
	removeDir.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	del := &cobra.Command{Use: "delete <ref>", Short: "Delete a workspace record (refused if it has recorded executions)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callAny("ws.delete", map[string]any{"ref": args[0], "confirm": true}, nil)
		}}

	start := &cobra.Command{Use: "start <ref>", Short: "Create an execution in the workspace and start it", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var x domain.Execution
			if err := callRunning("runs.create", map[string]string{"workspace": args[0]}, &x); err != nil {
				return err
			}
			fmt.Println("Execution", x.ID)
			return startRun(x.ID, yes)
		}}
	start.Flags().BoolVarP(&yes, "yes", "y", false, "approve the workspace commands without asking")
	stop := &cobra.Command{Use: "stop <ref>", Short: "Cancel the workspace's active execution and stop its processes", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var xs []domain.Execution
			if err := callRunning("runs.list", map[string]any{"workspace": args[0], "states": []string{"CONFIGURING", "READY", "RUNNING", "VERIFYING"}}, &xs); err != nil {
				return err
			}
			if len(xs) == 0 {
				fmt.Println("No active execution in this workspace.")
				return nil
			}
			for _, x := range xs {
				if err := callRunning("runs.cancel", map[string]string{"id": x.ID, "reason": "workspace stopped by user"}, nil); err != nil {
					return err
				}
				fmt.Println("Cancelled", x.ID)
			}
			return nil
		}}
	w.AddCommand(create, list, inspect, approve, configure, reopen, removeDir, del, start, stop)
	return w
}

func approveWorkspace(ref string, yes bool) error {
	var out struct {
		Workspace domain.Workspace `json:"workspace"`
		Commands  []string         `json:"commands"`
	}
	if err := callAny("ws.get", map[string]string{"ref": ref}, &out); err != nil {
		return err
	}
	if out.Workspace.Approved {
		return nil
	}
	fmt.Printf("Workspace %q will execute these commands on this machine:\n", out.Workspace.Name)
	for _, c := range out.Commands {
		fmt.Println("   ", c)
	}
	fmt.Printf("  isolation: %s, directory: %s\n", out.Workspace.Isolation.Name(), out.Workspace.Dir)
	if !yes {
		ok, err := confirm("Approve and allow these commands to run?")
		if err != nil {
			return err
		}
		if !ok {
			return &exitError{exitFailure, "not approved; nothing was run"}
		}
	}
	return callAny("ws.approve", map[string]any{"ref": ref, "confirm": true}, nil)
}

func startRun(id string, yes bool) error {
	var d execution.Detail
	if err := callRunning("runs.get", map[string]string{"id": id}, &d); err != nil {
		return err
	}
	if d.Workspace != nil && !d.Workspace.Approved {
		var cfg struct {
			Commands []string `json:"commands"`
		}
		_ = callRunning("ws.get", map[string]string{"ref": d.Workspace.ID}, &cfg)
		if len(cfg.Commands) > 0 {
			if err := approveWorkspace(d.Workspace.ID, yes); err != nil {
				return err
			}
		}
	}
	fmt.Println("Starting...")
	if err := callRunning("runs.start", map[string]string{"id": id}, nil); err != nil {
		return err
	}
	fmt.Printf("Execution %s is RUNNING. Next: runtide run verify %s   (or: run exec / run finish / run cancel)\n", id, id)
	return nil
}

func runCmd() *cobra.Command {
	r := &cobra.Command{Use: "run", Short: "Create, run, verify and export executions"}
	var workspace string
	var yes bool
	create := &cobra.Command{Use: "create [task]", Short: "Create an execution (state CREATED) in a READY workspace", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := ""
			if len(args) == 1 {
				task = args[0]
			}
			var x domain.Execution
			if err := callRunning("runs.create", map[string]string{"workspace": workspace, "task": task}, &x); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(x)
			}
			fmt.Printf("Execution %s created (%s). Start it with: runtide run start %s\n", x.ID, x.State, x.ID)
			return nil
		}}
	create.Flags().StringVar(&workspace, "workspace", "", "workspace id or project/name (required)")
	_ = create.MarkFlagRequired("workspace")
	start := &cobra.Command{Use: "start <id>", Short: "Prepare, run setup/start commands and bring services up", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return startRun(args[0], yes) }}
	start.Flags().BoolVarP(&yes, "yes", "y", false, "approve the workspace commands without asking")

	var states []string
	var limit int
	list := &cobra.Command{Use: "list", Short: "List executions", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		var xs []domain.Execution
		if err := callAny("runs.list", map[string]any{"workspace": workspace, "states": states, "limit": limit}, &xs); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(xs)
		}
		rows := [][]string{}
		for _, x := range xs {
			rows = append(rows, []string{x.ID, string(x.State), dash(x.Verification), ts(x.CreatedAt), trunc(x.Task, 44)})
		}
		table([]string{"EXECUTION", "STATE", "VERIFICATION", "CREATED", "TASK"}, rows)
		return nil
	}}
	list.Flags().StringVar(&workspace, "workspace", "", "only this workspace")
	list.Flags().StringSliceVar(&states, "state", nil, "only these states (e.g. RUNNING)")
	list.Flags().IntVar(&limit, "limit", 30, "maximum rows")

	status := &cobra.Command{Use: "status <id>", Short: "Show an execution's state, commands, services, verification and file changes", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var d execution.Detail
			if err := callAny("runs.get", map[string]string{"id": args[0]}, &d); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(d)
			}
			x := d.Execution
			fmt.Printf("%s  %s\n  task: %s\n  workspace: %s   isolation: %s   duration: %s\n", x.ID, color(stateColor(x.State), string(x.State)), x.Task, d.Workspace.Name, x.Isolation.Name(), durStr(float64(d.DurationMs)/1000))
			if x.Error != "" {
				fmt.Println("  " + color("31", "error: ") + x.Error)
			}
			if x.Verification != "" {
				fmt.Println("  verification:", x.Verification)
			}
			if d.Active {
				fmt.Println("  (active in the running engine)")
			}
			rows := [][]string{}
			for _, c := range d.Commands {
				ec := "-"
				if c.ExitCode != nil {
					ec = fmt.Sprint(*c.ExitCode)
				}
				rows = append(rows, []string{fmt.Sprint(c.Seq), c.Kind, c.Name, ec, trunc(c.Executable+" "+strings.Join(c.Args, " "), 50)})
			}
			if len(rows) > 0 {
				fmt.Println("\nCommands")
				table([]string{"#", "KIND", "NAME", "EXIT", "COMMAND"}, rows)
			}
			if len(d.Services) > 0 {
				fmt.Println("\nServices")
				rows = nil
				for _, s := range d.Services {
					rows = append(rows, []string{s.Name, s.LastState, s.Health, fmt.Sprint(s.Restarts)})
				}
				table([]string{"SERVICE", "STATE", "HEALTH", "RESTARTS"}, rows)
			}
			if len(d.Verification) > 0 {
				fmt.Println("\nVerification:", d.Summary.Overall)
				rows = nil
				for _, v := range d.Verification {
					rows = append(rows, []string{v.Name, v.Status, fmt.Sprintf("%dms", v.DurationMs), trunc(v.Error, 60)})
				}
				table([]string{"CHECK", "STATUS", "TIME", "DETAIL"}, rows)
			}
			if len(d.FileChanges) > 0 {
				counts := map[string]int{}
				for _, c := range d.FileChanges {
					counts[c.Change]++
				}
				fmt.Printf("\nFile changes: %v (runtide run files %s)\n", counts, x.ID)
			}
			return nil
		}}

	var cmdName string
	var follow bool
	logs := &cobra.Command{Use: "logs <id>", Short: "Show recorded command output (redacted)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectRunning(true)
			if err != nil {
				return err
			}
			var d execution.Detail
			if err := c.Call("runs.get", map[string]string{"id": args[0]}, &d); err != nil {
				return err
			}
			for _, cm := range d.Commands {
				if cmdName != "" && cm.Name != cmdName {
					continue
				}
				var ls []domain.OutputLine
				if err := c.Call("runs.output", map[string]any{"commandId": cm.ID, "limit": 100000}, &ls); err != nil {
					return err
				}
				for _, l := range ls {
					printOutputLine(jsonOut, cm.Name, l.Stream, l.Line, l.Time)
				}
				if cm.Truncated {
					fmt.Fprintf(os.Stderr, "(output of %s was truncated at the storage limit)\n", cm.Name)
				}
			}
			if !follow {
				return nil
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			return c.Events(ctx, func(topic string, data json.RawMessage) {
				if topic != "exec.output" {
					return
				}
				var o struct {
					ExecutionID, Name, Stream, Line string
					Time                            time.Time
				}
				if json.Unmarshal(data, &o) == nil && o.ExecutionID == d.Execution.ID && (cmdName == "" || o.Name == cmdName) {
					printOutputLine(jsonOut, o.Name, o.Stream, o.Line, o.Time)
				}
			})
		}}
	logs.Flags().StringVar(&cmdName, "command", "", "only this command")
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "stream new output")

	var types []string
	var search, severity string
	events := &cobra.Command{Use: "events <id>", Short: "Show the execution timeline", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var evs []domain.ExecEvent
			if err := callAny("runs.events", map[string]any{"id": args[0], "types": types, "search": search, "severity": severity, "limit": 5000}, &evs); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(evs)
			}
			for _, e := range evs {
				detail := ""
				if len(e.Data) > 0 {
					b, _ := json.Marshal(e.Data)
					detail = trunc(string(b), 110)
				}
				sev := e.Severity
				switch sev {
				case "error":
					sev = color("31", "error")
				case "warn":
					sev = color("33", "warn ")
				default:
					sev = "info "
				}
				fmt.Printf("%4d %s %s %-24s %-14s %s\n", e.Seq, e.Time.Local().Format("15:04:05.000"), sev, e.Type, trunc(e.Subject, 14), detail)
			}
			return nil
		}}
	events.Flags().StringSliceVar(&types, "type", nil, "event types (e.g. command.finished)")
	events.Flags().StringVar(&search, "search", "", "text to search for")
	events.Flags().StringVar(&severity, "severity", "", "info | warn | error")

	verify := &cobra.Command{Use: "verify <id>", Short: "Run the verification pipeline (exit code 3 when verification fails)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct {
				Results []domain.VerificationResult `json:"results"`
				Summary verification.Summary        `json:"summary"`
			}
			if err := callRunning("runs.verify", map[string]string{"id": args[0]}, &out); err != nil {
				return err
			}
			if jsonOut {
				_ = printJSON(out)
			} else {
				rows := [][]string{}
				for _, v := range out.Results {
					st := v.Status
					switch st {
					case "passed":
						st = color("32", st)
					case "failed", "timed_out":
						st = color("31", st)
					}
					rows = append(rows, []string{v.Name, st, fmt.Sprintf("%dms", v.DurationMs), trunc(v.Error, 70)})
				}
				table([]string{"CHECK", "STATUS", "TIME", "DETAIL"}, rows)
				fmt.Println("\nOverall:", out.Summary.Overall)
				for _, rs := range out.Summary.Reasons {
					fmt.Println("  -", rs)
				}
			}
			if out.Summary.Overall != "passed" {
				return &exitError{exitFindings, ""}
			}
			return nil
		}}

	var execName string
	exe := &cobra.Command{Use: "exec <id> -- <command> [args...]", Short: "Run an ad-hoc recorded command in the execution (needs your approval and a policy that allows it)", Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, c := args[0], args[1:]
			if !yes {
				ok, err := confirm(fmt.Sprintf("Run %q inside execution %s?", strings.Join(c, " "), id))
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			var out domain.Command
			err := callRunning("runs.exec", map[string]any{"id": id, "name": execName, "command": c[0], "args": c[1:], "approved": true}, &out)
			if err != nil {
				return err
			}
			code := -1
			if out.ExitCode != nil {
				code = *out.ExitCode
			}
			fmt.Printf("command %s finished with exit code %d (see: runtide run logs %s --command %s)\n", out.Name, code, id, out.Name)
			if code != 0 {
				return &exitError{exitFailure, ""}
			}
			return nil
		}}
	exe.Flags().StringVar(&execName, "name", "", "name for the command in the timeline")
	exe.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	finish := &cobra.Command{Use: "finish <id>", Short: "Complete a RUNNING execution without verification", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callRunning("runs.finish", map[string]string{"id": args[0]}, nil)
		}}
	var reason string
	cancel := &cobra.Command{Use: "cancel <id>", Short: "Cancel an execution and stop everything it started", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return callRunning("runs.cancel", map[string]string{"id": args[0], "reason": reason}, nil)
		}}
	cancel.Flags().StringVar(&reason, "reason", "", "recorded reason")

	files := &cobra.Command{Use: "files <id>", Short: "List the file changes recorded for an execution", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var d execution.Detail
			if err := callAny("runs.get", map[string]string{"id": args[0]}, &d); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(d.FileChanges)
			}
			rows := [][]string{}
			for _, c := range d.FileChanges {
				p := c.Path
				if c.OldPath != "" {
					p = c.OldPath + " -> " + c.Path
				}
				rows = append(rows, []string{c.Change, p, fmt.Sprintf("%d -> %d", c.SizeBefore, c.SizeAfter)})
			}
			table([]string{"CHANGE", "PATH", "SIZE"}, rows)
			return nil
		}}
	diff := &cobra.Command{Use: "diff <id> <path>", Short: "Show a unified diff of a modified text file", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct{ Path, Diff string }
			if err := callAny("runs.diff", map[string]string{"id": args[0], "path": args[1]}, &out); err != nil {
				return err
			}
			fmt.Print(out.Diff)
			return nil
		}}

	var outPath string
	var diffs, sign bool
	var artifacts []string
	export := &cobra.Command{Use: "export <id>", Short: "Export a Run Receipt (.runtide.zip)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if outPath != "" {
				outPath, _ = filepath.Abs(outPath)
			}
			var out struct {
				Path     string
				Manifest receipts.Manifest
			}
			if err := callAny("receipts.export", map[string]any{"id": args[0], "path": outPath, "diffs": diffs, "artifacts": artifacts, "sign": sign}, &out); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(out)
			}
			fmt.Printf("Receipt written: %s\n  status %s, verification %s, reproducibility %s\n", out.Path, out.Manifest.Status, dash(out.Manifest.Verification), out.Manifest.Reproducibility)
			for _, w := range out.Manifest.Warnings {
				fmt.Println(color("33", "  warning: ") + w)
			}
			return nil
		}}
	export.Flags().StringVarP(&outPath, "output", "o", "", "receipt file (default: the RUNTIDE receipts folder)")
	export.Flags().BoolVar(&diffs, "include-diffs", false, "include text diffs of modified files (review for secrets before sharing)")
	export.Flags().StringSliceVar(&artifacts, "artifact", nil, "embed this workspace file (relative path; sensitive files are refused)")
	export.Flags().BoolVar(&sign, "sign", false, "sign with your receipt signing key")

	compare := &cobra.Command{Use: "compare <a> <b>", Short: "Compare two executions or receipts (ids or files)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error { return compareCmd(args[0], args[1]) }}

	orphans := &cobra.Command{Use: "orphans", Short: "List processes RUNTIDE launched that are still alive after a restart", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var os []execution.Orphan
			if err := callAny("runs.orphans", nil, &os); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(os)
			}
			rows := [][]string{}
			for _, o := range os {
				rows = append(rows, []string{o.ExecutionID, fmt.Sprint(o.PID), o.Exe, fmt.Sprint(o.Verified), ts(o.StartedAt)})
			}
			table([]string{"EXECUTION", "PID", "EXE", "VERIFIED", "STARTED"}, rows)
			if len(rows) == 0 {
				fmt.Println("No orphaned processes.")
			}
			return nil
		}}
	cleanup := &cobra.Command{Use: "cleanup [id]", Short: "Terminate orphaned processes that are verified to be RUNTIDE's own", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				ok, err := confirm("Terminate orphaned processes RUNTIDE launched earlier? Unverified entries are never touched.")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			var out map[string]int
			if err := callAny("runs.cleanup", map[string]any{"id": id, "confirm": true}, &out); err != nil {
				return err
			}
			fmt.Printf("terminated %d, skipped %d\n", out["terminated"], out["skipped"])
			return nil
		}}
	cleanup.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	r.AddCommand(create, start, list, status, logs, events, verify, exe, finish, cancel, files, diff, export, compare, orphans, cleanup)
	return r
}

func stateColor(s domain.State) string {
	switch s {
	case domain.StateCompleted:
		return "32"
	case domain.StateFailed, domain.StateInterrupted:
		return "31"
	case domain.StateCancelled, domain.StateRequiresAttention:
		return "33"
	}
	return "36"
}

func printOutputLine(asJSON bool, name, stream, line string, t time.Time) {
	if asJSON {
		_ = printJSON(map[string]any{"command": name, "stream": stream, "line": line, "time": t})
		return
	}
	l := line
	if stream == "stderr" {
		l = color("33", line)
	}
	fmt.Printf("%s %-14s %s\n", t.Local().Format("15:04:05.000"), trunc(name, 14), l)
}

func compareCmd(a, b string) error {
	var c receipts.Comparison
	if err := callAny("receipts.compare", map[string]string{"a": a, "b": b}, &c); err != nil {
		return err
	}
	if jsonOut {
		return printJSON(c)
	}
	fmt.Printf("A: %s  %s  verification %s\nB: %s  %s  verification %s\n", c.A.ExecutionID, c.A.Status, dash(c.A.Verification), c.B.ExecutionID, c.B.Status, dash(c.B.Verification))
	fmt.Printf("Duration: %dms -> %dms (%+dms)\n", c.Durations.AMs, c.Durations.BMs, c.Durations.DeltaMs)
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Println("\n" + title)
		for _, i := range items {
			fmt.Println("  -", i)
		}
	}
	section("Environment differences", c.Environment.Changed)
	var tl []string
	for _, t := range c.Environment.Tools {
		tl = append(tl, fmt.Sprintf("%s: %s %s -> %s", t.ID, t.Change, dash(t.A), dash(t.B)))
	}
	section("Tool versions", tl)
	section("New failures (verification)", c.Verification.NewFailures)
	section("Resolved failures (verification)", c.Verification.Resolved)
	var sc []string
	for _, s := range c.Verification.StatusChanges {
		sc = append(sc, fmt.Sprintf("%s: %s -> %s", s.Name, s.A, s.B))
	}
	section("Verification status changes", sc)
	section("New commands", c.Commands.OnlyInB)
	section("Removed commands", c.Commands.OnlyInA)
	var cc []string
	for _, x := range c.Commands.Changed {
		cc = append(cc, fmt.Sprintf("%s: exit %s -> %s, output %s, %dms -> %dms", x.Key, x.ExitA, x.ExitB, x.OutputChanged, x.DurationAMs, x.DurationBMs))
	}
	section("Changed commands", cc)
	section("Files only in A", c.Files.OnlyInA)
	section("Files only in B", c.Files.OnlyInB)
	section("Files with different resulting content", c.Files.Changed)
	var sv []string
	for _, s := range c.Services {
		sv = append(sv, fmt.Sprintf("%s: %s -> %s", s.Name, s.A, s.B))
	}
	section("Service health differences", sv)
	section("New failure events", c.Failures.New)
	section("Resolved failure events", c.Failures.Resolved)
	section("Evidence missing or not comparable", c.Missing)
	section("Notes", c.Notes)
	return nil
}

func receiptCmd() *cobra.Command {
	rc := &cobra.Command{Use: "receipt", Short: "Inspect, verify, import, compare and replay Run Receipts"}
	var yes bool
	inspect := &cobra.Command{Use: "inspect <file|id>", Short: "Verify and summarise a receipt", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct {
				Receipt      receipts.Receipt      `json:"receipt"`
				Verification receipts.Verification `json:"verification"`
			}
			if err := callAny("receipts.inspect", map[string]string{"ref": args[0]}, &out); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(out)
			}
			r, v := out.Receipt, out.Verification
			m := r.Manifest
			fmt.Printf("Run Receipt (format %s v%d, RUNTIDE %s)\n  execution:  %s\n  project:    %s\n  task:       %s\n  status:     %s   verification: %s   isolation: %s\n  created:    %s\n  sha256:     %s\n  integrity:  OK (%d files verified)\n",
				m.Format, m.FormatVersion, m.RuntideVersion, m.ExecutionID, m.ProjectName, m.Task, m.Status, dash(m.Verification), m.Isolation, ts(m.CreatedAt), r.SHA256, v.FilesChecked)
			if v.Signature != nil {
				fmt.Printf("  signature:  key %s valid=%v trusted=%v\n", v.Signature.Fingerprint, v.Signature.Valid, v.Signature.Trusted)
			} else {
				fmt.Println("  signature:  none")
			}
			fmt.Printf("  reproducibility: %s\n", r.Reproduction.Class)
			for _, x := range r.Reproduction.Reasons {
				fmt.Println("    -", x)
			}
			fmt.Printf("  %d commands, %d events, %d file changes, %d verification checks\n", len(r.Commands), len(r.Events), len(r.FileChanges), len(r.Verification.Results))
			for _, w := range v.Warnings {
				fmt.Println(color("33", "  note: ") + w)
			}
			fmt.Println("  " + v.Authenticity)
			return nil
		}}
	var trust []string
	verify := &cobra.Command{Use: "verify <file|id>", Short: "Check a receipt's integrity (exit code 3 if it is not valid)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var v receipts.Verification
			if err := callAny("receipts.verify", map[string]string{"ref": args[0]}, &v); err != nil {
				return err
			}
			if jsonOut {
				_ = printJSON(v)
			} else {
				if v.Valid {
					fmt.Printf("VALID: %d files match the manifest (archive sha256 %s)\n", v.FilesChecked, v.SHA256)
				} else {
					fmt.Println(color("31", "INVALID"))
				}
				for _, p := range v.Problems {
					fmt.Println("  problem:", p)
				}
				for _, w := range v.Warnings {
					fmt.Println("  note:", w)
				}
				fmt.Println(v.Authenticity)
			}
			if !v.Valid {
				return &exitError{exitFindings, ""}
			}
			return nil
		}}
	verify.Flags().StringSliceVar(&trust, "trust", nil, "(trusted fingerprints are configured with `runtide config set trustedKeys ...`)")
	_ = verify.Flags().MarkHidden("trust")
	imp := &cobra.Command{Use: "import <file>", Short: "Verify and index a receipt (never executes anything)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, _ := filepath.Abs(args[0])
			var out struct {
				Receipt      store.ReceiptRecord   `json:"receipt"`
				Verification receipts.Verification `json:"verification"`
			}
			if err := callAny("receipts.import", map[string]string{"ref": abs}, &out); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(out)
			}
			fmt.Printf("Imported %s (execution %s, status %s). Stored at %s\n", out.Receipt.ID, out.Receipt.ExecutionID, out.Receipt.Status, out.Receipt.Path)
			return nil
		}}
	var htmlOut string
	report := &cobra.Command{Use: "report <file|id>", Short: "Generate a self-contained HTML report", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var h string
			if err := callAny("receipts.report", map[string]string{"ref": args[0]}, &h); err != nil {
				return err
			}
			if htmlOut == "" {
				fmt.Print(h)
				return nil
			}
			if _, err := os.Stat(htmlOut); err == nil {
				return fmt.Errorf("%s already exists", htmlOut)
			}
			if err := os.WriteFile(htmlOut, []byte(h), 0o644); err != nil {
				return err
			}
			fmt.Println("Report written to", htmlOut)
			return nil
		}}
	report.Flags().StringVarP(&htmlOut, "output", "o", "", "write to this .html file")
	list := &cobra.Command{Use: "list", Short: "List known receipts", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		var rs []store.ReceiptRecord
		if err := callAny("receipts.list", nil, &rs); err != nil {
			return err
		}
		if jsonOut {
			return printJSON(rs)
		}
		rows := [][]string{}
		for _, r := range rs {
			src := "exported"
			if r.Imported {
				src = "imported"
			}
			rows = append(rows, []string{r.ID, r.ProjectName, r.Status, src, dash(r.SignedBy), r.SHA256[:12], ts(r.CreatedAt)})
		}
		table([]string{"ID", "PROJECT", "STATUS", "SOURCE", "SIGNER", "SHA256", "CREATED"}, rows)
		return nil
	}}
	compare := &cobra.Command{Use: "compare <a> <b>", Short: "Compare two receipts", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error { return compareCmd(args[0], args[1]) }}
	keygen := &cobra.Command{Use: "keygen", Short: "Create the Ed25519 key used to sign receipts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var out map[string]string
			if err := callAny("receipts.keygen", nil, &out); err != nil {
				return err
			}
			fmt.Printf("Signing key created: %s\nFingerprint: %s\nShare the fingerprint out of band so others can trust it. Keep the key file private.\n", out["path"], out["fingerprint"])
			return nil
		}}
	var projectDir, projectName string
	var envKV []string
	var plan bool
	replay := &cobra.Command{Use: "replay <file|id>", Short: "Re-run a receipt's recorded steps in an isolated worktree and compare the result", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := map[string]string{}
			for _, kv := range envKV {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return &exitError{exitUsage, "--env expects KEY=VALUE"}
				}
				env[k] = v
			}
			if projectDir != "" {
				projectDir, _ = filepath.Abs(projectDir)
			}
			req := map[string]any{"ref": args[0], "projectDir": projectDir, "projectName": projectName, "env": env}
			var p receipts.ReplayPlan
			if err := callAny("receipts.plan", req, &p); err != nil {
				return err
			}
			fmt.Printf("Reproducibility (assessed on this machine): %s\n", p.Class)
			for _, x := range p.Reasons {
				fmt.Println("  -", x)
			}
			if len(p.Missing) > 0 {
				fmt.Println("  not captured:", strings.Join(p.Missing, "; "))
			}
			for _, x := range p.ToolIssues {
				fmt.Println(color("33", "  tool: ") + x)
			}
			fmt.Println("Steps that would run:")
			for _, x := range p.Steps {
				fmt.Println("   ", x)
			}
			for _, x := range p.Blockers {
				fmt.Println(color("31", "  blocked: ") + x)
			}
			if plan || len(p.Blockers) > 0 {
				if len(p.Blockers) > 0 {
					return &exitError{exitFailure, "replay is not possible yet"}
				}
				return nil
			}
			if !yes {
				ok, err := confirm("Execute these steps in a new isolated workspace?")
				if err != nil {
					return err
				}
				if !ok {
					return &exitError{exitFailure, "cancelled"}
				}
			}
			req["approve"] = true
			var res receipts.ReplayResult
			err := callRunning("receipts.replay", req, &res)
			if res.ExecutionID != "" {
				fmt.Printf("\nReplay execution %s finished: %s\nNew receipt: %s\n", res.ExecutionID, res.State, res.ReceiptPath)
			}
			if res.Comparison != nil {
				fmt.Printf("Verification: %s -> %s; new failures: %v; resolved: %v\n", res.Comparison.Verification.Overall[0], res.Comparison.Verification.Overall[1], res.Comparison.Verification.NewFailures, res.Comparison.Verification.Resolved)
				fmt.Printf("Full comparison: runtide receipt compare %s %s\n", args[0], res.ReceiptPath)
			}
			return err
		}}
	replay.Flags().StringVar(&projectDir, "project-dir", "", "local git checkout containing the recorded commit")
	replay.Flags().StringVar(&projectName, "project", "", "existing RUNTIDE project to attach the replay to")
	replay.Flags().StringSliceVar(&envKV, "env", nil, "KEY=VALUE for secret variables the receipt withheld")
	replay.Flags().BoolVar(&plan, "plan", false, "only show the plan")
	replay.Flags().BoolVarP(&yes, "yes", "y", false, "execute without asking")
	rc.AddCommand(inspect, verify, imp, report, list, compare, keygen, replay, pushCmd())
	return rc
}

func serverCmd() *cobra.Command {
	s := &cobra.Command{Use: "server", Short: "Run or check the self-hosted RUNTIDE server"}
	var addr string
	start := &cobra.Command{Use: "start [-- server flags]", Short: "Run runtide-server (found next to runtide.exe) in the foreground", Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, err := os.Executable()
			if err != nil {
				return err
			}
			bin := filepath.Join(filepath.Dir(self), "runtide-server.exe")
			if _, err := os.Stat(bin); err != nil {
				return fmt.Errorf("runtide-server.exe was not found next to %s; see docs/SELF_HOSTING.md", self)
			}
			a := args
			if addr != "" {
				a = append([]string{"--addr", addr}, a...)
			}
			c := exec.Command(bin, a...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			go func() { <-ctx.Done() }()
			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					return &exitError{ee.ExitCode(), ""}
				}
				return err
			}
			return nil
		}}
	start.Flags().StringVar(&addr, "addr", "", "listen address (default 127.0.0.1:8484)")
	var url string
	status := &cobra.Command{Use: "status", Short: "Check a server's health endpoint", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				url = "http://127.0.0.1:8484"
			}
			return serverStatus(strings.TrimRight(url, "/"))
		}}
	status.Flags().StringVar(&url, "url", "", "server base URL (default http://127.0.0.1:8484)")
	s.AddCommand(start, status)
	return s
}
