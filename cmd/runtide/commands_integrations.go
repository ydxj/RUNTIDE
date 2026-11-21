package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runtide/runtide/internal/docker"
	"github.com/runtide/runtide/internal/tools"
	"github.com/runtide/runtide/internal/wsl"
	"github.com/spf13/cobra"
)

// mcpBinary finds runtide-mcp.exe next to this executable.
func mcpBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(filepath.Dir(self), "runtide-mcp.exe")
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("runtide-mcp.exe was not found next to %s; see docs/MCP_INTEGRATION.md", self)
	}
	return bin, nil
}

func validLevel(l string) error {
	switch l {
	case "read", "controlled", "sensitive":
		return nil
	}
	return &exitError{exitUsage, fmt.Sprintf("unknown permission level %q (read, controlled, sensitive)", l)}
}

func mcpCmd() *cobra.Command {
	m := &cobra.Command{Use: "mcp", Short: "MCP server for coding agents (Claude Code and other MCP clients)"}
	var level string

	cfg := &cobra.Command{Use: "config", Short: "Print the registration command and JSON for an MCP client (changes nothing)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validLevel(level); err != nil {
				return err
			}
			bin, err := mcpBinary()
			if err != nil {
				return err
			}
			entry := map[string]any{"command": bin, "args": []string{"--level", level}}
			if jsonOut {
				return printJSON(map[string]any{"mcpServers": map[string]any{"runtide": entry}})
			}
			fmt.Printf("Claude Code (run this yourself; RUNTIDE does not modify your Claude configuration):\n\n  claude mcp add runtide -- \"%s\" --level %s\n\n", bin, level)
			b, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"runtide": entry}}, "  ", "  ")
			fmt.Printf("Generic MCP client JSON:\n\n  %s\n\n", b)
			fmt.Println("Permission level", level+":", map[string]string{
				"read":       "inspection only",
				"controlled": "read + create workspaces/executions, start approved workspaces, verify, export receipts",
				"sensitive":  "controlled + ad-hoc commands, reconfigure, cancel, delete (workspace policy still applies)",
			}[level])
			fmt.Println("Agents can never approve a workspace's commands: that stays a human action.")
			return nil
		}}
	cfg.Flags().StringVar(&level, "level", "controlled", "read | controlled | sensitive")

	serve := &cobra.Command{Use: "serve", Short: "Run the MCP server on stdio (what an MCP client launches)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validLevel(level); err != nil {
				return err
			}
			bin, err := mcpBinary()
			if err != nil {
				return err
			}
			c := exec.Command(bin, "--level", level)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					return &exitError{ee.ExitCode(), ""}
				}
				return err
			}
			return nil
		}}
	serve.Flags().StringVar(&level, "level", "controlled", "read | controlled | sensitive")

	check := &cobra.Command{Use: "check", Short: "Start the MCP server, connect as a client and exercise it (read-only calls only)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validLevel(level); err != nil {
				return err
			}
			bin, err := mcpBinary()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			client := mcp.NewClient(&mcp.Implementation{Name: "runtide-mcp-check", Version: "1"}, nil)
			cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin, "--level", level)}, nil)
			if err != nil {
				return &exitError{exitFailure, "could not start or handshake with runtide-mcp: " + err.Error()}
			}
			defer func() { _ = cs.Close() }()
			lt, err := cs.ListTools(ctx, nil)
			if err != nil {
				return &exitError{exitFailure, "tools/list failed: " + err.Error()}
			}
			names := make([]string, 0, len(lt.Tools))
			for _, t := range lt.Tools {
				names = append(names, t.Name)
			}
			sort.Strings(names)
			// A real round trip through the engine, using read-only tools only.
			calls := map[string]string{}
			for _, n := range []string{"list_projects", "list_verification_profiles"} {
				res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: n, Arguments: map[string]any{}})
				switch {
				case err != nil:
					calls[n] = "error: " + err.Error()
				case res.IsError:
					calls[n] = "tool error"
				default:
					calls[n] = "ok"
				}
			}
			failed := false
			for _, v := range calls {
				if v != "ok" {
					failed = true
				}
			}
			if jsonOut {
				_ = printJSON(map[string]any{"level": level, "tools": names, "calls": calls})
			} else {
				fmt.Printf("runtide-mcp (%s): handshake ok, %d tools\n  %s\n", level, len(names), strings.Join(names, "\n  "))
				for _, n := range []string{"list_projects", "list_verification_profiles"} {
					fmt.Printf("  call %-28s %s\n", n, calls[n])
				}
				fmt.Println("This checks RUNTIDE's own MCP server with the Go SDK client. It does not test any particular MCP product.")
			}
			if failed {
				return &exitError{exitFailure, ""}
			}
			return nil
		}}
	check.Flags().StringVar(&level, "level", "read", "read | controlled | sensitive")
	m.AddCommand(cfg, serve, check)
	return m
}

