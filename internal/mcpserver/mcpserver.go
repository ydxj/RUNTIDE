// Package mcpserver exposes RUNTIDE to AI coding agents through the Model
// Context Protocol (official Go SDK). Tools are grouped in permission levels;
// a level is chosen by the human who launches the server, never by the agent:
//
//	read       inspect only
//	controlled read + create workspaces/executions, start (approved workspaces only), verify, export
//	sensitive  controlled + ad-hoc commands, reconfigure/delete workspaces, cancel
//
// Agents cannot approve a workspace's commands: approval is a human action
// (`runtide workspace approve` or the desktop app), so an agent can propose a
// workspace but cannot make RUNTIDE execute it unreviewed.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Backend is the RUNTIDE engine (local API client or in-process engine).
type Backend interface {
	Call(method string, params, out any) error
}

// Levels.
const (
	LevelRead       = "read"
	LevelControlled = "controlled"
	LevelSensitive  = "sensitive"
)

// Config configures the server.
type Config struct {
	Backend Backend
	Level   string
	Version string
}

var (
	refRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:-]{0,127}$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
)

func checkRef(field, v string) error {
	if !refRe.MatchString(v) {
		return fmt.Errorf("%s %q is not a valid identifier", field, v)
	}
	return nil
}

// Out is the structured result of every tool.
type Out struct {
	Result any `json:"result"`
}

type refIn struct {
	ID string `json:"id" jsonschema:"execution id, e.g. ex_018c..."`
}

type listIn struct {
	Project string `json:"project,omitempty" jsonschema:"project name or id (optional)"`
}

type logsIn struct {
	ID      string `json:"id" jsonschema:"execution id"`
	Command string `json:"command,omitempty" jsonschema:"only this command name"`
	Tail    int    `json:"tail,omitempty" jsonschema:"number of last lines per command (default 100, max 2000)"`
}

type eventsIn struct {
	ID       string   `json:"id" jsonschema:"execution id"`
	Types    []string `json:"types,omitempty" jsonschema:"event types, e.g. command.finished"`
	Severity string   `json:"severity,omitempty" jsonschema:"info, warn or error"`
	Search   string   `json:"search,omitempty" jsonschema:"text to search for"`
	Limit    int      `json:"limit,omitempty" jsonschema:"maximum events (default 200, max 2000)"`
}

type receiptIn struct {
	Ref string `json:"ref" jsonschema:"receipt file path or stored receipt id"`
}

type createWorkspaceIn struct {
	Project    string `json:"project" jsonschema:"project name or id"`
	Name       string `json:"name" jsonschema:"workspace name"`
	Task       string `json:"task" jsonschema:"what the task is"`
	Isolation  string `json:"isolation,omitempty" jsonschema:"managed, worktree, docker or wsl (default managed)"`
	ConfigYAML string `json:"configYaml,omitempty" jsonschema:"workspace configuration YAML; the commands in it must be approved by the user before they can run"`
}

type createExecIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace id or project/name"`
	Task      string `json:"task,omitempty" jsonschema:"task description (default: the workspace task)"`
}

type startIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace id or project/name"`
	Task      string `json:"task,omitempty"`
}

type runCmdIn struct {
	ID      string   `json:"id" jsonschema:"execution id (must be RUNNING)"`
	Name    string   `json:"name,omitempty" jsonschema:"label shown in the timeline"`
	Command string   `json:"command" jsonschema:"executable"`
	Args    []string `json:"args,omitempty" jsonschema:"arguments (no shell is involved)"`
	Reason  string   `json:"reason" jsonschema:"why this command is needed; recorded in the audit log"`
}

type configureIn struct {
	Workspace  string `json:"workspace"`
	ConfigYAML string `json:"configYaml" jsonschema:"replacement configuration; revokes the user's approval"`
}

type cancelIn struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

func ok(v any) (*mcp.CallToolResult, Out, error) { return nil, Out{Result: v}, nil }

