import { useState } from 'react'
import { FolderOpen, RefreshCw, ScanSearch } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { EnvReport, ToolInfo, WorkspaceRow } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  PageHeader,
  Panel,
  Select,
  Spinner,
  cx,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { Modal } from '../../components/ui/Overlays'

function Tools() {
  const t = usePoll(() => call<ToolInfo[]>('tools.list'), [], 0)
  const cols: Column<ToolInfo>[] = [
    {
      key: 'name',
      header: 'Tool',
      sort: (a, b) => a.name.localeCompare(b.name),
      render: (x) => <span className="font-medium">{x.name}</span>,
    },
    {
      key: 'status',
      header: 'Status',
      width: '120px',
      render: (x) => (
        <Badge tone={x.installed ? 'ok' : 'neutral'}>
          {x.installed ? 'installed' : 'not installed'}
        </Badge>
      ),
    },
    {
      key: 'ver',
      header: 'Version',
      width: '170px',
      render: (x) => (
        <span title={(x.others ?? []).map((o) => `${o.version} - ${o.path}`).join('\n')}>
          {x.version || '-'}
          {(x.others ?? []).length > 0 && (
            <span className="ml-1 text-xs text-muted">+{x.others?.length} more</span>
          )}
        </span>
      ),
    },
    {
      key: 'path',
      header: 'On PATH',
      width: '90px',
      render: (x) => (x.installed ? x.onPath ? 'yes' : <span className="text-warn">no</span> : '-'),
    },
    { key: 'method', header: 'Detected via', width: '120px', render: (x) => x.method || '-' },
    {
      key: 'loc',
      header: 'Location',
      mono: true,
      render: (x) => <span title={x.note ?? x.path}>{x.path || x.note || '-'}</span>,
    },
  ]
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
      {t.loading && !t.data ? (
        <Spinner label="Detecting tools" />
      ) : t.error ? (
        <ErrorState message={t.error} onRetry={() => void t.reload()} />
      ) : (
        <DataTable
          label="Developer tools"
          columns={cols}
          rows={t.data ?? []}
          rowKey={(x) => x.id}
          initialSort={{ key: 'name', dir: 'asc' }}
        />
      )}
      <div className="flex items-center justify-between border-t border-line px-3 py-2 text-xs text-muted">
        <span>Each tool is run to read its real version. RUNTIDE never edits PATH.</span>
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => void t.reload()}>
          Re-scan
        </Button>
      </div>
    </div>
  )
}

