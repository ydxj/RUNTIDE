package receiptcore

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/runtide/runtide/internal/domain"
)

// HTML renders a self-contained, script-free report. Every value is
// HTML-escaped by html/template, so log lines cannot inject markup.
func HTML(r *Receipt, v *Verification) (string, error) {
	data := struct {
		R      *Receipt
		V      *Verification
		Events []domain.ExecEvent
		Cut    int
		Dur    func(domain.Command) string
		Exit   func(domain.Command) string
		Time   func(time.Time) string
		Class  string
		Notes  []string
		Gen    string
	}{R: r, V: v, Dur: cmdDuration, Exit: exitStr2, Time: func(t time.Time) string { return t.Format("15:04:05.000") }, Gen: time.Now().UTC().Format(time.RFC3339)}
	const maxEvents = 1000
	data.Events = r.Events
	if len(r.Events) > maxEvents {
		data.Events = r.Events[:maxEvents]
		data.Cut = len(r.Events) - maxEvents
	}
	data.Class = strings.ReplaceAll(r.Reproduction.Class, "_", " ")
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func cmdDuration(c domain.Command) string {
	if c.EndedAt == nil {
		return "-"
	}
	return fmt.Sprintf("%.2fs", c.EndedAt.Sub(c.StartedAt).Seconds())
}

func exitStr2(c domain.Command) string { return exitStr(c) }

var reportTmpl = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Run Receipt {{.R.Manifest.ExecutionID}}</title>
<style>
:root{--bg:#f6f6f6;--card:#fff;--line:#e2e2e2;--ink:#1a1a1a;--muted:#606060;--ok:#107c10;--bad:#c42b1c;--warn:#9d5d00;--acc:#0f6cbd}
@media (prefers-color-scheme:dark){:root{--bg:#202020;--card:#2b2b2b;--line:#3e3e3e;--ink:#fff;--muted:#aaa;--ok:#6ccb5f;--bad:#ff99a4;--warn:#fce100;--acc:#60cdff}}
body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.45 "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif}
main{max-width:1060px;margin:0 auto;padding:24px 16px 64px}h1{font-size:20px;margin:0}h2{font-size:15px;margin:28px 0 8px}
.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px 14px;margin:8px 0}
table{width:100%;border-collapse:collapse;font-size:13px}th,td{text-align:left;padding:5px 8px;border-bottom:1px solid var(--line);vertical-align:top}th{color:var(--muted);font-weight:600}
.muted{color:var(--muted)}.ok{color:var(--ok)}.bad{color:var(--bad)}.warn{color:var(--warn)}code,pre{font:12px "Cascadia Mono",Consolas,monospace}
pre{white-space:pre-wrap;word-break:break-word;margin:4px 0}.pill{border:1px solid var(--line);border-radius:4px;padding:0 6px;font-size:12px}
.notice{border-left:3px solid var(--warn);padding:6px 10px;background:var(--card);margin:8px 0}
</style></head><body><main>
<h1>Run Receipt</h1>
<p class="muted">{{.R.Manifest.ProjectName}} &middot; {{.R.Manifest.Task}}</p>
<div class="card"><table>
<tr><th>Execution</th><td><code>{{.R.Manifest.ExecutionID}}</code></td><th>Status</th><td class="{{if eq .R.Manifest.Status "COMPLETED"}}ok{{else}}bad{{end}}">{{.R.Manifest.Status}}</td></tr>
<tr><th>Verification</th><td>{{.R.Manifest.Verification}}</td><th>Isolation</th><td>{{.R.Manifest.Isolation}}</td></tr>
<tr><th>Created</th><td>{{.R.Manifest.CreatedAt}}</td><th>RUNTIDE</th><td>{{.R.Manifest.RuntideVersion}}</td></tr>
<tr><th>Archive SHA-256</th><td colspan="3"><code>{{.R.SHA256}}</code></td></tr>
</table></div>
<div class="notice"><b>What this report proves.</b> {{.V.Authenticity}} Integrity: <b>{{.V.Integrity}}</b>. Signature: <b>{{.V.SignatureState}}</b>.{{if .R.Signature}} Signer key <code>{{.R.Signature.Fingerprint}}</code> ({{if .R.Signature.Trusted}}trusted{{else}}not in your trusted list{{end}}).{{end}}</div>
{{range .V.Warnings}}<div class="notice warn">{{.}}</div>{{end}}

<h2>Verification: {{.R.Verification.Summary.Overall}}</h2>
<div class="card"><table><tr><th>Check</th><th>Status</th><th>Duration</th><th>Detail</th></tr>
{{range .R.Verification.Results}}<tr><td>{{.Name}}{{if not .Required}} <span class="pill">optional</span>{{end}}</td><td class="{{if eq .Status "passed"}}ok{{else if eq .Status "not_applicable"}}muted{{else}}bad{{end}}">{{.Status}}</td><td>{{.DurationMs}} ms</td><td>{{.Error}}{{if .Output}}<pre>{{.Output}}</pre>{{end}}</td></tr>
{{else}}<tr><td colspan="4" class="muted">No verification results recorded.</td></tr>{{end}}</table></div>

<h2>Commands ({{len .R.Commands}})</h2>
<div class="card"><table><tr><th>#</th><th>Kind</th><th>Command</th><th>Exit</th><th>Time</th></tr>
{{range $i, $c := .R.Commands}}<tr><td>{{$c.Seq}}</td><td>{{$c.Kind}}</td><td><b>{{$c.Name}}</b><br><code>{{$c.Executable}} {{range $c.Args}}{{.}} {{end}}</code>{{if $c.Redacted}} <span class="pill warn">args masked</span>{{end}}{{if $c.Truncated}} <span class="pill warn">output truncated</span>{{end}}</td><td>{{call $.Exit $c}}</td><td>{{call $.Dur $c}}</td></tr>{{end}}</table></div>

<h2>File changes ({{len .R.FileChanges}})</h2>
<div class="card"><table><tr><th>Change</th><th>Path</th><th>Size</th></tr>
{{range .R.FileChanges}}<tr><td>{{.Change}}</td><td><code>{{.Path}}</code>{{if .OldPath}} <span class="muted">(from {{.OldPath}})</span>{{end}}</td><td>{{.SizeBefore}} &rarr; {{.SizeAfter}}</td></tr>
{{else}}<tr><td colspan="3" class="muted">No file changes recorded.</td></tr>{{end}}</table>
<p class="muted">Sensitive files (.env, keys, credentials) are never listed. {{.R.Manifest.Redaction.SensitiveFilesExcluded}} were excluded.</p></div>

<h2>Environment</h2>
<div class="card"><p>{{.R.Environment.OS}}/{{.R.Environment.Arch}} &middot; isolation {{.R.Environment.Isolation}}{{if .R.Environment.Git.Head}} &middot; commit <code>{{.R.Environment.Git.Head}}</code>{{if .R.Environment.Git.Dirty}} (uncommitted changes){{end}}{{end}}</p>
<table><tr><th>Tool</th><th>Version</th></tr>{{range .R.Environment.Tools}}<tr><td>{{.Name}}</td><td>{{.Version}}</td></tr>{{end}}</table></div>

<h2>Reproducibility: {{.Class}}</h2>
<div class="card">{{range .R.Reproduction.Reasons}}<p>{{.}}</p>{{end}}{{if .R.Reproduction.Missing}}<p class="muted">Not captured: {{range .R.Reproduction.Missing}}{{.}}; {{end}}</p>{{end}}
<p class="muted">A receipt is not a machine snapshot. Reproduction re-runs recorded steps; it cannot restore external state, secrets or uncommitted work.</p></div>

<h2>Timeline ({{len .R.Events}} events)</h2>
<div class="card"><table><tr><th>#</th><th>Time</th><th>Type</th><th>Subject</th><th>Source</th></tr>
{{range .Events}}<tr><td>{{.Seq}}</td><td>{{call $.Time .Time}}</td><td class="{{if eq .Severity "error"}}bad{{else if eq .Severity "warn"}}warn{{end}}">{{.Type}}</td><td>{{.Subject}}</td><td class="muted">{{.Source}}</td></tr>{{end}}</table>
{{if .Cut}}<p class="muted">{{.Cut}} further events are in events.json.</p>{{end}}</div>
<p class="muted">Generated {{.Gen}} by RUNTIDE from receipt file. Log output is in the logs/ folder of the archive.</p>
</main></body></html>
`))
