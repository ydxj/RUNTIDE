import { useState } from 'react'
import { FileDown, FileSearch, FileUp, ShieldCheck } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { bytes, since } from '../../services/format'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { ReceiptInspect, ReceiptRecord, ReceiptVerification } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
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
  const [plan, setPlan] = useState<{
    class: string
    reasons: string[]
    steps: string[]
    blockers: string[]
    toolIssues: string[]
    needsEnv: string[]
  } | null>(null)
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
  const showPlan = async () => {
    try {
      setPlan(await call('receipts.plan', { ref: reference }))
    } catch (e) {
      reportError('Could not plan replay', e)
    }
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
            <b>Integrity OK</b>{' '}
            <span className="text-muted">
              {d.verification.filesChecked} files match the manifest.
            </span>
            {d.verification.signature ? (
              <Badge tone={d.verification.signature.trusted ? 'ok' : 'warn'}>
                signed {d.verification.signature.fingerprint}
                {d.verification.signature.trusted ? ' (trusted)' : ' (not trusted)'}
              </Badge>
            ) : (
              <Badge>unsigned</Badge>
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
            actions={<Button onClick={() => void showPlan()}>Replay plan…</Button>}
          >
            <List title="Why" items={d.receipt.reproduction.reasons} />
            <List title="Not captured" items={d.receipt.reproduction.missing} tone="warn" />
            {plan && (
              <div className="border-t border-line">
                <List
                  title="Steps a replay would run (only after you approve, in a new isolated worktree)"
                  items={plan.steps}
                />
                <List
                  title="Tool differences on this machine"
                  items={plan.toolIssues}
                  tone="warn"
                />
                <List title="Blocked because" items={plan.blockers} tone="bad" />
                <p className="px-3 pb-3 text-xs text-muted">
                  Run it with: runtide receipt replay &lt;file&gt; --project-dir &lt;checkout&gt;
                </p>
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
