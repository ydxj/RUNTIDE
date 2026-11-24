import { useState } from 'react'
import { FileDown, FileSearch, FileUp, ShieldCheck } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { bytes, since } from '../../services/format'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type {
  ReceiptInspect,
  ReceiptRecord,
  ReceiptVerification,
  ReplayPlan,
  ReplayResult,
} from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Panel,
  Spinner,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { Modal } from '../../components/ui/Overlays'
import { List, StateBadge, statusTone } from './common'

export function ReceiptsPage() {
  const toast = useUI((s) => s.toast)
  const go = useUI((s) => s.go)
  const { data, error, loading, reload } = usePoll(
    () => call<ReceiptRecord[]>('receipts.list'),
    [],
    0,
  )
  const [view, setView] = useState<{ ref: string; title: string } | null>(null)
  const [problems, setProblems] = useState<ReceiptVerification | null>(null)

  const importFile = async () => {
    try {
      const f = await native.pickFile('Import a Run Receipt (.runtide.zip)')
      if (!f) return
      try {
        await call('receipts.import', { ref: f })
        toast({
          kind: 'success',
          title: 'Receipt imported',
          detail: 'It was verified first; nothing from it was executed.',
        })
      } catch (e) {
        const v = await call<ReceiptVerification>('receipts.verify', { ref: f }).catch(() => null)
        if (v && !v.valid) setProblems(v)
        else reportError('Import failed', e)
      }
      void reload()
    } catch (e) {
      reportError('Could not choose file', e)
    }
  }
  const saveReport = async (r: ReceiptRecord) => {
    try {
      const html = await call<string>('receipts.report', { ref: r.id })
      const p = await native.pickSaveFile('Save HTML report', `${r.executionId}-report.html`)
      if (!p) return
      await native.writeTextFile(p, html)
      toast({ kind: 'success', title: 'Report saved', detail: p })
    } catch (e) {
      reportError('Report failed', e)
    }
  }

  const cols: Column<ReceiptRecord>[] = [
    {
      key: 'project',
      header: 'Project',
      width: '200px',
      sort: (a, b) => a.projectName.localeCompare(b.projectName),
      render: (r) => <span className="font-medium">{r.projectName}</span>,
    },
    {
      key: 'status',
      header: 'Status',
      width: '160px',
      render: (r) => <StateBadge state={r.status} />,
    },
    {
      key: 'src',
      header: 'Source',
      width: '90px',
      render: (r) => (r.imported ? 'imported' : 'exported'),
    },
    {
      key: 'sig',
      header: 'Signed by',
      width: '150px',
      render: (r) => r.signedBy ?? <span className="text-muted">unsigned</span>,
    },
    { key: 'size', header: 'Size', width: '80px', align: 'right', render: (r) => bytes(r.size) },
    {
      key: 'sha',
      header: 'SHA-256',
      width: '120px',
      mono: true,
      render: (r) => r.sha256.slice(0, 12),
    },
    { key: 'created', header: 'Added', width: '100px', render: (r) => since(r.createdAt) },
    {
      key: 'act',
      header: '',
      width: '230px',
      render: (r) => (
        <span className="flex gap-1" onClick={(e) => e.stopPropagation()}>
          <Button
            icon={<FileSearch className="h-3.5 w-3.5" />}
            onClick={() => setView({ ref: r.id, title: r.projectName })}
          >
            Inspect
          </Button>
          <Button icon={<FileDown className="h-3.5 w-3.5" />} onClick={() => void saveReport(r)}>
            Report
          </Button>
        </span>
      ),
    },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Run Receipts"
        subtitle="Portable, hash-checked evidence of executions. Importing verifies a receipt and never runs anything from it."
      >
        <Button
          icon={<FileUp className="h-3.5 w-3.5" />}
          variant="primary"
          onClick={() => void importFile()}
        >
          Import receipt…
        </Button>
        <Button onClick={() => go('compare')}>Compare…</Button>
      </PageHeader>
      <div className="mx-6 mb-4 flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        {loading && !data ? (
          <Spinner />
        ) : error ? (
          <ErrorState message={error} onRetry={() => void reload()} />
        ) : (
          <DataTable
            label="Receipts"
            columns={cols}
            rows={data ?? []}
            rowKey={(r) => r.id}
            initialSort={{ key: 'created', dir: 'desc' }}
            empty={
              <EmptyState
                title="No receipts yet"
                hint="Export one from an ended execution (Receipt tab) or import a .runtide.zip file someone shared with you."
              />
            }
          />
        )}
      </div>
      {view && (
        <InspectModal reference={view.ref} title={view.title} onClose={() => setView(null)} />
      )}
      {problems && (
        <Modal
          title="Receipt failed verification"
          onClose={() => setProblems(null)}
          footer={<Button onClick={() => setProblems(null)}>Close</Button>}
        >
          <p className="mb-2 text-sm">
            This file was not imported because its contents do not match what it claims:
          </p>
          <ul className="selectable list-disc space-y-1 pl-5 text-sm text-bad">
            {(problems.problems ?? []).map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </Modal>
      )}
    </div>
  )
}