function EnvFiles() {
  const toast = useUI((s) => s.toast)
  const ws = usePoll(() => call<WorkspaceRow[]>('workspaces.list'), [], 0)
  const [dir, setDir] = useState('')
  const [report, setReport] = useState<EnvReport>()
  const [err, setErr] = useState<string>()
  const [example, setExample] = useState<{
    source: string
    content: string
    destination: string
  } | null>(null)

  const inspect = async (d = dir) => {
    if (!d) return
    try {
      setReport(await call<EnvReport>('env.inspect', { dir: d }))
      setErr(undefined)
    } catch (e) {
      setReport(undefined)
      setErr(errMsg(e))
    }
  }
  const browse = async () => {
    try {
      const d = await native.pickDirectory('Choose a project folder')
      if (d) {
        setDir(d)
        void inspect(d)
      }
    } catch (e) {
      reportError('Could not open folder picker', e)
    }
  }
  const preview = async (source: string) => {
    try {
      const r = await call<{ content: string; destination: string }>('env.generateExample', {
        source,
        keepSafe: true,
        write: false,
      })
      setExample({ source, ...r })
    } catch (e) {
      reportError('Could not generate .env.example', e)
    }
  }
  const write = async () => {
    if (!example) return
    try {
      const r = await call<{ backup?: string }>('env.generateExample', {
        source: example.source,
        keepSafe: true,
        write: true,
      })
      toast({
        kind: 'success',
        title: '.env.example written',
        detail: r.backup ? `Previous file backed up to ${r.backup}` : example.destination,
      })
      setExample(null)
      void inspect()
    } catch (e) {
      reportError('Write failed', e)
    }
  }

  return (
    <div className="min-h-0 flex-1 space-y-3 overflow-auto">
      <div className="flex gap-2">
        <Select
          aria-label="Pick a workspace folder"
          value=""
          onChange={(e) => {
            setDir(e.target.value)
            void inspect(e.target.value)
          }}
        >
          <option value="">Workspace folders…</option>
          {(ws.data ?? []).map((w) => (
            <option key={w.name} value={w.root}>
              {w.name}
            </option>
          ))}
        </Select>
        <Input
          aria-label="Project folder"
          className="flex-1"
          placeholder="D:\projects\my-app"
          value={dir}
          onChange={(e) => setDir(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && void inspect()}
        />
        <IconButton label="Browse for folder" onClick={() => void browse()}>
          <FolderOpen className="h-4 w-4" />
        </IconButton>
        <Button
          variant="primary"
          icon={<ScanSearch className="h-3.5 w-3.5" />}
          onClick={() => void inspect()}
        >
          Inspect
        </Button>
      </div>
      <p className="text-xs text-muted">
        Secret values are masked. Nothing here is uploaded anywhere, and no file is changed without
        your confirmation (existing files are backed up first).
      </p>
      {err && (
        <div role="alert" className="rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad">
          {err}
        </div>
      )}
      {report && (report.files ?? []).length === 0 && (
        <EmptyState
          title="No .env files here"
          hint="Looked for .env, .env.local, .env.development, .env.example and similar."
        />
      )}
      {(report?.files ?? []).map((f) => (
        <Panel
          key={f.path}
          title={<span className="selectable font-mono text-xs">{f.path}</span>}
          actions={
            !/example|sample/.test(f.path) && (
              <Button onClick={() => void preview(f.path)}>Generate .env.example…</Button>
            )
          }
        >
          <table aria-label={`Variables in ${f.path}`} className="w-full text-sm">
            <tbody>
              {(f.entries ?? []).map((e) => (
                <tr key={e.key + e.line} className="h-7 border-b border-line/60">
                  <td className="w-12 px-3 text-right text-xs text-muted">{e.line}</td>
                  <td className="selectable px-3 font-mono text-xs">{e.key}</td>
                  <td
                    className={cx(
                      'selectable px-3 font-mono text-xs',
                      e.sensitive ? 'text-muted' : '',
                    )}
                  >
                    {e.value || <span className="text-muted">(empty)</span>}
                  </td>
                  <td className="px-3 text-right">
                    {e.sensitive && <Badge tone="warn">secret</Badge>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="space-y-1 p-3 text-sm">
            {(f.duplicates ?? []).length > 0 && (
              <div className="text-warn">Duplicate definitions: {f.duplicates?.join(', ')}</div>
            )}
            {(f.invalid ?? []).map((i) => (
              <div key={i.line} className="text-bad">
                Line {i.line}: {i.reason}
              </div>
            ))}
            {(f.secrets ?? []).map((s) => (
              <div key={s.key + s.line} className="text-warn">
                {s.key} (line {s.line}): {s.reason}
              </div>
            ))}
          </div>
        </Panel>
      ))}
      {(report?.missing ?? []).map((m) => (
        <div key={m.file} className="rounded border border-warn/40 bg-warn/10 p-3 text-sm">
          <b>{m.file.split('\\').pop()}</b> is missing {m.keys.length} variable(s) the example
          defines: <span className="selectable font-mono text-xs">{m.keys.join(', ')}</span>
        </div>
      ))}
      {(report?.hints ?? []).map((h) => (
        <div key={h} className="text-sm text-muted">
          {h}
        </div>
      ))}
      {example && (
        <Modal
          title="Generate .env.example"
          width="max-w-2xl"
          onClose={() => setExample(null)}
          footer={
            <>
              <Button onClick={() => setExample(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => void write()}>
                Write file
              </Button>
            </>
          }
        >
          <p className="mb-2 text-sm">
            Secrets are blanked. This will be written to{' '}
            <span className="selectable font-mono text-xs">{example.destination}</span>; an existing
            file is backed up first.
          </p>
          <pre className="selectable max-h-72 overflow-auto rounded border border-line bg-subtle p-2 font-mono text-xs">
            {example.content}
          </pre>
        </Modal>
      )}
    </div>
  )
}

export function EnvironmentPage() {
  const [tab, setTab] = useState<'tools' | 'env'>('tools')
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Environment"
        subtitle="Installed runtimes and project environment files."
      />
      <div role="tablist" aria-label="Environment sections" className="flex gap-1 px-6 pb-2">
        {(
          [
            ['tools', 'Developer tools'],
            ['env', 'Environment files'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={cx(
              'h-8 rounded-md px-3 text-sm',
              tab === id
                ? 'bg-surface font-medium shadow-[0_0_0_1px_rgb(var(--line))]'
                : 'hover:bg-surface/60',
            )}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="flex min-h-0 flex-1 flex-col px-6 pb-4">
        {tab === 'tools' ? <Tools /> : <EnvFiles />}
      </div>
    </div>
  )
}
