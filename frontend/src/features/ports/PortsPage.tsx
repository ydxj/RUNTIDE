import { useMemo, useState } from 'react'
import { ArrowRight, RefreshCw, Skull, TriangleAlert } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { useUI } from '../../stores/ui'
import type { Listener, Process } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Spinner,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { confirmKill } from '../processes/killProcess'

const key = (l: Listener) => `${l.protocol}/${l.family}/${l.address}/${l.port}/${l.pid}`

export function PortsPage() {
  const go = useUI((s) => s.go)
  const [query, setQuery] = useState('')
  const [udp, setUdp] = useState(false)
  const [sel, setSel] = useState<Listener | null>(null)
  const { data, error, loading, reload } = usePoll(
    () => call<Listener[]>('ports.list', { query, udp }),
    [query, udp],
  )

  const cols: Column<Listener>[] = useMemo(
    () => [
      {
        key: 'port',
        header: 'Port',
        width: '84px',
        align: 'right',
        sort: (a, b) => a.port - b.port,
        render: (l) => l.port,
      },
      { key: 'proto', header: 'Proto', width: '64px', render: (l) => l.protocol.toUpperCase() },
      {
        key: 'family',
        header: 'IP',
        width: '60px',
        render: (l) => (l.family === 'IPv4' ? 'v4' : 'v6'),
      },
      {
        key: 'addr',
        header: 'Local address',
        mono: true,
        sort: (a, b) => a.address.localeCompare(b.address),
        render: (l) => l.address,
      },
      {
        key: 'proc',
        header: 'Process',
        sort: (a, b) => a.processName.localeCompare(b.processName),
        render: (l) => (
          <span className="flex items-center gap-1.5">
            {l.processName}
            {l.managed && <Badge tone="accent">{l.managed}</Badge>}
            {l.stale && <Badge tone="warn">stale</Badge>}
          </span>
        ),
      },
      {
        key: 'pid',
        header: 'PID',
        width: '72px',
        align: 'right',
        sort: (a, b) => a.pid - b.pid,
        render: (l) => l.pid,
      },
      {
        key: 'exp',
        header: 'Exposure',
        width: '120px',
        render: (l) => (
          <Badge
            tone={
              l.exposure === 'loopback'
                ? 'neutral'
                : l.exposure === 'all-interfaces'
                  ? 'warn'
                  : 'accent'
            }
          >
            {l.exposure === 'all-interfaces' ? 'all interfaces' : l.exposure}
          </Badge>
        ),
      },
      {
        key: 'note',
        header: 'Notes',
        width: '170px',
        render: (l) =>
          l.conflict ? (
            <span className="flex items-center gap-1 text-warn">
              <TriangleAlert className="h-3.5 w-3.5" aria-hidden /> shared by several processes
            </span>
          ) : (
            ''
          ),
      },
    ],
    [],
  )

  const terminate = async (l: Listener) => {
    const ps = await call<Process[]>('processes.list', { query: String(l.pid) })
    const p = ps.find((x) => x.pid === l.pid)
    if (!p) {
      useUI.getState().toast({ kind: 'info', title: 'That process has already exited' })
      void reload()
      return
    }
    confirmKill(p, () => {
      setSel(null)
      void reload()
    })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Ports"
        subtitle="Listening TCP sockets straight from the Windows socket table."
      >
        <Input
          aria-label="Filter ports"
          placeholder="Filter by port, process, address or PID"
          className="w-72"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <label className="flex items-center gap-1.5 text-sm">
          <input type="checkbox" checked={udp} onChange={(e) => setUdp(e.target.checked)} /> Include
          UDP
        </label>
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => void reload()}>
          Refresh
        </Button>
      </PageHeader>
      <div className="flex min-h-0 flex-1 gap-3 px-6 pb-4">
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
          {loading && !data ? (
            <Spinner label="Reading socket table" />
          ) : error ? (
            <ErrorState message={error} onRetry={() => void reload()} />
          ) : (
            <DataTable
              label="Listening ports"
              columns={cols}
              rows={data ?? []}
              rowKey={key}
              selectedKey={sel ? key(sel) : null}
              onSelect={setSel}
              initialSort={{ key: 'port', dir: 'asc' }}
              empty={
                <EmptyState
                  title="No listeners match"
                  hint="Nothing is listening on a port that matches this filter."
                />
              }
            />
          )}
        </div>
        {sel && (
          <aside
            aria-label="Port details"
            className="selectable w-80 shrink-0 overflow-auto rounded-lg border border-line bg-surface p-4 text-sm"
          >
            <div className="text-base font-semibold">
              {sel.protocol.toUpperCase()} port {sel.port}
            </div>
            <div className="text-muted">
              {sel.address} · {sel.family}
            </div>
            <dl className="mt-3 space-y-2">
              <div>
                <dt className="text-xs text-muted">Owning process</dt>
                <dd>
                  {sel.processName} (PID {sel.pid})
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Executable</dt>
                <dd className="break-all">{sel.processPath || 'unavailable'}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Reachable from</dt>
                <dd>
                  {
                    {
                      loopback: 'this machine only',
                      'all-interfaces': 'every network interface',
                      network: 'a specific network interface',
                    }[sel.exposure]
                  }
                </dd>
              </div>
              {sel.managed && (
                <div>
                  <dt className="text-xs text-muted">RUNTIDE service</dt>
                  <dd>{sel.managed}</dd>
                </div>
              )}
            </dl>
            {sel.conflict && (
              <p className="mt-3 rounded border border-warn/40 bg-warn/10 p-2 text-xs">
                {sel.conflict}. Requests may reach a different process than the one you expect.
              </p>
            )}
            {sel.stale && (
              <p className="mt-3 rounded border border-line bg-subtle p-2 text-xs text-muted">
                The owning process could not be resolved; it may have just exited.
              </p>
            )}
            <div className="mt-4 flex flex-wrap gap-2">
              <Button
                icon={<ArrowRight className="h-3.5 w-3.5" />}
                onClick={() => go('processes', { query: String(sel.pid) })}
              >
                Go to process
              </Button>
              <Button
                variant="danger"
                icon={<Skull className="h-3.5 w-3.5" />}
                disabled={sel.protection === 'protected' || sel.stale}
                onClick={() => void terminate(sel)}
              >
                Terminate…
              </Button>
            </div>
          </aside>
        )}
      </div>
    </div>
  )
}
