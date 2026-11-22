package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/runtide/runtide/internal/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// engineBackend drives a real in-process RUNTIDE engine.
type engineBackend struct{ e *app.Engine }

func (b engineBackend) Call(method string, params, out any) error {
	pb, _ := json.Marshal(params)
	res, err := b.e.Dispatch(context.Background(), method, pb)
	if err != nil {
		return err
	}
	if out == nil || res == nil {
		return nil
	}
	rb, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(rb, out)
}

func connect(t *testing.T, level string) (*mcp.ClientSession, *app.Engine) {
	t.Helper()
	e, err := app.New(app.Options{Home: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { e.Close(true) })
	srv, err := New(Config{Backend: engineBackend{e}, Level: level, Version: "test"})
	require.NoError(t, err)
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	_, err = srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })
	return cs, e
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	res, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	m := map[string]bool{}
	for _, tl := range res.Tools {
		m[tl.Name] = true
	}
	return m
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, name)
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	var sc map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &sc)
	}
	return sc, text, res.IsError
}

func TestPermissionLevelsControlWhichToolsExist(t *testing.T) {
	read, _ := connect(t, LevelRead)
	names := toolNames(t, read)
	for _, n := range []string{"list_projects", "list_workspaces", "inspect_execution", "get_execution_status", "get_execution_logs", "get_execution_events", "inspect_receipt", "list_verification_profiles"} {
		assert.True(t, names[n], n)
	}
	for _, n := range []string{"create_workspace", "start_workspace", "run_verification", "export_receipt", "run_command", "delete_workspace"} {
		assert.False(t, names[n], "read level must not offer "+n)
	}
	ctl, _ := connect(t, LevelControlled)
	names = toolNames(t, ctl)
	for _, n := range []string{"create_workspace", "create_execution", "start_workspace", "run_verification", "export_receipt"} {
		assert.True(t, names[n], n)
	}
	for _, n := range []string{"run_command", "configure_workspace", "cancel_execution", "delete_workspace"} {
		assert.False(t, names[n], "controlled level must not offer sensitive tool "+n)
	}
	sens, _ := connect(t, LevelSensitive)
	names = toolNames(t, sens)
	assert.True(t, names["run_command"] && names["delete_workspace"])
	// Profiles can contain commands: agents may list and use them, never create or change them.
	for name := range names {
		if strings.Contains(name, "profile") {
			assert.Equal(t, "list_verification_profiles", name, "no tool may write profiles, even at the sensitive level")
		}
	}
	_, err := New(Config{Backend: engineBackend{}, Level: "root"})
	assert.Error(t, err)
	// Calling a tool that is not offered fails.
	res, err := read.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_command", Arguments: map[string]any{}})
	assert.True(t, err != nil || res.IsError)
}

