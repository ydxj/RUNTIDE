import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { bytes, since, pct } from '../../services/format'
import type { Overview, ResourceSample } from '../../types'
import { useUI } from '../../stores/ui'
import {
  Badge,
  ErrorState,
  PageHeader,
  Panel,
  Spinner,
  StatusDot,
} from '../../components/ui/primitives'
import { AreaChart, Meter } from '../../components/ui/charts'

function Stat({
  label,
  value,
  sub,
  onClick,
}: {
  label: string
  value: string
  sub?: string
  onClick?: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-lg border border-line bg-surface px-4 py-3 text-left hover:bg-subtle"
    >
      <div className="text-xs text-muted">{label}</div>
      <div className="text-xl font-semibold leading-7 tabular-nums">{value}</div>
      {sub && <div className="text-xs text-muted">{sub}</div>}
    </button>
  )
}

const healthTone = (h: string) =>
  h === 'healthy' ? 'ok' : h === 'degraded' ? 'bad' : h === 'stopped' ? 'neutral' : 'warn'

export function OverviewPage() {
  const go = useUI((s) => s.go)
  const ov = usePoll(() => call<Overview>('overview'), [])
  const hist = usePoll(() => call<ResourceSample[]>('resources.history', { n: 120 }), [])

  if (ov.error && !ov.data) return <ErrorState message={ov.error} onRetry={ov.reload} />
  if (!ov.data) return <Spinner label="Reading machine state" />
  const o = ov.data
  const s = o.resources.sample
  const h = hist.data ?? []

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Overview"
        subtitle={`Measured ${since(o.generatedAt) === '0s ago' ? 'just now' : since(o.generatedAt)} · refreshes automatically`}
      />
      <div className="grid gap-3 px-6 pb-6">
        <div className="grid grid-cols-4 gap-3">
          <Stat
            label="CPU"
            value={pct(s?.cpuPercent)}
            sub={s ? 'whole machine' : 'sampling…'}
            onClick={() => go('resources')}
          />
          <Stat
            label="Memory"
            value={pct(s?.memPercent)}
            sub={s ? `${bytes(s.memUsed)} of ${bytes(s.memTotal)}` : 'sampling…'}
            onClick={() => go('resources')}
          />
          <Stat
            label="Processes"
            value={String(o.processes)}
            sub={`${o.listening} listening ports`}
            onClick={() => go('processes')}
          />
          <Stat
            label="Managed services"
            value={`${o.services.Running} / ${o.services.Total}`}
            sub={`${o.services.Failed} failed · ${o.services.Unhealthy} unhealthy`}
            onClick={() => go('services')}
          />
        </div>

        <div className="grid grid-cols-3 gap-3">
          <Panel title="CPU & memory" className="col-span-2">
            <div className="grid grid-cols-2 gap-4 p-3">
              <div>
                <div className="mb-1 text-xs text-muted">CPU %</div>
                <AreaChart values={h.map((x) => x.cpuPercent)} label="CPU history" />
              </div>
              <div>
                <div className="mb-1 text-xs text-muted">Memory %</div>
                <AreaChart
                  values={h.map((x) => x.memPercent)}
                  label="Memory history"
                  stroke="rgb(var(--ok))"
                />
              </div>
            </div>
            {(o.resources.disks ?? []).length > 0 && (
              <div className="grid grid-cols-2 gap-x-6 gap-y-2 border-t border-line p-3">
                {(o.resources.disks ?? []).map((d) => (
                  <div key={d.path}>
                    <div className="mb-1 flex justify-between text-xs">
                      <span>
                        {d.path} {d.label && `(${d.label})`}
                      </span>
                      <span className="text-muted">
                        {bytes(d.free)} free of {bytes(d.total)}
                      </span>
                    </div>
                    <Meter value={d.usedPercent} />
                  </div>
                ))}
              </div>
            )}
          </Panel>

          <Panel title="Attention">
            {o.warnings.length === 0 && (o.recentFailures ?? []).length === 0 ? (
              <div className="flex items-center gap-2 p-4 text-sm text-muted">
                <CheckCircle2 className="h-4 w-4 text-ok" aria-hidden /> Nothing needs attention.
              </div>
            ) : (
              <ul className="divide-y divide-line text-sm">
                {o.warnings.map((w) => (
                  <li key={w} className="flex gap-2 p-3">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" aria-hidden />
                    {w}
                  </li>
                ))}
                {(o.recentFailures ?? []).map((f, i) => (
                  <li key={i} className="p-3">
                    <div className="flex justify-between gap-2">
                      <span className="font-medium">{f.subject || f.kind}</span>
                      <span className="text-xs text-muted">{since(f.time)}</span>
                    </div>
                    <div className="truncate text-xs text-muted" title={f.detail}>
                      {f.detail}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Panel
            title="Workspaces"
            actions={
              <button className="px-2 text-xs text-accent" onClick={() => go('workspaces')}>
                Manage
              </button>
            }
          >
            {(o.workspaces ?? []).length === 0 ? (
              <div className="p-4 text-sm text-muted">
                No workspaces yet. Create one on the Workspaces page.
              </div>
            ) : (
              <ul className="divide-y divide-line text-sm">
                {(o.workspaces ?? []).map((w) => (
                  <li key={w.name} className="flex h-9 items-center gap-2 px-3">
                    <StatusDot tone={healthTone(w.health)} />
                    <span className="flex-1 font-medium">{w.name}</span>
                    <Badge tone={healthTone(w.health)}>{w.health}</Badge>
                    <span className="w-14 text-right text-xs text-muted">
                      {w.running}/{w.total} up
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
          <Panel
            title="Recently started processes"
            actions={
              <button className="px-2 text-xs text-accent" onClick={() => go('processes')}>
                All processes
              </button>
            }
          >
            <ul className="divide-y divide-line text-sm">
              {(o.recentProcesses ?? []).map((p) => (
                <li key={p.pid} className="flex h-9 items-center gap-2 px-3">
                  <span className="flex-1 truncate">{p.name}</span>
                  <span className="text-xs text-muted tabular-nums">PID {p.pid}</span>
                  <span className="w-20 text-right text-xs text-muted">{since(p.startTime)}</span>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
      </div>
    </div>
  )
}
