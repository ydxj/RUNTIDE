import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { bytes, pct } from '../../services/format'
import { reportError, useUI } from '../../stores/ui'
import type { CurrentResources, Process, ResourceSample, Settings } from '../../types'
import { ErrorState, PageHeader, Panel, Select, Spinner } from '../../components/ui/primitives'
import { AreaChart, Meter } from '../../components/ui/charts'

export function ResourcesPage() {
  const settings = useUI((s) => s.settings)
  const setSettings = useUI((s) => s.setSettings)
  const cur = usePoll(() => call<CurrentResources>('resources.current'), [])
  const hist = usePoll(() => call<ResourceSample[]>('resources.history', { n: 300 }), [])
  const procs = usePoll(() => call<Process[]>('processes.list', {}), [], 5)

  const setInterval_ = async (sec: number) => {
    try {
      setSettings(await call<Settings>('settings.set', { sampleSec: sec }))
    } catch (e) {
      reportError('Could not change sampling interval', e)
    }
  }

  if (cur.error && !cur.data) return <ErrorState message={cur.error} onRetry={cur.reload} />
  if (!cur.data) return <Spinner />
  const s = cur.data.sample
  const h = hist.data ?? []
  const topCpu = [...(procs.data ?? [])].sort((a, b) => b.cpuPercent - a.cpuPercent).slice(0, 6)
  const topMem = [...(procs.data ?? [])].sort((a, b) => b.memoryBytes - a.memoryBytes).slice(0, 6)
  const span =
    h.length > 1
      ? Math.round(
          ((Date.parse(h[h.length - 1]!.time) - Date.parse(h[0]!.time)) / 1000 / 60) * 10,
        ) / 10
      : 0

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Resources"
        subtitle={`${h.length} samples over ${span} min kept in memory. Sampling only runs while RUNTIDE is running.`}
      >
        <label className="flex items-center gap-2 text-sm">
          Sample every
          <Select
            aria-label="Sampling interval"
            value={settings?.sampleSec ?? 2}
            onChange={(e) => void setInterval_(Number(e.target.value))}
          >
            {[1, 2, 5, 10, 30, 60].map((n) => (
              <option key={n} value={n}>
                {n} s
              </option>
            ))}
          </Select>
        </label>
      </PageHeader>
      <div className="grid gap-3 px-6 pb-6">
        <div className="grid grid-cols-2 gap-3">
          <Panel title={`CPU: ${pct(s?.cpuPercent)}`}>
            <div className="p-3">
              <AreaChart values={h.map((x) => x.cpuPercent)} label="CPU utilisation" height={140} />
            </div>
          </Panel>
          <Panel title={`Memory: ${bytes(s?.memUsed)} of ${bytes(s?.memTotal)}`}>
            <div className="p-3">
              <AreaChart
                values={h.map((x) => x.memPercent)}
                label="Memory utilisation"
                height={140}
                stroke="rgb(var(--ok))"
              />
              <dl className="mt-2 grid grid-cols-3 gap-2 text-xs">
                <div>
                  <dt className="text-muted">Used</dt>
                  <dd>{bytes(s?.memUsed)}</dd>
                </div>
                <div>
                  <dt className="text-muted">Available</dt>
                  <dd>{bytes(s?.memAvailable)}</dd>
                </div>
                <div>
                  <dt className="text-muted">Commit charge</dt>
                  <dd>
                    {bytes(s?.commitUsed)} / {bytes(s?.commitTotal)}
                  </dd>
                </div>
              </dl>
            </div>
          </Panel>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Panel title="Top processes by CPU">
            <ul className="divide-y divide-line text-sm">
              {topCpu.map((p) => (
                <li key={p.pid} className="flex h-8 items-center px-3">
                  <span className="flex-1 truncate">{p.name}</span>
                  <span className="tabular-nums text-muted">{p.cpuPercent.toFixed(1)}%</span>
                </li>
              ))}
            </ul>
          </Panel>
          <Panel title="Top processes by memory">
            <ul className="divide-y divide-line text-sm">
              {topMem.map((p) => (
                <li key={p.pid} className="flex h-8 items-center px-3">
                  <span className="flex-1 truncate">{p.name}</span>
                  <span className="tabular-nums text-muted">{bytes(p.memoryBytes)}</span>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
        <Panel title="Disks">
          <div className="grid grid-cols-2 gap-x-6 gap-y-3 p-3">
            {(cur.data.disks ?? []).map((d) => (
              <div key={d.path}>
                <div className="mb-1 flex justify-between text-sm">
                  <span>
                    {d.path} {d.label} <span className="text-xs text-muted">{d.fileSystem}</span>
                  </span>
                  <span className="text-muted">
                    {bytes(d.free)} free of {bytes(d.total)}
                  </span>
                </div>
                <Meter value={d.usedPercent} />
              </div>
            ))}
          </div>
        </Panel>
        <Panel title="Network interfaces">
          <table aria-label="Network interfaces" className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-xs text-muted">
                <th className="h-8 px-3 text-left">Interface</th>
                <th className="px-3 text-left">Status</th>
                <th className="px-3 text-left">Addresses</th>
              </tr>
            </thead>
            <tbody>
              {(cur.data.interfaces ?? []).map((i) => (
                <tr key={i.name} className="h-8 border-b border-line/60">
                  <td className="px-3">{i.name}</td>
                  <td className="px-3">{i.up ? 'up' : 'down'}</td>
                  <td className="selectable px-3 font-mono text-xs">
                    {(i.addrs ?? []).join(', ')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="border-t border-line p-3 text-xs text-muted">
            Per-interface throughput counters are not collected; see docs for the reason.
          </p>
        </Panel>
      </div>
    </div>
  )
}