type isolationRow struct {
	Level     int    `json:"level"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail"`
	Scope     string `json:"isolates"`
}

// isolationAvailability probes the machine; it never starts anything.
func isolationAvailability() []isolationRow {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows := []isolationRow{{Level: 1, Name: "managed", Available: true, Detail: "always available",
		Scope: "the process tree (Job Object) and environment only; files and network are not isolated"}}
	git := false
	gv := ""
	for _, t := range tools.Detect(ctx) {
		if t.ID == "git" && t.Installed {
			git, gv = true, t.Version
		}
	}
	d := "git was not found on PATH (non-git projects use a filtered copy)"
	if git {
		d = "git " + gv + " found; the project needs a repository with at least one commit"
	}
	rows = append(rows, isolationRow{Level: 2, Name: "worktree", Available: git, Detail: d,
		Scope: "a separate directory; the process can still reach the rest of your user profile"})
	ds := docker.GetStatus(ctx)
	dd := ds.Message
	if ds.DaemonAvailable {
		dd = "Docker engine " + ds.ServerVersion + " is running"
	}
	rows = append(rows, isolationRow{Level: 3, Name: "docker", Available: ds.DaemonAvailable, Detail: dd,
		Scope: "a container with only the workspace mounted; not a guarantee against malicious code"})
	ws := wsl.GetStatus(ctx)
	var names []string
	for _, x := range ws.Distros {
		if !strings.HasPrefix(x.Name, "docker-desktop") {
			names = append(names, fmt.Sprintf("%s (WSL%d, %s)", x.Name, x.Version, x.State))
		}
	}
	wd := ws.Message
	if ws.Installed && len(names) > 0 {
		wd = "distributions: " + strings.Join(names, ", ")
	}
	rows = append(rows, isolationRow{Level: 4, Name: "wsl", Available: ws.Installed && len(names) > 0, Detail: wd,
		Scope: "inside a WSL2 distribution; Windows drives stay mounted, so this is not a security boundary"})
	return rows
}

func diagnosticsCmd() *cobra.Command {
	d := &cobra.Command{Use: "diagnostics", Aliases: []string{"diag"}, Short: "Diagnose this machine (read-only)"}
	d.AddCommand(&cobra.Command{Use: "isolation", Short: "Which execution isolation levels can run on this machine", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := isolationAvailability()
			if jsonOut {
				return printJSON(rows)
			}
			out := [][]string{}
			for _, r := range rows {
				st := color("32", "available")
				if !r.Available {
					st = color("33", "unavailable")
				}
				out = append(out, []string{fmt.Sprint(r.Level), r.Name, st, r.Detail})
			}
			table([]string{"LEVEL", "NAME", "STATUS", "DETAIL"}, out)
			fmt.Println("\nRUNTIDE never falls back to a weaker level: a missing runtime fails the execution with that reason.")
			return nil
		}})
	d.AddCommand(&cobra.Command{Use: "run", Short: "Run the Doctor checks (same as `runtide doctor`)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dc := doctorCmd()
			dc.SetArgs(nil)
			return dc.RunE(dc, nil)
		}})
	return d
}
