import { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, CircleAlert, Info, Stethoscope, TriangleAlert, Wrench } from 'lucide-react'
import { call } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { DoctorReport, Finding, FixPlan } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  PageHeader,
  Panel,
  Spinner,
} from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'

const icon = (s: Finding['severity']) =>
  s === 'error' ? (
    <CircleAlert className="h-4 w-4 text-bad" aria-label="error" />
  ) : s === 'warning' ? (
    <TriangleAlert className="h-4 w-4 text-warn" aria-label="warning" />
  ) : (
    <Info className="h-4 w-4 text-accent" aria-label="note" />
  )

export function DoctorPage() {
  const params = useUI((s) => s.pageParams)
  const toast = useUI((s) => s.toast)
  const [report, setReport] = useState<DoctorReport>()
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string>()
  const [filter, setFilter] = useState<'all' | Finding['severity']>('all')
  const [plan, setPlan] = useState<FixPlan | null>(null)

  const run = useCallback(async () => {
    setRunning(true)
    setError(undefined)
    try {
      setReport(await call<DoctorReport>('doctor.run'))
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setRunning(false)
    }
  }, [])

  useEffect(() => {
    if (params['autorun']) void run()
  }, [params, run])

  const preview = async (f: Finding) => {
    try {
      setPlan(await call<FixPlan>('doctor.plan', { fixId: f.fixId }))
    } catch (e) {
      reportError('This fix cannot be applied', e)
    }
  }
  const apply = async () => {
    if (!plan) return
    try {
      await call('doctor.apply', { fixId: plan.fixId, confirm: true })
      toast({ kind: 'success', title: 'Fix applied', detail: plan.changes.join('; ') })
      setPlan(null)
      void run()
    } catch (e) {
      reportError('Fix failed', e)
    }
  }

  const shown = (report?.findings ?? []).filter((f) => filter === 'all' || f.severity === filter)

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Doctor"
        subtitle="Checks your real workspaces, ports, tools, Docker and WSL. Read-only; fixes are always opt-in."
      >
        <Button
          variant="primary"
          disabled={running}
          icon={<Stethoscope className="h-3.5 w-3.5" />}
          onClick={() => void run()}
        >
          {running ? 'Checking…' : report ? 'Run again' : 'Run diagnostics'}
        </Button>
      </PageHeader>
      <div className="grid gap-3 px-6 pb-6">
        {running && !report && <Spinner label="Inspecting your environment" />}
        {error && (
          <div role="alert" className="rounded border border-bad/40 bg-bad/10 p-3 text-sm text-bad">
            {error}
          </div>
        )}
        {!report && !running && !error && (
          <EmptyState
            title="No diagnostics run yet"
            hint="Run diagnostics to look for port conflicts, missing executables, bad paths, missing environment variables, failed services and more."
          />
        )}
        {report && (
          <>
            <div className="flex items-center gap-2 text-sm">
              {(['all', 'error', 'warning', 'info'] as const).map((f) => (
                <button
                  key={f}
                  type="button"
                  aria-pressed={filter === f}
                  onClick={() => setFilter(f)}
                  className={`h-7 rounded-full border px-3 text-xs ${filter === f ? 'border-accent bg-accent/10 text-accent' : 'border-line bg-surface'}`}
                >
                  {f === 'all'
                    ? `All (${report.findings.length})`
                    : f === 'error'
                      ? `Errors (${report.errors})`
                      : f === 'warning'
                        ? `Warnings (${report.warnings})`
                        : `Notes (${report.infos})`}
                </button>
              ))}
            </div>
            {report.findings.length === 0 && (
              <Panel>
                <div className="flex items-center gap-2 p-4 text-sm">
                  <CheckCircle2 className="h-5 w-5 text-ok" /> No problems found.
                </div>
              </Panel>
            )}
            {shown.map((f) => (
              <Panel key={f.id}>
                <div className="flex gap-3 p-3">
                  <div className="pt-0.5">{icon(f.severity)}</div>
                  <div className="selectable min-w-0 flex-1 text-sm">
                    <div className="flex items-center gap-2">
                      <span className="font-semibold">{f.title}</span>
                      <Badge>{f.component}</Badge>
                    </div>
                    <p className="mt-0.5 text-muted">{f.description}</p>
                    {(f.evidence ?? []).filter(Boolean).length > 0 && (
                      <ul className="mt-2 space-y-0.5 rounded border border-line bg-subtle p-2 font-mono text-xs">
                        {(f.evidence ?? []).filter(Boolean).map((e, i) => (
                          <li key={i} className="break-all">
                            {e}
                          </li>
                        ))}
                      </ul>
                    )}
                    <p className="mt-2">
                      <b>How to fix:</b> {f.resolution}
                    </p>
                  </div>
                  {f.fixAvailable && (
                    <Button
                      icon={<Wrench className="h-3.5 w-3.5" />}
                      onClick={() => void preview(f)}
                    >
                      Preview fix…
                    </Button>
                  )}
                </div>
              </Panel>
            ))}
          </>
        )}
      </div>
      {plan && (
        <Modal
          title="Apply this fix?"
          onClose={() => setPlan(null)}
          footer={
            <>
              <Button onClick={() => setPlan(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => void apply()}>
                Apply fix
              </Button>
            </>
          }
        >
          <p className="mb-2 text-sm">{plan.description}</p>
          <p className="text-xs font-medium text-muted">This will:</p>
          <ul className="selectable mt-1 list-disc space-y-1 pl-5 text-sm">
            {plan.changes.map((c) => (
              <li key={c} className="break-all">
                {c}
              </li>
            ))}
          </ul>
          <p className="mt-3 text-xs text-muted">Existing files are never overwritten.</p>
        </Modal>
      )}
    </div>
  )
}