// New builds the MCP server for the configured permission level.
func New(cfg Config) (*mcp.Server, error) {
	if cfg.Backend == nil {
		return nil, errors.New("backend is required")
	}
	switch cfg.Level {
	case "":
		cfg.Level = LevelControlled
	case LevelRead, LevelControlled, LevelSensitive:
	default:
		return nil, fmt.Errorf("unknown permission level %q (read, controlled, sensitive)", cfg.Level)
	}
	b := cfg.Backend
	call := func(method string, params any) (any, error) {
		var out any
		if err := b.Call(method, params, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "runtide", Version: cfg.Version}, &mcp.ServerOptions{
		Instructions: "RUNTIDE records and verifies software executions. Use read tools freely. Workspace commands only run after the user approves them (runtide workspace approve); you cannot approve them yourself. Report results from get_execution_status and run_verification verbatim; never claim success the evidence does not show.",
	})
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	yes := true
	no := false
	ctl := &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: false}
	sens := &mcp.ToolAnnotations{DestructiveHint: &yes}

	// ---- read-only
	mcp.AddTool(s, &mcp.Tool{Name: "list_projects", Description: "List RUNTIDE projects.", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, Out, error) {
			r, err := call("projects.list", nil)
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_workspaces", Description: "List task workspaces with state, isolation and whether their commands are approved.", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, Out, error) {
			if in.Project != "" {
				if err := checkRef("project", in.Project); err != nil {
					return nil, Out{}, err
				}
			}
			r, err := call("ws.list", map[string]string{"project": in.Project})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "list_executions", Description: "List recent executions.", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, Out, error) {
			if in.Project != "" {
				if err := checkRef("project", in.Project); err != nil {
					return nil, Out{}, err
				}
			}
			r, err := call("runs.list", map[string]any{"project": in.Project, "limit": 30})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	inspect := func(ctx context.Context, _ *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, Out, error) {
		if err := checkRef("id", in.ID); err != nil {
			return nil, Out{}, err
		}
		r, err := call("runs.get", map[string]string{"id": in.ID})
		if err != nil {
			return nil, Out{}, err
		}
		return ok(r)
	}
	mcp.AddTool(s, &mcp.Tool{Name: "inspect_execution", Description: "Full recorded detail of an execution: state, commands, services, verification results, file changes, environment.", Annotations: ro}, inspect)
	mcp.AddTool(s, &mcp.Tool{Name: "get_execution_status", Description: "Compact status of an execution: state, error, verification verdict, per-check results.", Annotations: ro},
		func(ctx context.Context, req *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			var d struct {
				Execution struct {
					ID, State, Task, Error, Verification string
				} `json:"execution"`
				Verification []struct {
					Name, Status, Error string
				} `json:"verification"`
				Commands []struct {
					Name, Kind string
					ExitCode   *int
				} `json:"commands"`
				Active bool `json:"active"`
			}
			if err := b.Call("runs.get", map[string]string{"id": in.ID}, &d); err != nil {
				return nil, Out{}, err
			}
			return ok(map[string]any{"id": d.Execution.ID, "state": d.Execution.State, "task": d.Execution.Task, "error": d.Execution.Error,
				"verification": d.Execution.Verification, "checks": d.Verification, "commands": d.Commands, "active": d.Active})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_execution_logs", Description: "Recorded stdout/stderr of an execution's commands (secrets are redacted).", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, in logsIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			if in.Tail <= 0 {
				in.Tail = 100
			}
			if in.Tail > 2000 {
				return nil, Out{}, errors.New("tail must be at most 2000")
			}
			var d struct {
				Commands []struct{ ID, Name string } `json:"commands"`
			}
			if err := b.Call("runs.get", map[string]string{"id": in.ID}, &d); err != nil {
				return nil, Out{}, err
			}
			out := map[string][]string{}
			for _, c := range d.Commands {
				if in.Command != "" && c.Name != in.Command {
					continue
				}
				var ls []struct{ Stream, Line string }
				if err := b.Call("runs.output", map[string]any{"commandId": c.ID, "limit": 100000}, &ls); err != nil {
					return nil, Out{}, err
				}
				if len(ls) > in.Tail {
					ls = ls[len(ls)-in.Tail:]
				}
				for _, l := range ls {
					out[c.Name] = append(out[c.Name], "["+l.Stream+"] "+l.Line)
				}
			}
			return ok(out)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_execution_events", Description: "The execution timeline (command, process, service, verification and state events).", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, in eventsIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			if in.Limit <= 0 {
				in.Limit = 200
			}
			if in.Limit > 2000 {
				return nil, Out{}, errors.New("limit must be at most 2000")
			}
			r, err := call("runs.events", map[string]any{"id": in.ID, "types": in.Types, "severity": in.Severity, "search": in.Search, "limit": in.Limit})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "inspect_receipt", Description: "Verify the integrity of a Run Receipt and summarise it (the verification states what integrity does and does not prove).", Annotations: ro},
		func(ctx context.Context, _ *mcp.CallToolRequest, in receiptIn) (*mcp.CallToolResult, Out, error) {
			if strings.TrimSpace(in.Ref) == "" || len(in.Ref) > 1024 {
				return nil, Out{}, errors.New("ref is required")
			}
			var out struct {
				Receipt struct {
					Manifest     any `json:"manifest"`
					Reproduction any `json:"reproduction"`
					Verification any `json:"verification"`
					Commands     []any
					FileChanges  []any
				} `json:"receipt"`
				Verification any `json:"verification"`
			}
			if err := b.Call("receipts.inspect", map[string]string{"ref": in.Ref}, &out); err != nil {
				return nil, Out{}, err
			}
			return ok(map[string]any{"manifest": out.Receipt.Manifest, "integrity": out.Verification, "reproduction": out.Receipt.Reproduction,
				"verificationResults": out.Receipt.Verification, "commandCount": len(out.Receipt.Commands), "fileChangeCount": len(out.Receipt.FileChanges)})
		})

	if cfg.Level == LevelRead {
		return s, nil
	}

	// ---- controlled
	mcp.AddTool(s, &mcp.Tool{Name: "create_workspace", Description: "Create a task workspace. Its configured commands must be approved by the user before any execution can run them.", Annotations: ctl},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createWorkspaceIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("project", in.Project); err != nil {
				return nil, Out{}, err
			}
			if !nameRe.MatchString(in.Name) {
				return nil, Out{}, fmt.Errorf("name %q is invalid (letters, digits, . _ -)", in.Name)
			}
			if len(in.ConfigYAML) > 64<<10 || len(in.Task) > 2000 {
				return nil, Out{}, errors.New("configYaml (64 KiB) or task (2000 chars) is too long")
			}
			r, err := call("ws.create", map[string]any{"project": in.Project, "name": in.Name, "task": in.Task, "isolation": in.Isolation, "configYaml": in.ConfigYAML, "confirm": true})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(map[string]any{"workspace": r, "next": "The user must review and approve this workspace (runtide workspace approve " + in.Project + "/" + in.Name + ") before start_workspace can run it."})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_execution", Description: "Create an execution (state CREATED) in a READY workspace without starting it.", Annotations: ctl},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createExecIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("workspace", in.Workspace); err != nil {
				return nil, Out{}, err
			}
			r, err := call("runs.create", map[string]string{"workspace": in.Workspace, "task": in.Task})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "start_workspace", Description: "Create an execution in the workspace and start it. Fails unless the user approved the workspace's current commands.", Annotations: ctl},
		func(ctx context.Context, _ *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("workspace", in.Workspace); err != nil {
				return nil, Out{}, err
			}
			var w struct {
				Workspace struct{ Approved bool } `json:"workspace"`
				Commands  []string                `json:"commands"`
			}
			if err := b.Call("ws.get", map[string]string{"ref": in.Workspace}, &w); err != nil {
				return nil, Out{}, err
			}
			if !w.Workspace.Approved && len(w.Commands) > 0 {
				return nil, Out{}, fmt.Errorf("the user has not approved this workspace's commands, so it cannot start. Ask them to run: runtide workspace approve %s. Commands awaiting approval: %s", in.Workspace, strings.Join(w.Commands, " | "))
			}
			var x struct{ ID string }
			if err := b.Call("runs.create", map[string]string{"workspace": in.Workspace, "task": in.Task}, &x); err != nil {
				return nil, Out{}, err
			}
			if err := b.Call("runs.start", map[string]string{"id": x.ID}, nil); err != nil {
				return nil, Out{}, fmt.Errorf("execution %s was created but did not start: %w", x.ID, err)
			}
			return ok(map[string]any{"executionId": x.ID, "state": "RUNNING", "next": "use run_verification or get_execution_status"})
		})
	mcp.AddTool(s, &mcp.Tool{Name: "run_verification", Description: "Run the workspace's verification pipeline against a RUNNING execution and return real per-check results. The execution ends COMPLETED only if every required check passed.", Annotations: ctl},
		func(ctx context.Context, _ *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			var out any
			if err := b.Call("runs.verify", map[string]string{"id": in.ID}, &out); err != nil {
				return nil, Out{}, err
			}
			return ok(out)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "export_receipt", Description: "Export a Run Receipt for an ended execution (no diffs, no secrets) and return its path and manifest.", Annotations: ctl},
		func(ctx context.Context, _ *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			r, err := call("receipts.export", map[string]any{"id": in.ID})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	if cfg.Level != LevelSensitive {
		return s, nil
	}

	// ---- sensitive (explicitly enabled by the human who launched this server)
	mcp.AddTool(s, &mcp.Tool{Name: "run_command", Description: "SENSITIVE: run an ad-hoc recorded command inside a RUNNING execution. The workspace policy must allow ad-hoc commands and may restrict executables.", Annotations: sens},
		func(ctx context.Context, _ *mcp.CallToolRequest, in runCmdIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			if strings.TrimSpace(in.Command) == "" || len(in.Command) > 512 || len(in.Args) > 64 || strings.TrimSpace(in.Reason) == "" {
				return nil, Out{}, errors.New("command (max 512 chars), args (max 64) and reason are required")
			}
			for _, a := range in.Args {
				if len(a) > 4096 {
					return nil, Out{}, errors.New("argument too long")
				}
			}
			r, err := call("runs.exec", map[string]any{"id": in.ID, "name": in.Name, "command": in.Command, "args": in.Args, "approved": true})
			if err != nil {
				return nil, Out{}, err
			}
			return ok(r)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "configure_workspace", Description: "SENSITIVE: replace a workspace configuration. This revokes the user's approval.", Annotations: sens},
		func(ctx context.Context, _ *mcp.CallToolRequest, in configureIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("workspace", in.Workspace); err != nil {
				return nil, Out{}, err
			}
			if len(in.ConfigYAML) > 64<<10 {
				return nil, Out{}, errors.New("configYaml too long")
			}
			if err := b.Call("ws.configure", map[string]string{"ref": in.Workspace, "yaml": in.ConfigYAML}, nil); err != nil {
				return nil, Out{}, err
			}
			return ok("configuration replaced; approval revoked")
		})
	mcp.AddTool(s, &mcp.Tool{Name: "cancel_execution", Description: "SENSITIVE: cancel an execution and stop everything it started.", Annotations: sens},
		func(ctx context.Context, _ *mcp.CallToolRequest, in cancelIn) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("id", in.ID); err != nil {
				return nil, Out{}, err
			}
			if err := b.Call("runs.cancel", map[string]string{"id": in.ID, "reason": "cancelled by agent: " + in.Reason}, nil); err != nil {
				return nil, Out{}, err
			}
			return ok("cancelled")
		})
	mcp.AddTool(s, &mcp.Tool{Name: "delete_workspace", Description: "SENSITIVE: delete a workspace record that has no recorded executions.", Annotations: sens},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			Workspace string `json:"workspace"`
		}) (*mcp.CallToolResult, Out, error) {
			if err := checkRef("workspace", in.Workspace); err != nil {
				return nil, Out{}, err
			}
			if err := b.Call("ws.delete", map[string]any{"ref": in.Workspace, "confirm": true}, nil); err != nil {
				return nil, Out{}, err
			}
			return ok("deleted")
		})
	return s, nil
}

// JSON helper for tests and callers.
func JSON(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