func TestAgentWorkflowRequiresHumanApprovalAndReturnsRealEvidence(t *testing.T) {
	cs, e := connect(t, LevelControlled)
	root := t.TempDir()
	_, err := e.Dispatch(context.Background(), "projects.create", mustJSON(map[string]string{"name": "shop", "root": root}))
	require.NoError(t, err)

	yaml := "version: 1\ntask: add greeting\nstart:\n  - {name: greet, command: cmd.exe, args: ['/c', 'echo', 'hello from the agent task']}\nverify:\n  name: v\n  checks:\n    - {name: echoed, command: cmd.exe, args: ['/c', 'echo', 'ok'], expect_output: ok}\n"
	sc, text, isErr := call(t, cs, "create_workspace", map[string]any{"project": "shop", "name": "greeting", "task": "add greeting", "configYaml": yaml})
	require.False(t, isErr, text)
	assert.Contains(t, text, "must review and approve")

	// The agent cannot start it: nobody approved the commands.
	_, text, isErr = call(t, cs, "start_workspace", map[string]any{"workspace": "shop/greeting"})
	require.True(t, isErr)
	assert.Contains(t, text, "has not approved")
	assert.Contains(t, text, "runtide workspace approve")
	assert.Contains(t, text, "greet")
	_ = sc

	// The human approves through the engine, not through MCP.
	_, err = e.Dispatch(context.Background(), "ws.approve", mustJSON(map[string]any{"ref": "shop/greeting", "confirm": true}))
	require.NoError(t, err)
	sc, text, isErr = call(t, cs, "start_workspace", map[string]any{"workspace": "shop/greeting"})
	require.False(t, isErr, text)
	id := sc["result"].(map[string]any)["executionId"].(string)

	sc, text, isErr = call(t, cs, "get_execution_status", map[string]any{"id": id})
	require.False(t, isErr, text)
	assert.Equal(t, "RUNNING", sc["result"].(map[string]any)["state"])

	_, text, isErr = call(t, cs, "get_execution_logs", map[string]any{"id": id, "command": "greet"})
	require.False(t, isErr, text)
	assert.Contains(t, text, "hello from the agent task")

	_, text, isErr = call(t, cs, "run_verification", map[string]any{"id": id})
	require.False(t, isErr, text)
	assert.Contains(t, text, `"overall":"passed"`)
	sc, _, _ = call(t, cs, "get_execution_status", map[string]any{"id": id})
	r := sc["result"].(map[string]any)
	assert.Equal(t, "COMPLETED", r["state"])
	assert.Equal(t, "passed", r["verification"])

	_, text, isErr = call(t, cs, "get_execution_events", map[string]any{"id": id, "types": []string{"command.finished"}})
	require.False(t, isErr, text)
	assert.Contains(t, text, "command.finished")

	sc, text, isErr = call(t, cs, "export_receipt", map[string]any{"id": id})
	require.False(t, isErr, text)
	path := sc["result"].(map[string]any)["path"].(string)
	_, text, isErr = call(t, cs, "inspect_receipt", map[string]any{"ref": path})
	require.False(t, isErr, text)
	assert.Contains(t, text, `"valid":true`)
	assert.Contains(t, text, "do not prove who created")
	_, text, _ = call(t, cs, "list_executions", map[string]any{})
	assert.Contains(t, text, id)
}

func TestToolArgumentValidation(t *testing.T) {
	cs, _ := connect(t, LevelControlled)
	for name, args := range map[string]map[string]any{
		"inspect_execution":    {"id": "../../etc/passwd"},
		"get_execution_status": {"id": "bad id with spaces"},
		"create_workspace":     {"project": "shop", "name": "bad name!", "task": "x"},
		"start_workspace":      {"workspace": ""},
		"get_execution_logs":   {"id": "ex_1", "tail": 999999},
		"get_execution_events": {"id": "ex_1", "limit": 99999},
	} {
		_, text, isErr := call(t, cs, name, args)
		assert.True(t, isErr, name+": "+text)
	}
	_, text, isErr := call(t, cs, "inspect_execution", map[string]any{"id": "ex_doesnotexist"})
	assert.True(t, isErr)
	assert.NotEmpty(t, text)
}

func TestSensitiveCommandRespectsWorkspacePolicy(t *testing.T) {
	cs, e := connect(t, LevelSensitive)
	root := t.TempDir()
	_, err := e.Dispatch(context.Background(), "projects.create", mustJSON(map[string]string{"name": "shop", "root": root}))
	require.NoError(t, err)
	yaml := "version: 1\npolicy: {deny: [powershell]}\n"
	_, _, isErr := call(t, cs, "create_workspace", map[string]any{"project": "shop", "name": "w", "task": "t", "configYaml": yaml})
	require.False(t, isErr)
	sc, text, isErr := call(t, cs, "start_workspace", map[string]any{"workspace": "shop/w"})
	require.False(t, isErr, text)
	id := sc["result"].(map[string]any)["executionId"].(string)
	// Policy forbids ad-hoc commands in this workspace, so even the sensitive tool refuses.
	_, text, isErr = call(t, cs, "run_command", map[string]any{"id": id, "command": "cmd.exe", "args": []string{"/c", "echo", "x"}, "reason": "testing"})
	require.True(t, isErr)
	assert.Contains(t, text, "policy does not allow ad-hoc")
	_, _, isErr = call(t, cs, "run_command", map[string]any{"id": id, "command": "cmd.exe"})
	assert.True(t, isErr, "reason is mandatory")
	_, _, isErr = call(t, cs, "cancel_execution", map[string]any{"id": id, "reason": "done"})
	assert.False(t, isErr)
	time.Sleep(100 * time.Millisecond)
	assert.True(t, strings.Contains(JSON(map[string]string{"a": "b"}), "a"))
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
