import { useEffect, useMemo, useState } from 'react'
import {
  Download,
  FileUp,
  Play,
  Plus,
  RotateCw,
  ShieldCheck,
  Square,
  Trash2,
  Pencil,
} from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { useEngineEvent } from '../../hooks/useEngineEvent'
import { call, native } from '../../services/api'
import { since } from '../../services/format'
import { reportError, useUI } from '../../stores/ui'
import type {
  WorkspaceHistory,
  WorkspacePreview,
  WorkspaceProgress,
  WorkspaceRow,
  WorkspaceStatus,
} from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  PageHeader,
  Panel,
  Spinner,
  StatusDot,
  cx,
} from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'
import { stateTone, healthTone as svcHealthTone } from '../services/ServicesPage'
import { TEMPLATE, WorkspaceEditor } from './WorkspaceEditor'

const wsTone = (h: string) =>
  h === 'healthy' ? 'ok' : h === 'degraded' ? 'bad' : h === 'stopped' ? 'neutral' : 'warn'

export function WorkspacesPage() {
  const params = useUI((s) => s.pageParams)
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const [sel, setSel] = useState<string | null>(params['name'] ?? null)
  const [editor, setEditor] = useState<{ yaml: string; root: string; editing: boolean } | null>(
    null,
  )
  const [authorize, setAuthorize] = useState<{
    row: WorkspaceRow
    preview: WorkspacePreview
    action: 'up' | 'restart'
  } | null>(null)
  const [progress, setProgress] = useState<WorkspaceProgress[]>([])
  const [running, setRunning] = useState<string | null>(null)

  const list = usePoll(() => call<WorkspaceRow[]>('workspaces.list'), [], 3)
  const rows = useMemo(() => list.data ?? [], [list.data])
  useEffect(() => {
    if (!sel && rows[0]) setSel(rows[0].name)
  }, [rows, sel])
  const current = rows.find((r) => r.name === sel)
  const status = usePoll(
    () =>
      sel ? call<WorkspaceStatus>('workspaces.status', { name: sel }) : Promise.resolve(undefined),
    [sel],
    2,
  )
  const hist = usePoll(
    () =>
      sel
        ? call<WorkspaceHistory[]>('workspaces.history', { name: sel, limit: 6 })
        : Promise.resolve([]),
    [sel],
    6,
  )

  useEngineEvent<WorkspaceProgress>('progress', (p) => {
    if (p.workspace === sel) setProgress((l) => [...l, p].slice(-60))
  })

  const run = async (verb: 'up' | 'down' | 'restart') => {
    if (!current) return
    setProgress([])
    setRunning(verb)
    try {
      await call(`workspaces.${verb}`, { name: current.name })
      toast({ kind: 'success', title: `${current.name}: ${verb === 'down' ? 'stopped' : 'is up'}` })
    } catch (e) {
      reportError(`${current.name}: ${verb} failed`, e)
    } finally {
      setRunning(null)
      void status.reload()
      void list.reload()
      void hist.reload()
    }
  }

  const start = async (action: 'up' | 'restart') => {
    if (!current) return
    if (current.approved) return run(action)
    try {
      const preview = await call<WorkspacePreview>('workspaces.inspect', {
        yaml: current.yaml,
        root: current.root,
      })
      setAuthorize({ row: current, preview, action })
    } catch (e) {
      reportError('Workspace definition is invalid', e)
    }
  }

  const approveAndRun = async () => {
    if (!authorize) return
    const { row, action } = authorize
    setAuthorize(null)
    try {
      await call('workspaces.approve', { name: row.name })
      await list.reload()
      await run(action)
    } catch (e) {
      reportError('Could not authorise workspace', e)
    }
  }

  const importFile = async () => {
    try {
      const f = await native.pickFile('Import a workspace YAML file')
      if (!f) return
      const text = await native.readTextFile(f)
      setEditor({ yaml: text, root: f.replace(/[\\/][^\\/]*$/, ''), editing: false })
    } catch (e) {
      reportError('Could not read file', e)
    }
  }

  const exportYaml = async () => {
    if (!current) return
    try {
      const p = await native.pickSaveFile('Export workspace', `${current.name}.yaml`)
      if (!p) return
      await native.writeTextFile(p, await call<string>('workspaces.export', { name: current.name }))
      toast({ kind: 'success', title: 'Workspace exported', detail: p })
    } catch (e) {
      reportError('Export failed', e)
    }
  }

  const remove = () => {
    if (!current) return
    ask({
      title: `Delete workspace ${current.name}?`,
      danger: true,
      confirmLabel: 'Delete workspace',
      body: 'The workspace definition and its history are removed. Your project files are not touched. Running services must be stopped first.',
      onConfirm: async () => {
        try {
          await call('workspaces.delete', { name: current.name })
          setSel(null)
        } catch (e) {
          reportError('Could not delete workspace', e)
        }
        void list.reload()
      },
    })
  }

  const st = status.data
  const busy = running !== null

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Workspaces"
        subtitle="A workspace is a project's whole environment: services, dependencies and health."
      >
        <Button icon={<FileUp className="h-3.5 w-3.5" />} onClick={() => void importFile()}>
          Import…
        </Button>
        <Button
          variant="primary"
          icon={<Plus className="h-3.5 w-3.5" />}
          onClick={() => setEditor({ yaml: TEMPLATE, root: '', editing: false })}
        >
          New workspace
        </Button>
      </PageHeader>
      {list.loading && !list.data ? (
        <Spinner />
      ) : list.error ? (
        <ErrorState message={list.error} onRetry={() => void list.reload()} />
      ) : rows.length === 0 ? (
        <EmptyState
          title="No workspaces yet"
          hint="Define your frontend, backend, database and workers once, then start them all in the right order."
          action={
            <Button
              variant="primary"
              onClick={() => setEditor({ yaml: TEMPLATE, root: '', editing: false })}
            >
              Create your first workspace
            </Button>
          }
        />
      ) : (
        <div className="flex min-h-0 flex-1 gap-3 px-6 pb-4">
          <ul aria-label="Workspaces" className="w-60 shrink-0 space-y-1 overflow-auto">
            {rows.map((r) => (
              <li key={r.name}>
                <button
                  type="button"
                  aria-current={r.name === sel}
                  onClick={() => {
                    setSel(r.name)
                    setProgress([])
                  }}
                  className={cx(
                    'w-full rounded-lg border px-3 py-2 text-left',
                    r.name === sel
                      ? 'border-accent bg-surface'
                      : 'border-line bg-surface hover:bg-subtle',
                  )}
                >
                  <div className="flex items-center gap-2">
                    <StatusDot tone={wsTone(r.health)} />
                    <span className="font-medium">{r.name}</span>
                  </div>
                  <div className="mt-0.5 flex justify-between text-xs text-muted">
                    <span>{r.health}</span>
                    <span>
                      {r.running}/{r.total} running
                    </span>
                  </div>
                </button>
              </li>
            ))}
          </ul>
          {current && (
            <div className="flex min-w-0 flex-1 flex-col gap-3 overflow-auto">
              <Panel
                title={
                  <span className="flex items-center gap-2">
                    {current.name}{' '}
                    <Badge tone={wsTone(st?.health ?? current.health)}>
                      {st?.health ?? current.health}
                    </Badge>
                    {current.approved ? (
                      <Badge tone="ok">authorised</Badge>
                    ) : (
                      <Badge tone="warn">not authorised</Badge>
                    )}
                  </span>
                }
                actions={
                  <>
                    <Button
                      variant="primary"
                      disabled={busy}
                      icon={<Play className="h-3.5 w-3.5" />}
                      onClick={() => void start('up')}
                    >
                      {running === 'up' ? 'Starting…' : 'Start'}
                    </Button>
                    <Button
                      disabled={busy}
                      icon={<Square className="h-3.5 w-3.5" />}
                      onClick={() => void run('down')}
                    >
                      {running === 'down' ? 'Stopping…' : 'Stop'}
                    </Button>
                    <Button
                      disabled={busy}
                      icon={<RotateCw className="h-3.5 w-3.5" />}
                      onClick={() => void start('restart')}
                    >
                      Restart
                    </Button>
                  </>
                }
              >
                <div className="px-3 py-2 text-sm text-muted">
                  <div className="selectable">{current.description || 'No description.'}</div>
                  <div className="selectable mt-0.5 font-mono text-xs">{current.root}</div>
                </div>
                <table aria-label="Workspace services" className="w-full text-sm">
                  <thead>
                    <tr className="border-y border-line text-xs text-muted">
                      <th className="h-8 px-3 text-left">Order</th>
                      <th className="px-3 text-left">Service</th>
                      <th className="px-3 text-left">State</th>
                      <th className="px-3 text-left">Health</th>
                      <th className="px-3 text-left">Depends on</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(st?.services ?? []).map((s) => (
                      <tr key={s.name} className="h-8 border-b border-line/60">
                        <td className="px-3 tabular-nums">{s.layer + 1}</td>
                        <td className="px-3 font-medium">{s.name}</td>
                        <td className="px-3">
                          <Badge tone={stateTone(s.runtime?.state)}>
                            {s.runtime?.state ?? 'stopped'}
                          </Badge>
                        </td>
                        <td className="px-3">
                          <Badge tone={svcHealthTone(s.runtime?.health)}>
                            {s.runtime?.health ?? (s.spec.health ? 'unknown' : 'not monitored')}
                          </Badge>
                        </td>
                        <td className="px-3 text-muted">
                          {(s.spec.dependsOn ?? []).join(', ') || '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="flex gap-2 border-t border-line p-3">
                  <Button
                    disabled={busy}
                    icon={<Pencil className="h-3.5 w-3.5" />}
                    onClick={() =>
                      setEditor({ yaml: current.yaml, root: current.root, editing: true })
                    }
                  >
                    Edit
                  </Button>
                  <Button
                    icon={<Download className="h-3.5 w-3.5" />}
                    onClick={() => void exportYaml()}
                  >
                    Export…
                  </Button>
                  <Button
                    variant="danger"
                    disabled={busy}
                    icon={<Trash2 className="h-3.5 w-3.5" />}
                    onClick={remove}
                  >
                    Delete…
                  </Button>
                </div>
              </Panel>
              {progress.length > 0 && (
                <Panel title="Progress">
                  <ul
                    aria-label="Startup progress"
                    className="selectable divide-y divide-line text-sm"
                  >
                    {progress
                      .filter((p) => p.service)
                      .map((p, i) => (
                        <li key={i} className="flex gap-3 px-3 py-1.5">
                          <Badge
                            tone={
                              p.phase === 'failed'
                                ? 'bad'
                                : p.phase === 'ready' || p.phase === 'stopped'
                                  ? 'ok'
                                  : p.phase === 'rolled-back'
                                    ? 'warn'
                                    : 'accent'
                            }
                          >
                            {p.phase}
                          </Badge>
                          <span className="w-28 font-medium">{p.service}</span>
                          <span className="min-w-0 flex-1 truncate text-muted" title={p.detail}>
                            {p.detail}
                          </span>
                        </li>
                      ))}
                  </ul>
                </Panel>
              )}
              <Panel title="History">
                <ul className="divide-y divide-line text-sm">
                  {(hist.data ?? []).map((h, i) => (
                    <li key={i} className="flex items-center gap-3 px-3 py-1.5">
                      <span className="w-20 text-xs text-muted">{since(h.time)}</span>
                      <span className="w-16 font-medium">{h.action}</span>
                      <Badge tone={h.outcome === 'ok' ? 'ok' : 'bad'}>{h.outcome}</Badge>
                      <span className="min-w-0 flex-1 truncate text-xs text-muted" title={h.detail}>
                        {h.detail}
                      </span>
                    </li>
                  ))}
                  {(hist.data ?? []).length === 0 && (
                    <li className="p-3 text-sm text-muted">No activity yet.</li>
                  )}
                </ul>
              </Panel>
            </div>
          )}
        </div>
      )}

      {editor && (
        <WorkspaceEditor
          initialYaml={editor.yaml}
          initialRoot={editor.root}
          editing={editor.editing}
          onClose={() => setEditor(null)}
          onSaved={(name) => {
            setEditor(null)
            setSel(name)
            void list.reload()
          }}
        />
      )}
      {authorize && (
        <Modal
          title={`Authorise ${authorize.row.name} to run?`}
          width="max-w-2xl"
          onClose={() => setAuthorize(null)}
          footer={
            <>
              <Button onClick={() => setAuthorize(null)}>Cancel</Button>
              <Button
                variant="primary"
                icon={<ShieldCheck className="h-3.5 w-3.5" />}
                onClick={() => void approveAndRun()}
              >
                Authorise and {authorize.action === 'up' ? 'start' : 'restart'}
              </Button>
            </>
          }
        >
          <p className="mb-2 text-sm">
            Starting this workspace will run the following commands on this computer, as you:
          </p>
          <ul className="selectable space-y-1 rounded border border-line bg-subtle p-2 font-mono text-xs">
            {authorize.preview.commands.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-muted">
            Authorisation applies to this exact definition. If it changes, you will be asked again.
          </p>
        </Modal>
      )}
    </div>
  )
}
