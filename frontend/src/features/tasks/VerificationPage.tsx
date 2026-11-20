import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { since } from '../../services/format'
import { useUI } from '../../stores/ui'
import type { Execution, ExecutionDetail } from '../../types'
import {
  Badge,
  EmptyState,
  ErrorState,
  PageHeader,
  Panel,
  Spinner,
} from '../../components/ui/primitives'
import { StateBadge, statusTone, verdictTone } from './common'

/** Verification verdicts across recent executions (each loaded from its recorded results). */
export function VerificationPage() {
  const go = useUI((s) => s.go)
  const { data, error, loading, reload } = usePoll(
    async () => {
      const xs = await call<Execution[]>('runs.list', { limit: 15 })
      const ds = await Promise.all(
        (xs ?? []).map((x) => call<ExecutionDetail>('runs.get', { id: x.id })),
      )
      return ds
    },
    [],
    5,
  )
  if (loading && !data) return <Spinner />
  if (error && !data) return <ErrorState message={error} onRetry={() => void reload()} />
  const ds = data ?? []
  const counts = { passed: 0, failed: 0, none: 0 }
  ds.forEach((d) => {
    const v = d.verificationSummary.overall
    if (v === 'passed') counts.passed++
    else if (v === 'failed') counts.failed++
    else counts.none++
  })
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Verification"
        subtitle="Whether tasks actually met their acceptance criteria. A task is verified only when every required check ran and passed."
      />
      <div className="grid gap-3 px-6 pb-6">
        <div className="grid grid-cols-3 gap-3 text-sm">
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Passed</div>
              <div className="text-xl font-semibold text-ok">{counts.passed}</div>
            </div>
          </Panel>
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Failed</div>
              <div className="text-xl font-semibold text-bad">{counts.failed}</div>
            </div>
          </Panel>
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Not verified</div>
              <div className="text-xl font-semibold">{counts.none}</div>
            </div>
          </Panel>
        </div>
        {ds.length === 0 && (
          <EmptyState
            title="Nothing to verify yet"
            hint="Executions appear here with the results of their verification pipelines."
          />
        )}
        {ds.map((d) => (
          <Panel
            key={d.execution.id}
            title={
              <button
                type="button"
                className="text-left hover:underline"
                onClick={() => go('executions', { id: d.execution.id })}
              >
                {d.execution.task || d.execution.id}
              </button>
            }
            actions={
              <>
                <StateBadge state={d.execution.state} />
                <Badge tone={verdictTone(d.verificationSummary.overall)}>
                  {d.verificationSummary.overall === 'none'
                    ? 'not verified'
                    : d.verificationSummary.overall}
                </Badge>
              </>
            }
          >
            {d.verification.length === 0 ? (
              <div className="p-3 text-sm text-muted">
                No verification results ({since(d.execution.createdAt)}).
              </div>
            ) : (
              <ul className="divide-y divide-line text-sm">
                {d.verification.map((v) => (
                  <li key={v.name} className="flex items-center gap-3 px-3 py-1.5">
                    <Badge tone={statusTone(v.status)}>{v.status.replace('_', ' ')}</Badge>
                    <span className="w-48 font-medium">{v.name}</span>
                    <span className="selectable min-w-0 flex-1 truncate text-muted" title={v.error}>
                      {v.error}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        ))}
      </div>
    </div>
  )
}
