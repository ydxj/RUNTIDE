package procutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Files that may start a process without procutil.Hide, and why.
var allowed = map[string]string{
	"internal/capsule/capsule.go":           "sets CREATE_NO_WINDOW itself (task commands)",
	"internal/supervisor/supervisor.go":     "sets CREATE_NO_WINDOW itself (services)",
	"cmd/runtide/backend.go":                "starts the detached engine with its own creation flags",
	"cmd/runtide/commands_exec.go":          "command-line tool: helper runs in the user's own console",
	"cmd/runtide/commands_integrations.go":  "command-line tool: helper runs in the user's own console",
	"internal/procutil/procutil_windows.go": "the helper itself",
}

// TestEveryHelperProcessHidesItsWindow guards against the bug that made RUNTIDE look like malware:
// the background engine has no console, so any console program it starts without CREATE_NO_WINDOW
// opens a visible console window. Every exec.Command in shipped code must be wrapped in
// procutil.Hide (or live in an allowed file).
func TestEveryHelperProcessHidesItsWindow(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "dist", "frontend", "tests", "build", "wailsjs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok || !isExecCommand(call) {
				return true
			}
			checked++
			if _, ok := allowed[rel]; ok {
				return true
			}
			if len(stack) >= 2 {
				if parent, ok := stack[len(stack)-2].(*ast.CallExpr); ok && isProcutilHide(parent) {
					return true
				}
				// exec.Command(...).Output() style: the selector chain sits above Hide's argument only
				// when wrapped, so anything else is a violation.
			}
			assert.Failf(t, "process started without procutil.Hide",
				"%s:%d starts a process without procutil.Hide; a console window would flash on Windows", rel, fset.Position(call.Pos()).Line)
			return true
		})
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, checked, 5, "the scan found suspiciously few process launches; is it looking in the right place?")
}

func isExecCommand(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

func isProcutilHide(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "procutil" && sel.Sel.Name == "Hide"
}
