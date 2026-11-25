import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeft,
  Ban,
  CheckCheck,
  Download,
  FileText,
  SkipForward,
  Stethoscope,
} from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { useEngineEvent } from '../../hooks/useEngineEvent'
import { call, native } from '../../services/api'
import { clock, bytes, joinArgs } from '../../services/format'
import { reportError, useUI } from '../../stores/ui'
import type {
  ExecCommand,
  ExecEvent,
  ExecutionDetail,
  OutputLine,
  ReceiptRecord,
  VerificationProfile,
} from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Input,
  Panel,
  Select,
  Spinner,
  cx,
} from '../../components/ui/primitives'
import { ISOLATION, ISOLATION_HELP, StateBadge, ms, statusTone, verdictTone } from './common'

const TABS = [
  'Overview',
  'Timeline',
  'Commands',
  'Logs',
  'Processes',
  'Services',
  'File Changes',
  'Verification',
  'Environment',
  'Receipt',
] as const
type Tab = (typeof TABS)[number]

export function ExecutionDetailView({ id, onBack }: { id: string; onBack: () => void }) {
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const [tab, setTab] = useState<Tab>('Overview')
  const [profile, setProfile] = useState('')
  const profiles = usePoll(() => call<VerificationProfile[]>('profiles.list'), [], 0)
  const [focus, setFocus] = useState<{ commandId?: string }>({})
  const d = usePoll(() => call<ExecutionDetail>('runs.get', { id }), [id], 2)
  useEngineEvent('run', () => void d.reload())

  const x = d.data?.execution
  const act = async (method: string, extra: Record<string, unknown>, ok: string) => {
    try {
      await call(method, { id, ...extra })
      toast({ kind: 'success', title: ok })
    } catch (e) {
      reportError('Action failed', e)
    } finally {
      void d.reload()
    }
  }

  if (d.error && !d.data) return <ErrorState message={d.error} onRetry={() => void d.reload()} />
  if (!d.data || !x) return <Spinner label="Loading execution" />
  const running = x.state === 'RUNNING'
  const live = ['CONFIGURING', 'READY', 'RUNNING', 'VERIFYING'].includes(x.state)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-start justify-between gap-4 px-6 pt-4 pb-2">
        <div className="min-w-0">
          <button
            type="button"
            onClick={onBack}
            className="mb-1 flex items-center gap-1 text-xs text-accent"
          >
            <ArrowLeft className="h-3 w-3" /> Executions
          </button>
          <h1 className="truncate text-lg font-semibold leading-6">{x.task || 'Execution'}</h1>
          <div className="selectable flex flex-wrap items-center gap-2 text-sm text-muted">
            <StateBadge state={x.state} />
            {x.verification && (
              <Badge tone={verdictTone(x.verification)}>verification {x.verification}</Badge>
            )}
            <span className="font-mono text-xs">{x.id}</span>
            <span>
              {d.data.workspace?.name} · {ISOLATION[x.isolation]} · {ms(d.data.durationMs)}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          {running && (profiles.data ?? []).length > 0 && (
            <Select
              aria-label="Verification profile"
              value={profile}
              onChange={(e) => setProfile(e.target.value)}
            >
              <option value="">Workspace / project default</option>
              {(profiles.data ?? []).map((p) => (
                <option key={p.id} value={p.name}>
                  Profile: {p.name}
                </option>
              ))}
            </Select>
          )}
          {running && (
            <Button
              variant="primary"
              icon={<Stethoscope className="h-3.5 w-3.5" />}
              onClick={() => void act('runs.verify', { profile }, 'Verification finished')}
            >
              Verify
            </Button>
          )}
          {running && (
            <Button
              icon={<CheckCheck className="h-3.5 w-3.5" />}
              onClick={() =>
                ask({
                  title: 'Finish without verification?',
                  confirmLabel: 'Finish',
                  body: 'The execution is marked COMPLETED with verification "none". No acceptance checks are run.',
                  onConfirm: () => act('runs.finish', {}, 'Execution finished'),
                })
              }
            >
              Finish
            </Button>
          )}
          {live && (
            <Button
              variant="danger"
              icon={<Ban className="h-3.5 w-3.5" />}
              onClick={() =>
                ask({
                  title: 'Cancel this execution?',
                  danger: true,
                  confirmLabel: 'Cancel execution',
                  body: 'Running commands are interrupted and every service it started is stopped. Evidence recorded so far is kept.',
                  onConfirm: () =>
                    act(
                      'runs.cancel',
                      { reason: 'cancelled in the desktop app' },
                      'Execution cancelled',
                    ),
                })
              }
            >
              Cancel
            </Button>
          )}
        </div>
      </div>
      {x.error && (
        <div
          role="alert"
          className="selectable mx-6 mb-2 rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {x.error}
        </div>
      )}
      <div
        role="tablist"
        aria-label="Execution sections"
        className="flex gap-1 overflow-x-auto px-6 pb-2"
      >
        {TABS.map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={cx(
              'h-8 shrink-0 rounded-md px-3 text-sm',
              tab === t
                ? 'bg-surface font-medium shadow-[0_0_0_1px_rgb(var(--line))]'
                : 'hover:bg-surface/60',
            )}
          >
            {t}
          </button>
        ))}
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto px-6 pb-4">
        {tab === 'Overview' && <OverviewTab d={d.data} />}
        {tab === 'Timeline' && (
          <TimelineTab
            id={id}
            live={live}
            onOpenCommand={(cid) => {
              setFocus({ commandId: cid })
              setTab('Logs')
            }}
          />
        )}
        {tab === 'Commands' && (
          <CommandsTab
            d={d.data}
            onLogs={(cid) => {
              setFocus({ commandId: cid })
              setTab('Logs')
            }}
          />
        )}
        {tab === 'Logs' && <LogsTab d={d.data} live={live} initial={focus.commandId} />}
        {tab === 'Processes' && <ProcessesTab d={d.data} />}
        {tab === 'Services' && <ServicesTab d={d.data} />}
        {tab === 'File Changes' && <FilesTab d={d.data} />}
        {tab === 'Verification' && <VerificationTab d={d.data} />}
        {tab === 'Environment' && <EnvironmentTab d={d.data} />}
        {tab === 'Receipt' && <ReceiptTab d={d.data} />}
      </div>
    </div>
  )
}