function InspectModal({
  reference,
  title,
  onClose,
}: {
  reference: string
  title: string
  onClose: () => void
}) {
  const [state, setState] = useState<{ data?: ReceiptInspect; error?: string }>({})
  const ask = useUI((s) => s.ask)
  const go = useUI((s) => s.go)
  const [plan, setPlan] = useState<ReplayPlan | null>(null)
  const [projectDir, setProjectDir] = useState('')
  const [secrets, setSecrets] = useState<Record<string, string>>({})
  const [result, setResult] = useState<ReplayResult | null>(null)
  const [running, setRunning] = useState(false)
  usePoll(
    async () => {
      try {
        setState({ data: await call<ReceiptInspect>('receipts.inspect', { ref: reference }) })
      } catch (e) {
        setState({ error: errMsg(e) })
      }
      return null
    },
    [reference],
    0,
  )
  const d = state.data
  const showPlan = async (dir = projectDir, env = secrets) => {
    try {
      const p = await call<ReplayPlan>('receipts.plan', { ref: reference, projectDir: dir, env })
      // Defensive: lists are arrays from the engine, but never let a null crash the dialog.
      setPlan({
        ...p,
        reasons: p.reasons ?? [],
        steps: p.steps ?? [],
        needsEnv: p.needsEnv ?? [],
        toolIssues: p.toolIssues ?? [],
        missing: p.missing ?? [],
        blockers: p.blockers ?? [],
        dependencies: p.dependencies ?? [],
        destructive: p.destructive ?? [],
        permissions: p.permissions ?? [],
      })
    } catch (e) {
      reportError('Could not plan replay', e)
    }
  }
  const browse = async () => {
    const p = await native.pickDirectory('Choose a git checkout that contains the recorded commit')
    if (p) {
      setProjectDir(p)
      await showPlan(p)
    }
  }
  const runReplay = () => {
    if (!plan) return
    ask({
      title: 'Run this replay?',
      confirmLabel: 'Approve and run replay',
      danger: plan.destructive.length > 0,
      body: (
        <div className="space-y-2 text-sm">
          <p>
            RUNTIDE will execute {plan.steps.length} recorded step(s) in a new git worktree of{' '}
            <code>{projectDir}</code>. Nothing runs until you approve.
          </p>
          {plan.destructive.length > 0 && (
            <div className="rounded border border-bad/40 bg-bad/10 p-2 text-bad">
              <b>Potentially destructive operations (heuristic):</b>
              <ul className="mt-1 list-disc pl-5">
                {plan.destructive.map((x) => (
                  <li key={x} className="break-all font-mono text-xs">
                    {x}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <ul className="list-disc pl-5 text-muted">
            {plan.permissions.map((x) => (
              <li key={x}>{x}</li>
            ))}
          </ul>
        </div>
      ),
      onConfirm: async () => {
        setRunning(true)
        setResult(null)
        try {
          setResult(
            await call<ReplayResult>('receipts.replay', {
              ref: reference,
              projectDir,
              env: secrets,
              approve: true,
            }),
          )
        } catch (e) {
          reportError('Replay failed', e)
        } finally {
          setRunning(false)
        }
      },
    })
  }
  return (
    <Modal title={`Receipt: ${title}`} width="max-w-4xl" onClose={onClose}>
      {state.error && (
        <div
          role="alert"
          className="selectable whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {state.error}
        </div>
      )}
      {!d && !state.error && <Spinner />}
      {d && (
        <div className="space-y-3 text-sm">
          <div className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-ok" />
            <b>Integrity {d.verification.integrity}</b>{' '}
            <span className="text-muted">
              {d.verification.filesChecked} files match the manifest.
            </span>
            {d.verification.signatureState === 'verified' && d.verification.signature && (
              <Badge tone={d.verification.signature.trusted ? 'ok' : 'warn'}>
                signature verified · key {d.verification.signature.fingerprint}
                {d.verification.signature.trusted ? ' (trusted)' : ' (not in your trusted list)'}
              </Badge>
            )}
            {d.verification.signatureState === 'unavailable' && (
              <Badge>signature unavailable (unsigned)</Badge>
            )}
            {d.verification.signatureState === 'invalid' && (
              <Badge tone="bad">signature invalid</Badge>
            )}
          </div>
          <p className="text-xs text-muted">{d.verification.authenticity}</p>
          <Panel title="Execution">
            <dl className="selectable grid grid-cols-3 gap-3 p-3">
              <div>
                <dt className="text-xs text-muted">Task</dt>
                <dd>{d.receipt.manifest.task}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Status</dt>
                <dd>
                  <StateBadge state={d.receipt.manifest.status} /> verification{' '}
                  {d.receipt.manifest.verification || 'none'}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Created</dt>
                <dd>
                  {new Date(d.receipt.manifest.createdAt).toLocaleString()} · RUNTIDE{' '}
                  {d.receipt.manifest.runtideVersion}
                </dd>
              </div>
            </dl>
          </Panel>
          <Panel
            title={`Reproducibility: ${d.receipt.reproduction.class.replaceAll('_', ' ')}`}
            actions={<Button onClick={() => void showPlan()}>Replay pre-flight…</Button>}
          >
            <List title="Why" items={d.receipt.reproduction.reasons} />
            <List title="Not captured" items={d.receipt.reproduction.missing} tone="warn" />
            {plan && (
              <div className="border-t border-line">
                <div className="flex items-center gap-2 px-3 py-2 text-sm">
                  <span className="text-xs font-semibold text-muted">Source checkout</span>
                  <Input
                    aria-label="Project directory"
                    className="flex-1"
                    placeholder="Git checkout that contains the recorded commit"
                    value={projectDir}
                    onChange={(e) => setProjectDir(e.target.value)}
                    onBlur={() => void showPlan()}
                  />
                  <Button onClick={() => void browse()}>Browse…</Button>
                </div>
                {plan.dependencies.length > 0 && (
                  <div className="px-3 pb-2">
                    <div className="mb-1 text-xs font-semibold text-muted">Dependencies</div>
                    <ul className="space-y-0.5 text-sm">
                      {plan.dependencies.map((x) => (
                        <li key={x.kind + x.name} className="flex items-center gap-2">
                          <Badge
                            tone={
                              x.status === 'ok' ? 'ok' : x.status === 'different' ? 'warn' : 'bad'
                            }
                          >
                            {x.status}
                          </Badge>
                          <span className="font-medium">{x.name}</span>
                          <span className="text-muted">
                            needs {x.required}
                            {x.available && `, here ${x.available}`}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {plan.needsEnv.length > 0 && (
                  <div className="space-y-1 px-3 pb-2">
                    <div className="text-xs font-semibold text-muted">
                      Secret variables withheld from the receipt
                    </div>
                    {plan.needsEnv.map((k) => (
                      <label key={k} className="flex items-center gap-2 text-sm">
                        <span className="w-48 font-mono text-xs">{k}</span>
                        <Input
                          type="password"
                          aria-label={`Value for ${k}`}
                          value={secrets[k] ?? ''}
                          onChange={(e) => setSecrets({ ...secrets, [k]: e.target.value })}
                          onBlur={() => void showPlan()}
                        />
                      </label>
                    ))}
                  </div>
                )}
                <List
                  title="Steps a replay would run (only after you approve, in a new isolated worktree)"
                  items={plan.steps}
                />
                <List
                  title="Potentially destructive operations (heuristic, not a safety guarantee)"
                  items={plan.destructive}
                  tone="bad"
                />
                <List title="Permissions this replay needs" items={plan.permissions} />
                <List title="Blocked because" items={plan.blockers} tone="bad" />
                <div className="flex items-center gap-2 px-3 pb-3">
                  <Button
                    variant="primary"
                    disabled={plan.blockers.length > 0 || running}
                    onClick={runReplay}
                  >
                    {running ? 'Replaying…' : 'Review and run replay…'}
                  </Button>
                  <span className="text-xs text-muted">
                    Metadata-only and blocked receipts cannot be replayed.
                  </span>
                </div>
                {result && (
                  <div className="border-t border-line px-3 py-2 text-sm">
                    <div className="flex items-center gap-2">
                      <Badge
                        tone={
                          result.outcome === 'reproduced'
                            ? 'ok'
                            : result.outcome === 'partially_reproduced'
                              ? 'warn'
                              : 'bad'
                        }
                      >
                        {result.outcome.replaceAll('_', ' ')}
                      </Badge>
                      <span className="text-muted">
                        replay execution {result.executionId} finished {result.state}
                      </span>
                    </div>
                    <List title="Why" items={result.outcomeReasons} />
                    {result.executionId && (
                      <Button onClick={() => go('executions', { id: result.executionId })}>
                        Open replay execution
                      </Button>
                    )}
                  </div>
                )}
              </div>
            )}
          </Panel>
          <Panel title="Verification results">
            <ul className="divide-y divide-line">
              {d.receipt.verification.results.map((v) => (
                <li key={v.name} className="flex items-center gap-2 px-3 py-1.5">
                  <Badge tone={statusTone(v.status)}>{v.status.replace('_', ' ')}</Badge>
                  <span className="font-medium">{v.name}</span>
                  <span className="selectable truncate text-muted">{v.error}</span>
                </li>
              ))}
            </ul>
            {d.receipt.verification.results.length === 0 && (
              <div className="p-3 text-muted">No verification results in this receipt.</div>
            )}
          </Panel>
          <Panel
            title={`Commands (${d.receipt.commands.length}) · file changes (${d.receipt.fileChanges.length}) · events (${d.receipt.events.length})`}
          >
            <ul className="selectable divide-y divide-line font-mono text-xs">
              {d.receipt.commands.map((c) => (
                <li key={c.id} className="px-3 py-1.5">
                  {c.kind}:{c.name} → exit {c.exitCode ?? '?'}{' '}
                  {c.redacted && <span className="text-warn">(args masked)</span>}
                </li>
              ))}
            </ul>
          </Panel>
          {(d.verification.warnings ?? []).map((w) => (
            <div key={w} className="rounded border border-warn/40 bg-warn/10 p-2">
              {w}
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}
