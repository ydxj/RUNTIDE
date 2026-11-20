import { useMemo, useState } from 'react'
import { Pencil, Play, Plus, RefreshCw, RotateCw, ScrollText, Square, Trash2 } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { useEngineEvent } from '../../hooks/useEngineEvent'
import { call } from '../../services/api'
import { duration, joinArgs, since } from '../../services/format'
import { reportError, useUI } from '../../stores/ui'
import type { HealthStatus, RunRecord, ServiceState, ServiceView } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  PageHeader,
  Spinner,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { ServiceForm } from './ServiceForm'

export const stateTone = (s: ServiceState | undefined) =>
  s === 'running'
    ? 'ok'
    : s === 'failed'
      ? 'bad'
      : s === 'backoff' || s === 'starting' || s === 'stopping'
        ? 'warn'
        : 'neutral'
export const healthTone = (h: HealthStatus | undefined) =>
  h === 'healthy' ? 'ok' : h === 'unhealthy' ? 'bad' : h === 'starting' ? 'warn' : 'neutral'

export function ServicesPage() {
  const go = useUI((s) => s.go)
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const [query, setQuery] = useState('')
  const [sel, setSel] = useState<string | null>(null)
  const [form, setForm] = useState<{ initial: ServiceView | null } | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const { data, error, loading, reload } = usePoll(
    () => call<ServiceView[]>('services.list'),
    [],
    2,
  )
  useEngineEvent('service', () => void reload())
  const selected = data?.find((s) => s.key === sel)
  const runs = usePoll(
    () => (sel ? call<RunRecord[]>('services.runs', { key: sel, limit: 8 }) : Promise.resolve([])),
    [sel],
    5,
  )

  const act = async (s: ServiceView, verb: 'start' | 'stop' | 'restart') => {
    setBusy(s.key)
    try {
      await call(`services.${verb}`, { key: s.key })
      toast({
        kind: 'success',
        title: `${s.key} ${verb === 'stop' ? 'stopped' : verb === 'start' ? 'started' : 'restarted'}`,
      })
    } catch (e) {
      reportError(`Could not ${verb} ${s.key}`, e)
    } finally {
      setBusy(null)
      void reload()
    }
  }

  const remove = (s: ServiceView) =>
    ask({
      title: `Delete ${s.key}?`,
      confirmLabel: 'Delete',
      danger: true,
      body: 'The service definition is removed from RUNTIDE. Project files are not touched.',
      onConfirm: async () => {
        try {
          await call('services.delete', { key: s.key })
          setSel(null)
        } catch (e) {
          reportError('Could not delete service', e)
        }
        void reload()
      },
    })

  const rows = useMemo(
    () => (data ?? []).filter((s) => !query || s.key.toLowerCase().includes(query.toLowerCase())),
    [data, query],
  )

  const cols: Column<ServiceView>[] = [
    {
      key: 'name',
      header: 'Service',
      sort: (a, b) => a.key.localeCompare(b.key),
      render: (s) => (
        <span className="flex items-center gap-2">
          <span className="font-medium">{s.name}</span>
          {s.workspace && <Badge>{s.workspace}</Badge>}
        </span>
      ),
    },
    { key: 'type', header: 'Type', width: '90px', render: (s) => s.spec.type },
    {
      key: 'state',
      header: 'State',
      width: '110px',
      render: (s) => (
        <Badge tone={stateTone(s.runtime?.state)}>{s.runtime?.state ?? 'stopped'}</Badge>
      ),
    },
    {
      key: 'health',
      header: 'Health',
      width: '120px',
      render: (s) => (
        <Badge tone={healthTone(s.runtime?.health)}>
          {s.runtime?.health ?? (s.spec.health ? 'unknown' : 'not monitored')}
        </Badge>
      ),
    },
    {
      key: 'pid',
      header: 'PID',
      width: '72px',
      align: 'right',
      render: (s) => (s.runtime?.pid ? s.runtime.pid : ''),
    },
    {
      key: 'up',
      header: 'Uptime',
      width: '90px',
      align: 'right',
      render: (s) => (s.runtime && s.runtime.pid ? duration(s.runtime.uptimeSec) : ''),
    },
    {
      key: 'act',
      header: 'Actions',
      width: '132px',
      render: (s) => {
        const active =
          s.runtime && ['running', 'starting', 'backoff', 'stopping'].includes(s.runtime.state)
        return (
          <span className="flex" onClick={(e) => e.stopPropagation()}>
            {active ? (
              <IconButton
                label={`Stop ${s.key}`}
                disabled={busy === s.key}
                onClick={() => void act(s, 'stop')}
              >
                <Square className="h-3.5 w-3.5" />
              </IconButton>
            ) : (
              <IconButton
                label={`Start ${s.key}`}
                disabled={busy === s.key}
                onClick={() => void act(s, 'start')}
              >
                <Play className="h-3.5 w-3.5" />
              </IconButton>
            )}
            <IconButton
              label={`Restart ${s.key}`}
              disabled={busy === s.key}
              onClick={() => void act(s, 'restart')}
            >
              <RotateCw className="h-3.5 w-3.5" />
            </IconButton>
            <IconButton label={`Logs of ${s.key}`} onClick={() => go('logs', { service: s.key })}>
              <ScrollText className="h-3.5 w-3.5" />
            </IconButton>
          </span>
        )
      },
    },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Services"
        subtitle="Processes launched and supervised by RUNTIDE. They are not the same as native Windows services."
      >
        <Input
          aria-label="Filter services"
          placeholder="Filter services"
          className="w-56"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => void reload()}>
          Refresh
        </Button>
        <Button
          variant="primary"
          icon={<Plus className="h-3.5 w-3.5" />}
          onClick={() => setForm({ initial: null })}
        >
          Add service
        </Button>
      </PageHeader>
      <div className="flex min-h-0 flex-1 gap-3 px-6 pb-4">
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
          {loading && !data ? (
            <Spinner />
          ) : error ? (
            <ErrorState message={error} onRetry={() => void reload()} />
          ) : (
            <DataTable
              label="Services"
              columns={cols}
              rows={rows}
              rowKey={(s) => s.key}
              selectedKey={sel}
              onSelect={(s) => setSel(s.key)}
              empty={
                <EmptyState
                  title="No services yet"
                  hint="Add a standalone service, or define several together in a workspace."
                  action={
                    <Button variant="primary" onClick={() => setForm({ initial: null })}>
                      Add service
                    </Button>
                  }
                />
              }
            />
          )}
        </div>
        {selected && (
          <aside
            aria-label="Service details"
            className="selectable w-80 shrink-0 overflow-auto rounded-lg border border-line bg-surface p-4 text-sm"
          >
            <div className="text-base font-semibold">{selected.key}</div>
            <div className="mt-1 break-all rounded bg-subtle p-2 font-mono text-xs">
              {selected.spec.command} {joinArgs(selected.spec.args)}
            </div>
            <dl className="mt-3 space-y-2">
              <div>
                <dt className="text-xs text-muted">Working directory</dt>
                <dd className="break-all">
                  {selected.spec.workingDirectory || selected.root || '-'}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted">Restart policy</dt>
                <dd>
                  {selected.spec.restart.policy}
                  {selected.spec.restart.policy !== 'never' &&
                    `, up to ${selected.spec.restart.maxRetries} times`}
                </dd>
              </div>
              {selected.runtime && (
                <>
                  <div>
                    <dt className="text-xs text-muted">Exit code</dt>
                    <dd>{selected.runtime.exitCode ?? '-'}</dd>
                  </div>
                  {selected.runtime.lastError && (
                    <div>
                      <dt className="text-xs text-muted">Last error</dt>
                      <dd className="text-bad">{selected.runtime.lastError}</dd>
                    </div>
                  )}
                  {selected.runtime.healthDetail && (
                    <div>
                      <dt className="text-xs text-muted">Health detail</dt>
                      <dd>{selected.runtime.healthDetail}</dd>
                    </div>
                  )}
                  {selected.runtime.totalRestarts > 0 && (
                    <div>
                      <dt className="text-xs text-muted">Automatic restarts</dt>
                      <dd>{selected.runtime.totalRestarts}</dd>
                    </div>
                  )}
                </>
              )}
            </dl>
            <h3 className="mt-4 mb-1 text-xs font-semibold text-muted">Recent runs</h3>
            <ul className="space-y-1 text-xs">
              {(runs.data ?? []).map((r) => (
                <li
                  key={r.startedAt + r.pid}
                  className="flex justify-between gap-2 rounded bg-subtle px-2 py-1"
                >
                  <span>{since(r.startedAt)}</span>
                  <span className="text-muted">
                    {r.endedAt ? `exit ${r.exitCode} · ${r.reason}` : 'running'}
                  </span>
                </li>
              ))}
              {(runs.data ?? []).length === 0 && <li className="text-muted">No runs recorded.</li>}
            </ul>
            <div className="mt-4 flex gap-2">
              {!selected.workspace && (
                <>
                  <Button
                    icon={<Pencil className="h-3.5 w-3.5" />}
                    onClick={() => setForm({ initial: selected })}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="danger"
                    icon={<Trash2 className="h-3.5 w-3.5" />}
                    onClick={() => remove(selected)}
                  >
                    Delete
                  </Button>
                </>
              )}
              {selected.workspace && (
                <Button onClick={() => go('stacks', { name: selected.workspace })}>
                  Open stack
                </Button>
              )}
            </div>
          </aside>
        )}
      </div>
      {form && (
        <ServiceForm
          initial={form.initial?.spec ?? null}
          onClose={() => setForm(null)}
          onSaved={() => {
            setForm(null)
            void reload()
          }}
        />
      )}
    </div>
  )
}
