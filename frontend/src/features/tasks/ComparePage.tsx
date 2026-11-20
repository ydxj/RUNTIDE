import { useState } from 'react'
import { GitCompare } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { errMsg, useUI } from '../../stores/ui'
import type { Comparison, Execution, ReceiptRecord } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  PageHeader,
  Panel,
  Select,
  Spinner,
} from '../../components/ui/primitives'
import { List, StateBadge, ms, statusTone, verdictTone } from './common'

interface Option {
  value: string
  label: string
}

/** Visual comparison of two executions or receipts. */
export function ComparePage() {
  const params = useUI((s) => s.pageParams)
  const options = usePoll(
    async () => {
      const [xs, rs] = await Promise.all([
        call<Execution[]>('runs.list', { limit: 100 }),
        call<ReceiptRecord[]>('receipts.list'),
      ])
      const o: Option[] = []
      ;(xs ?? [])
        .filter((x) =>
          ['COMPLETED', 'FAILED', 'CANCELLED', 'INTERRUPTED', 'REQUIRES_ATTENTION'].includes(
            x.state,
          ),
        )
        .forEach((x) =>
          o.push({ value: x.id, label: `Execution · ${x.task || x.id} · ${x.state}` }),
        )
      ;(rs ?? []).forEach((r) =>
        o.push({
          value: r.id,
          label: `Receipt · ${r.projectName} · ${r.status} · ${r.sha256.slice(0, 8)}`,
        }),
      )
      return o
    },
    [],
    0,
  )
  const [a, setA] = useState(params['a'] ?? '')
  const [b, setB] = useState(params['b'] ?? '')
  const [res, setRes] = useState<Comparison>()
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)

  const pickFile = async (set: (v: string) => void) => {
    const f = await native.pickFile('Choose a receipt (.runtide.zip)')
    if (f) set(f)
  }
  const run = async () => {
    setBusy(true)
    setErr(undefined)
    try {
      setRes(await call<Comparison>('receipts.compare', { a, b }))
    } catch (e) {
      setRes(undefined)
      setErr(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  const picker = (label: string, v: string, set: (x: string) => void) => (
    <label className="block min-w-0 flex-1">
      <span className="mb-1 block text-xs font-medium text-muted">{label}</span>
      <div className="flex gap-1">
        <Select
          aria-label={label}
          className="min-w-0 flex-1"
          value={v}
          onChange={(e) => set(e.target.value)}
        >
          <option value="">Choose…</option>
          {v && !(options.data ?? []).some((o) => o.value === v) && <option value={v}>{v}</option>}
          {(options.data ?? []).map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </Select>
        <Button onClick={() => void pickFile(set)}>File…</Button>
      </div>
    </label>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Compare"
        subtitle="What changed between two executions. Equal-looking evidence does not make them equivalent."
      />
      <div className="grid gap-3 px-6 pb-6">
        <div className="flex items-end gap-3">
          {picker('Baseline (A)', a, setA)}
          {picker('Candidate (B)', b, setB)}
          <Button
            variant="primary"
            disabled={!a || !b || busy}
            icon={<GitCompare className="h-3.5 w-3.5" />}
            onClick={() => void run()}
          >
            Compare
          </Button>
        </div>
        {busy && <Spinner label="Comparing" />}
        {err && (
          <div
            role="alert"
            className="selectable whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
          >
            {err}
          </div>
        )}
        {!res && !busy && !err && (
          <EmptyState
            title="Pick two executions or receipts"
            hint="Failures, changed commands, tool versions, files and durations are compared. Anything not recorded on both sides is listed as missing evidence."
          />
        )}
        {res && <Result c={res} />}
      </div>
    </div>
  )
}

function Result({ c }: { c: Comparison }) {
  const side = (s: Comparison['a'], label: string) => (
    <div className="min-w-0 flex-1 p-3">
      <div className="text-xs text-muted">{label}</div>
      <div className="truncate font-medium">{s.task}</div>
      <div className="flex items-center gap-2 text-sm">
        <StateBadge state={s.status} />
        <Badge tone={verdictTone(s.verification)}>verification {s.verification || 'none'}</Badge>
      </div>
      <div className="truncate font-mono text-xs text-muted">{s.executionId}</div>
    </div>
  )
  const v = c.verification
  return (
    <div className="grid gap-3" aria-label="Comparison result">
      <Panel>
        <div className="flex divide-x divide-line">
          {side(c.a, 'A')}
          {side(c.b, 'B')}
        </div>
        <div className="border-t border-line px-3 py-2 text-sm">
          Duration {ms(c.durations.aMs)} → {ms(c.durations.bMs)} (
          {c.durations.deltaMs >= 0 ? '+' : '-'}
          {ms(Math.abs(c.durations.deltaMs))})
        </div>
      </Panel>
      <div className="grid grid-cols-2 gap-3">
        <Panel title={`Verification: ${v.overall[0]} → ${v.overall[1]}`}>
          <List title="New failures" items={v.newFailures} tone="bad" />
          <List title="Resolved failures" items={v.resolvedFailures} tone="ok" />
          {(v.statusChanges ?? []).length > 0 && (
            <ul className="divide-y divide-line text-sm">
              {v.statusChanges?.map((s) => (
                <li key={s.name} className="flex items-center gap-2 px-3 py-1.5">
                  <span className="font-medium">{s.name}</span>
                  <Badge tone={statusTone(s.a)}>{s.a}</Badge>→
                  <Badge tone={statusTone(s.b)}>{s.b}</Badge>
                </li>
              ))}
            </ul>
          )}
          <List title="Only in A" items={v.onlyInA} />
          <List title="Only in B" items={v.onlyInB} />
          {!(v.newFailures ?? []).length &&
            !(v.resolvedFailures ?? []).length &&
            !(v.statusChanges ?? []).length && (
              <p className="p-3 text-sm text-muted">No verification status differences.</p>
            )}
        </Panel>
        <Panel title="Environment">
          <List title="Differences" items={c.environment.changed} tone="warn" />
          {(c.environment.tools ?? []).length > 0 && (
            <ul className="divide-y divide-line text-sm">
              {c.environment.tools?.map((t) => (
                <li key={t.id} className="px-3 py-1.5">
                  <b>{t.id}</b>: {t.change} {t.a || '-'} → {t.b || '-'}
                </li>
              ))}
            </ul>
          )}
          {!(c.environment.changed ?? []).length && !(c.environment.tools ?? []).length && (
            <p className="p-3 text-sm text-muted">No recorded environment differences.</p>
          )}
        </Panel>
      </div>
      <Panel title={`Commands (${c.commands.unchanged} unchanged)`}>
        <List title="New in B" items={c.commands.new} />
        <List title="Removed (only in A)" items={c.commands.removed} />
        {(c.commands.changed ?? []).length > 0 && (
          <table aria-label="Changed commands" className="w-full text-sm">
            <thead>
              <tr className="border-y border-line text-xs text-muted">
                <th className="h-8 px-3 text-left">Command</th>
                <th className="px-3 text-left">Exit</th>
                <th className="px-3 text-left">Output</th>
                <th className="px-3 text-left">Duration</th>
              </tr>
            </thead>
            <tbody>
              {c.commands.changed?.map((x) => (
                <tr key={x.command} className="border-b border-line/60">
                  <td className="selectable break-all px-3 py-1.5 font-mono text-xs">
                    {x.command}
                  </td>
                  <td className="px-3">
                    {x.exitA} → {x.exitB}
                  </td>
                  <td className="px-3">{x.output}</td>
                  <td className="px-3">
                    {ms(x.durationAMs)} → {ms(x.durationBMs)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
      <div className="grid grid-cols-2 gap-3">
        <Panel title={`Files (${c.files.identical} identical)`}>
          <List title="Only in A" items={c.files.onlyInA} />
          <List title="Only in B" items={c.files.onlyInB} />
          <List title="Same path, different result" items={c.files.changedContent} tone="warn" />
        </Panel>
        <Panel title="Failure events & services">
          <List title="New failure events" items={c.failureEvents.new} tone="bad" />
          <List title="Resolved failure events" items={c.failureEvents.resolved} tone="ok" />
          {(c.services ?? []).length > 0 && (
            <List
              title="Service health differences"
              items={c.services?.map((s) => `${s.name}: ${s.a} → ${s.b}`)}
              tone="warn"
            />
          )}
        </Panel>
      </div>
      <Panel title="Evidence missing or not comparable">
        <List title="" items={c.missingEvidence} tone="warn" />
        {!(c.missingEvidence ?? []).length && (
          <p className="p-3 text-sm text-muted">Nothing reported missing.</p>
        )}
      </Panel>
      <div className="selectable rounded border border-line bg-surface p-3 text-xs text-muted">
        {(c.notes ?? []).join(' ')}
      </div>
    </div>
  )
}