function Dl({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted">{k}</dt>
      <dd className="selectable text-sm">{v}</dd>
    </div>
  )
}

function OverviewTab({ d }: { d: ExecutionDetail }) {
  const x = d.execution
  const failed = d.commands.filter((c) => c.exitCode != null && c.exitCode !== 0)
  return (
    <div className="grid gap-3">
      <Panel title="Summary">
        <dl className="grid grid-cols-3 gap-3 p-3">
          <Dl k="State" v={x.state} />
          <Dl k="Started" v={x.startedAt ? new Date(x.startedAt).toLocaleString() : '-'} />
          <Dl
            k="Ended"
            v={x.endedAt ? new Date(x.endedAt).toLocaleString() : d.active ? 'still running' : '-'}
          />
          <Dl k="Commands" v={`${d.commands.length} (${failed.length} failed)`} />
          <Dl k="Events recorded" v={d.eventCount} />
          <Dl k="File changes" v={d.fileChanges.length} />
          <Dl
            k="Verification"
            v={d.verificationSummary.overall === 'none' ? 'not run' : d.verificationSummary.overall}
          />
          <Dl k="RUNTIDE" v={x.runtideVersion} />
          <Dl
            k="Isolation"
            v={`${ISOLATION[x.isolation]}: ${ISOLATION_HELP[ISOLATION[x.isolation] ?? 'managed']}`}
          />
        </dl>
      </Panel>
      {(d.verificationSummary.reasons ?? []).length > 0 && (
        <Panel title="Why verification failed">
          <ul className="selectable list-disc space-y-1 p-3 pl-7 text-sm">
            {d.verificationSummary.reasons?.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </Panel>
      )}
      <p className="text-xs text-muted">
        What RUNTIDE recorded here was directly observed by RUNTIDE (commands it launched, their
        output and exit codes, services it supervised, file changes in the workspace). It does not
        see what other programs did.
      </p>
    </div>
  )
}

function TimelineTab({
  id,
  live,
  onOpenCommand,
}: {
  id: string
  live: boolean
  onOpenCommand: (commandId: string) => void
}) {
  const [q, setQ] = useState('')
  const [type, setType] = useState('')
  const [sev, setSev] = useState('')
  const [subject, setSubject] = useState('')
  const [sel, setSel] = useState<ExecEvent | null>(null)
  const toast = useUI((s) => s.toast)
  const evs = usePoll(
    () =>
      call<ExecEvent[]>('runs.events', {
        id,
        types: type ? [type] : [],
        severity: sev,
        search: q,
        subject,
        limit: 5000,
      }),
    [id, type, sev, q, subject],
    live ? 2 : 0,
  )
  const all = useMemo(() => evs.data ?? [], [evs.data])
  const types = useMemo(() => Array.from(new Set(all.map((e) => e.type))).sort(), [all])
  const subjects = useMemo(
    () => Array.from(new Set(all.map((e) => e.subject).filter(Boolean))).sort() as string[],
    [all],
  )
  const list = useRef<HTMLUListElement>(null)
  const nextFailure = () => {
    const errs = all.filter((e) => e.severity === 'error')
    if (errs.length === 0) return
    const i = errs.findIndex((e) => e.seq === sel?.seq)
    const n = errs[(i + 1) % errs.length]
    if (n) {
      setSel(n)
      document.getElementById(`ev-${n.seq}`)?.scrollIntoView({ block: 'center' })
    }
  }
  const exportSel = async () => {
    try {
      const p = await native.pickSaveFile('Export events', `events-${id}.json`)
      if (!p) return
      await native.writeTextFile(p, JSON.stringify(sel ? [sel] : all, null, 2))
      toast({
        kind: 'success',
        title: sel ? 'Event exported' : `${all.length} events exported`,
        detail: p,
      })
    } catch (e) {
      reportError('Export failed', e)
    }
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search events"
          placeholder="Search events"
          className="w-56"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <Select aria-label="Event type" value={type} onChange={(e) => setType(e.target.value)}>
          <option value="">All types</option>
          {types.map((t) => (
            <option key={t}>{t}</option>
          ))}
        </Select>
        <Select
          aria-label="Service or command"
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
        >
          <option value="">All services/commands</option>
          {subjects.map((t) => (
            <option key={t}>{t}</option>
          ))}
        </Select>
        <Select aria-label="Severity" value={sev} onChange={(e) => setSev(e.target.value)}>
          <option value="">All severities</option>
          <option>info</option>
          <option>warn</option>
          <option>error</option>
        </Select>
        <Button icon={<SkipForward className="h-3.5 w-3.5" />} onClick={nextFailure}>
          Next failure
        </Button>
        <Button icon={<Download className="h-3.5 w-3.5" />} onClick={() => void exportSel()}>
          {sel ? 'Export selected…' : 'Export all…'}
        </Button>
        <span className="text-xs text-muted">{all.length} events</span>
      </div>
      <div className="flex min-h-0 flex-1 gap-3">
        <ul
          ref={list}
          aria-label="Timeline"
          className="min-h-0 flex-1 overflow-auto rounded-lg border border-line bg-surface"
        >
          {all.length === 0 && (
            <li>
              <EmptyState title="No events match" />
            </li>
          )}
          {all.map((e) => (
            <li key={e.id} id={`ev-${e.seq}`}>
              <button
                type="button"
                onClick={() => setSel(e)}
                className={cx(
                  'flex w-full items-center gap-3 border-b border-line/60 px-3 py-1.5 text-left text-sm hover:bg-subtle',
                  sel?.seq === e.seq && 'bg-accent/10',
                )}
              >
                <span className="w-10 text-xs text-muted tabular-nums">{e.seq}</span>
                <span className="w-24 font-mono text-xs text-muted">{clock(e.time)}</span>
                <span
                  className={cx(
                    'w-44 truncate font-mono text-xs',
                    e.severity === 'error' && 'text-bad',
                    e.severity === 'warn' && 'text-warn',
                  )}
                >
                  {e.type}
                </span>
                <span className="min-w-0 flex-1 truncate">{e.subject}</span>
                <span className="text-xs text-muted">{e.source}</span>
              </button>
            </li>
          ))}
        </ul>
        {sel && (
          <aside
            aria-label="Event details"
            className="selectable w-80 shrink-0 overflow-auto rounded-lg border border-line bg-surface p-3 text-sm"
          >
            <div className="font-semibold">{sel.type}</div>
            <div className="text-xs text-muted">
              #{sel.seq} · {new Date(sel.time).toLocaleString()} · {sel.source}
            </div>
            <pre className="mt-2 whitespace-pre-wrap break-words rounded bg-subtle p-2 font-mono text-xs">
              {JSON.stringify(sel.data ?? {}, null, 2)}
            </pre>
            {typeof sel.data?.['commandId'] === 'string' && (
              <Button
                className="mt-2"
                icon={<FileText className="h-3.5 w-3.5" />}
                onClick={() => onOpenCommand(String(sel.data?.['commandId']))}
              >
                Open command output
              </Button>
            )}
          </aside>
        )}
      </div>
    </div>
  )
}

function CommandsTab({ d, onLogs }: { d: ExecutionDetail; onLogs: (id: string) => void }) {
  return (
    <Panel title={`Commands (${d.commands.length})`}>
      <table aria-label="Commands" className="w-full text-sm">
        <thead>
          <tr className="border-b border-line text-xs text-muted">
            <th className="h-8 px-3 text-left">#</th>
            <th className="px-3 text-left">Kind</th>
            <th className="px-3 text-left">Command</th>
            <th className="px-3 text-left">Exit</th>
            <th className="px-3 text-left">Output</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {d.commands.map((c) => (
            <tr key={c.id} className="border-b border-line/60 align-top">
              <td className="px-3 py-1.5 tabular-nums">{c.seq}</td>
              <td className="px-3 py-1.5">
                <Badge>{c.kind}</Badge>
              </td>
              <td className="selectable px-3 py-1.5">
                <div className="font-medium">{c.name}</div>
                <div className="break-all font-mono text-xs text-muted">
                  {c.executable} {joinArgs(c.args)}
                </div>
                <div className="text-xs text-muted">
                  in {c.cwd}
                  {c.redacted && <span className="ml-2 text-warn">args masked</span>}
                </div>
              </td>
              <td className="px-3 py-1.5">
                {c.exitCode == null ? (
                  c.endedAt ? (
                    '-'
                  ) : (
                    'running'
                  )
                ) : (
                  <Badge tone={c.exitCode === 0 ? 'ok' : 'bad'}>{c.exitCode}</Badge>
                )}
                {c.cancelled && <Badge tone="warn">cancelled</Badge>}
                {c.timedOut && <Badge tone="bad">timed out</Badge>}
                {c.error && <div className="text-xs text-bad">{c.error}</div>}
              </td>
              <td className="px-3 py-1.5 text-xs text-muted">
                {bytes(c.stdoutBytes)} out · {bytes(c.stderrBytes)} err
                {c.truncated && <span className="text-warn"> (truncated)</span>}
              </td>
              <td className="px-3 py-1.5">
                <Button onClick={() => onLogs(c.id)}>Output</Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </Panel>
  )
}

function LogsTab({ d, live, initial }: { d: ExecutionDetail; live: boolean; initial?: string }) {
  const [cid, setCid] = useState(initial ?? d.commands[0]?.id ?? '')
  const [lines, setLines] = useState<OutputLine[]>([])
  const [follow, setFollow] = useState(true)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!cid) return
    call<OutputLine[]>('runs.output', { commandId: cid, limit: 5000 })
      .then((l) => setLines(l ?? []))
      .catch((e) => reportError('Could not load output', e))
  }, [cid])
  useEngineEvent<{ commandId: string; stream: 'stdout' | 'stderr'; line: string; time: string }>(
    'exec.output',
    (o) => {
      if (o.commandId !== cid || !live) return
      setLines((l) =>
        [
          ...l,
          {
            id: Date.now() + Math.random(),
            commandId: cid,
            time: o.time,
            stream: o.stream,
            line: o.line,
          },
        ].slice(-5000),
      )
    },
  )
  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [lines, follow])
  const cmd = d.commands.find((c) => c.id === cid)
  if (d.commands.length === 0) return <EmptyState title="No commands ran" />
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Select aria-label="Command" value={cid} onChange={(e) => setCid(e.target.value)}>
          {d.commands.map((c) => (
            <option key={c.id} value={c.id}>
              {c.seq}. {c.name} ({c.kind})
            </option>
          ))}
        </Select>
        <label className="flex items-center gap-1 text-sm">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} />{' '}
          Follow
        </label>
        {cmd?.truncated && (
          <Badge tone="warn">
            output exceeded the storage limit: only the first part is stored
          </Badge>
        )}
        <span className="text-xs text-muted">Secrets are redacted before storage.</span>
      </div>
      <div
        ref={box}
        role="log"
        aria-label="Command output"
        className="selectable min-h-0 flex-1 overflow-auto rounded-lg border border-line bg-surface p-2 font-mono text-xs leading-5"
      >
        {lines.length === 0 && (
          <div className="p-4 text-center text-muted">No output recorded for this command.</div>
        )}
        {lines.map((l) => (
          <div
            key={l.id}
            className={cx('whitespace-pre-wrap break-words', l.stream === 'stderr' && 'text-warn')}
          >
            <span className="mr-2 text-muted">{clock(l.time)}</span>
            {l.line}
          </div>
        ))}
      </div>
    </div>
  )
}

function ProcessesTab({ d }: { d: ExecutionDetail }) {
  return (
    <Panel title="Processes launched by RUNTIDE">
      <table aria-label="Managed processes" className="w-full text-sm">
        <thead>
          <tr className="border-b border-line text-xs text-muted">
            <th className="h-8 px-3 text-left">PID</th>
            <th className="px-3 text-left">Executable</th>
            <th className="px-3 text-left">Started</th>
            <th className="px-3 text-left">Ended</th>
          </tr>
        </thead>
        <tbody>
          {d.processes.map((p) => (
            <tr key={p.commandId + p.pid} className="h-8 border-b border-line/60">
              <td className="px-3 tabular-nums">{p.pid}</td>
              <td className="px-3">{p.exe}</td>
              <td className="px-3">{new Date(p.startedAt).toLocaleTimeString()}</td>
              <td className="px-3">
                {p.endedAt ? (
                  new Date(p.endedAt).toLocaleTimeString()
                ) : (
                  <Badge tone="warn">no end recorded</Badge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {d.processes.length === 0 && (
        <div className="p-4 text-sm text-muted">No processes recorded.</div>
      )}
      <p className="border-t border-line p-3 text-xs text-muted">
        Only processes RUNTIDE started are listed. Child processes inside the Job Object are cleaned
        up with their parent but are not enumerated individually.
      </p>
    </Panel>
  )
}

function ServicesTab({ d }: { d: ExecutionDetail }) {
  return (
    <Panel title="Services">
      <table aria-label="Services" className="w-full text-sm">
        <thead>
          <tr className="border-b border-line text-xs text-muted">
            <th className="h-8 px-3 text-left">Service</th>
            <th className="px-3 text-left">Last state</th>
            <th className="px-3 text-left">Health</th>
            <th className="px-3 text-left">Restarts</th>
            <th className="px-3 text-left">Detail</th>
          </tr>
        </thead>
        <tbody>
          {d.services.map((s) => (
            <tr key={s.name} className="h-8 border-b border-line/60">
              <td className="px-3 font-medium">{s.name}</td>
              <td className="px-3">{s.lastState}</td>
              <td className="px-3">
                <Badge
                  tone={
                    s.health === 'healthy' ? 'ok' : s.health === 'unhealthy' ? 'bad' : 'neutral'
                  }
                >
                  {s.health}
                </Badge>
              </td>
              <td className="px-3">{s.restarts}</td>
              <td className="selectable px-3 text-muted">{s.healthDetail}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {d.services.length === 0 && (
        <div className="p-4 text-sm text-muted">This execution defined no services.</div>
      )}
    </Panel>
  )
}

function FilesTab({ d }: { d: ExecutionDetail }) {
  const [diff, setDiff] = useState<{ path: string; diff: string } | null>(null)
  const open = async (path: string) => {
    try {
      setDiff(await call<{ path: string; diff: string }>('runs.diff', { id: d.execution.id, path }))
    } catch (e) {
      reportError('No diff available', e)
    }
  }
  return (
    <div className="grid gap-3">
      <Panel title={`File changes (${d.fileChanges.length})`}>
        <table aria-label="File changes" className="w-full text-sm">
          <thead>
            <tr className="border-b border-line text-xs text-muted">
              <th className="h-8 px-3 text-left">Change</th>
              <th className="px-3 text-left">Path</th>
              <th className="px-3 text-right">Size</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {d.fileChanges.map((c) => (
              <tr key={c.path} className="h-8 border-b border-line/60">
                <td className="px-3">
                  <Badge
                    tone={c.change === 'deleted' ? 'bad' : c.change === 'added' ? 'ok' : 'accent'}
                  >
                    {c.change}
                  </Badge>
                </td>
                <td className="selectable px-3 font-mono text-xs">
                  {c.oldPath ? `${c.oldPath} → ` : ''}
                  {c.path}
                </td>
                <td className="px-3 text-right tabular-nums">
                  {bytes(c.sizeBefore)} → {bytes(c.sizeAfter)}
                </td>
                <td className="px-3 text-right">
                  {c.change === 'modified' && (
                    <Button onClick={() => void open(c.path)}>Diff</Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {d.fileChanges.length === 0 && (
          <div className="p-4 text-sm text-muted">No file changes recorded.</div>
        )}
        <p className="border-t border-line p-3 text-xs text-muted">
          Respects .gitignore. Secret files (.env, keys, credentials), dependency folders and build
          output are never recorded.
        </p>
      </Panel>
      {diff && (
        <Panel
          title={`Diff: ${diff.path}`}
          actions={
            <Button variant="ghost" onClick={() => setDiff(null)}>
              Close
            </Button>
          }
        >
          <pre
            aria-label="Diff"
            className="selectable max-h-96 overflow-auto p-3 font-mono text-xs leading-5"
          >
            {diff.diff.split('\n').map((l, i) => (
              <div
                key={i}
                className={
                  l.startsWith('+')
                    ? 'text-ok'
                    : l.startsWith('-')
                      ? 'text-bad'
                      : l.startsWith('@@')
                        ? 'text-accent'
                        : ''
                }
              >
                {l || ' '}
              </div>
            ))}
          </pre>
        </Panel>
      )}
    </div>
  )
}

function VerificationTab({ d }: { d: ExecutionDetail }) {
  const s = d.verificationSummary
  if (d.verification.length === 0)
    return (
      <EmptyState
        title="Verification has not run"
        hint="Use Verify to run the workspace's acceptance checks. RUNTIDE never marks a task verified without executing them."
      />
    )
  return (
    <div className="grid gap-3">
      <div className="flex items-center gap-2 text-sm">
        <Badge tone={verdictTone(s.overall)}>{s.overall}</Badge>
        {Object.entries(s.counts).map(([k, v]) => (
          <span key={k} className="text-muted">
            {v} {k.replace('_', ' ')}
          </span>
        ))}
      </div>
      {(s.reasons ?? []).map((r) => (
        <div
          key={r}
          className="selectable rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {r}
        </div>
      ))}
      <Panel title="Checks">
        <ul className="divide-y divide-line">
          {d.verification.map((v) => (
            <li key={v.name} className="p-3 text-sm">
              <div className="flex items-center gap-2">
                <Badge tone={statusTone(v.status)}>{v.status.replace('_', ' ')}</Badge>
                <span className="font-medium">{v.name}</span>
                {!v.required && <Badge>optional</Badge>}
                <span className="ml-auto text-xs text-muted">{ms(v.durationMs)}</span>
              </div>
              {v.error && <div className="selectable mt-1 text-bad">{v.error}</div>}
              {(v.needs ?? []).length > 0 && (
                <div className="mt-0.5 text-xs text-muted">needs: {v.needs?.join(', ')}</div>
              )}
              {v.output && (
                <pre className="selectable mt-1 max-h-40 overflow-auto rounded bg-subtle p-2 font-mono text-xs">
                  {v.output}
                </pre>
              )}
              {(v.evidence ?? []).length > 0 && (
                <div className="mt-0.5 break-all font-mono text-xs text-muted">
                  evidence: {v.evidence?.join(', ')}
                </div>
              )}
            </li>
          ))}
        </ul>
      </Panel>
    </div>
  )
}

function EnvironmentTab({ d }: { d: ExecutionDetail }) {
  const e = d.environment
  return (
    <div className="grid gap-3">
      <Panel title="Machine">
        <dl className="grid grid-cols-3 gap-3 p-3">
          <Dl k="OS" v={`${e.os ?? '?'}/${e.arch ?? '?'}`} />
          <Dl k="Isolation" v={e.isolation ?? '-'} />
          <Dl
            k="Git"
            v={
              e.git?.head
                ? `${e.git.head.slice(0, 10)}${e.git.dirty ? ' (uncommitted changes)' : ''}`
                : 'not a git repository'
            }
          />
        </dl>
      </Panel>
      <Panel title="Tools at start">
        <table aria-label="Recorded tools" className="w-full text-sm">
          <tbody>
            {(e.tools ?? []).map((t) => (
              <tr key={t.id} className="h-8 border-b border-line/60">
                <td className="px-3 font-medium">{t.name}</td>
                <td className="px-3">{t.version}</td>
                <td className="px-3 text-muted">{t.onPath ? 'on PATH' : 'not on PATH'}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {(e.tools ?? []).length === 0 && (
          <div className="p-4 text-sm text-muted">No tool versions recorded.</div>
        )}
      </Panel>
      <Panel title="Environment variables set by the workspace">
        <p className="selectable p-3 font-mono text-xs">
          {(e.envKeys ?? []).join(', ') || '(none)'}{' '}
          <span className="font-sans text-muted">- names only; values are never recorded.</span>
        </p>
      </Panel>
    </div>
  )
}

function ReceiptTab({ d }: { d: ExecutionDetail }) {
  const toast = useUI((s) => s.toast)
  const go = useUI((s) => s.go)
  const [diffs, setDiffs] = useState(false)
  const [sign, setSign] = useState(false)
  const [done, setDone] = useState<{
    path: string
    manifest: { reproducibility: string; warnings?: string[] }
  } | null>(null)
  const ended =
    d.execution.state !== 'RUNNING' &&
    ['COMPLETED', 'FAILED', 'CANCELLED', 'INTERRUPTED', 'REQUIRES_ATTENTION'].includes(
      d.execution.state,
    )
  const exportIt = async () => {
    try {
      const target = await native.pickSaveFile(
        'Export Run Receipt',
        `${d.execution.id}.runtide.zip`,
      )
      const r = await call<{
        path: string
        manifest: { reproducibility: string; warnings?: string[] }
      }>('receipts.export', { id: d.execution.id, path: target || '', diffs, sign })
      setDone(r)
      toast({ kind: 'success', title: 'Receipt exported', detail: r.path })
    } catch (e) {
      reportError('Export failed', e)
    }
  }
  if (!ended)
    return (
      <EmptyState
        title="Receipts are exported after an execution ends"
        hint="Finish, verify or cancel the execution first."
      />
    )
  return (
    <Panel title="Run Receipt">
      <div className="space-y-3 p-3 text-sm">
        <p>
          A receipt is a portable, hash-checked archive of this execution's evidence. It lists every
          file with its SHA-256. Hashes detect changes; they do not prove who made the receipt
          unless it is signed with a key you trust.
        </p>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={diffs} onChange={(e) => setDiffs(e.target.checked)} />{' '}
          Include text diffs of modified files{' '}
          <span className="text-warn">(may contain code or secrets)</span>
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={sign} onChange={(e) => setSign(e.target.checked)} /> Sign
          with my receipt key (create one under Integrations)
        </label>
        <div className="flex gap-2">
          <Button
            variant="primary"
            icon={<Download className="h-3.5 w-3.5" />}
            onClick={() => void exportIt()}
          >
            Export receipt…
          </Button>
          <Button onClick={() => go('receipts')}>Open Receipts</Button>
        </div>
        {done && (
          <div className="selectable rounded border border-ok/40 bg-ok/10 p-2">
            Saved to <span className="font-mono text-xs">{done.path}</span>. Reproducibility:{' '}
            <b>{done.manifest.reproducibility.replaceAll('_', ' ')}</b>.
            {(done.manifest.warnings ?? []).map((w) => (
              <div key={w} className="text-warn">
                {w}
              </div>
            ))}
          </div>
        )}
      </div>
    </Panel>
  )
}

export type { ExecCommand, ReceiptRecord }
