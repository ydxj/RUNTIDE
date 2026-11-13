import { useState } from 'react'
import { Play, RefreshCw, Square } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { reportError, useUI } from '../../stores/ui'
import type { Container, DockerStatus, WinService, WslStatus } from '../../types'
import {
  Badge,
  Button,
  IconButton,
  Input,
  PageHeader,
  Panel,
  Spinner,
} from '../../components/ui/primitives'

function Docker() {
  const toast = useUI((s) => s.toast)
  const st = usePoll(() => call<DockerStatus>('docker.status'), [], 10)
  const ok = st.data?.daemonAvailable
  const list = usePoll(
    () => (ok ? call<Container[]>('docker.list') : Promise.resolve([])),
    [ok],
    10,
  )
  const act = async (c: Container, verb: 'start' | 'stop') => {
    try {
      await call(`docker.${verb}`, { id: c.id })
      toast({ kind: 'success', title: `${c.name} ${verb === 'start' ? 'started' : 'stopped'}` })
    } catch (e) {
      reportError(`Could not ${verb} ${c.name}`, e)
    }
    void list.reload()
  }
  const projects = new Map<string, Container[]>()
  for (const c of list.data ?? [])
    if (c.composeProject)
      projects.set(c.composeProject, [...(projects.get(c.composeProject) ?? []), c])
  return (
    <Panel
      title="Docker"
      actions={
        <Button
          icon={<RefreshCw className="h-3.5 w-3.5" />}
          onClick={() => {
            void st.reload()
            void list.reload()
          }}
        >
          Refresh
        </Button>
      }
    >
      {!st.data ? (
        <Spinner />
      ) : (
        <>
          <div className="flex items-center gap-3 border-b border-line p-3 text-sm">
            <Badge tone={ok ? 'ok' : 'neutral'}>
              {ok
                ? 'daemon running'
                : st.data.cliInstalled
                  ? 'daemon unreachable'
                  : 'not installed'}
            </Badge>
            <span className="selectable text-muted">
              {st.data.message}
              {ok && ` · server ${st.data.serverVersion}`}
              {st.data.clientVersion && ` · CLI ${st.data.clientVersion}`}
            </span>
          </div>
          {ok && (
            <table aria-label="Containers" className="w-full text-sm">
              <thead>
                <tr className="border-b border-line text-xs text-muted">
                  <th className="h-8 px-3 text-left">Name</th>
                  <th className="px-3 text-left">State</th>
                  <th className="px-3 text-left">Ports</th>
                  <th className="px-3 text-right">CPU</th>
                  <th className="px-3 text-right">Memory</th>
                  <th className="px-3 text-left">Compose</th>
                  <th className="w-20" />
                </tr>
              </thead>
              <tbody>
                {(list.data ?? []).map((c) => (
                  <tr key={c.id} className="h-8 border-b border-line/60">
                    <td className="px-3">
                      <div className="font-medium">{c.name}</div>
                      <div className="text-xs text-muted">{c.image}</div>
                    </td>
                    <td className="px-3">
                      <Badge tone={c.state === 'running' ? 'ok' : 'neutral'}>{c.state}</Badge>
                    </td>
                    <td
                      className="selectable max-w-[220px] truncate px-3 font-mono text-xs"
                      title={(c.ports ?? []).join('\n')}
                    >
                      {(c.ports ?? []).join(', ')}
                    </td>
                    <td className="px-3 text-right tabular-nums">{c.cpuPercent ?? ''}</td>
                    <td className="px-3 text-right tabular-nums">
                      {c.memUsage?.split('/')[0] ?? ''}
                    </td>
                    <td className="px-3 text-xs text-muted">
                      {c.composeProject ? `${c.composeProject}/${c.composeService}` : ''}
                    </td>
                    <td className="px-3 text-right">
                      {c.state === 'running' ? (
                        <IconButton label={`Stop ${c.name}`} onClick={() => void act(c, 'stop')}>
                          <Square className="h-3.5 w-3.5" />
                        </IconButton>
                      ) : (
                        <IconButton label={`Start ${c.name}`} onClick={() => void act(c, 'start')}>
                          <Play className="h-3.5 w-3.5" />
                        </IconButton>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {ok && projects.size > 0 && (
            <div className="border-t border-line p-3 text-xs text-muted">
              Compose projects:{' '}
              {Array.from(projects.entries())
                .map(([k, v]) => `${k} (${v.length})`)
                .join(', ')}
            </div>
          )}
          {ok && (list.data ?? []).length === 0 && (
            <div className="p-4 text-sm text-muted">No containers exist yet.</div>
          )}
        </>
      )}
    </Panel>
  )
}

function Wsl() {
  const st = usePoll(() => call<WslStatus>('wsl.status'), [], 15)
  return (
    <Panel title="WSL" actions={<span className="px-2 text-xs text-muted">read-only</span>}>
      {!st.data ? (
        <Spinner />
      ) : (
        <>
          <div className="border-b border-line p-3 text-sm text-muted">{st.data.message}</div>
          {(st.data.distros ?? []).length > 0 && (
            <table aria-label="WSL distributions" className="w-full text-sm">
              <thead>
                <tr className="border-b border-line text-xs text-muted">
                  <th className="h-8 px-3 text-left">Distribution</th>
                  <th className="px-3 text-left">State</th>
                  <th className="px-3 text-left">WSL version</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {(st.data.distros ?? []).map((d) => (
                  <tr key={d.name} className="h-8 border-b border-line/60">
                    <td className="px-3 font-medium">{d.name}</td>
                    <td className="px-3">
                      <Badge tone={d.state === 'Running' ? 'ok' : 'neutral'}>{d.state}</Badge>
                    </td>
                    <td className="px-3">{d.version}</td>
                    <td className="px-3">{d.default && <Badge tone="accent">default</Badge>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <p className="border-t border-line p-3 text-xs text-muted">
            RUNTIDE only lists distributions. It never runs commands inside them or modifies them.
          </p>
        </>
      )}
    </Panel>
  )
}

function WindowsServices() {
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const [show, setShow] = useState(false)
  const [q, setQ] = useState('')
  const list = usePoll(
    () => (show ? call<WinService[]>('winservices.list') : Promise.resolve([])),
    [show],
    10,
  )
  const rows = (list.data ?? [])
    .filter((s) => !q || `${s.name} ${s.displayName}`.toLowerCase().includes(q.toLowerCase()))
    .slice(0, 200)
  const control = (s: WinService, action: 'start' | 'stop') =>
    ask({
      title: `${action === 'start' ? 'Start' : 'Stop'} Windows service ${s.displayName}?`,
      danger: action === 'stop',
      confirmLabel: action === 'start' ? 'Start service' : 'Stop service',
      body: (
        <div className="space-y-2">
          <p>
            This changes a <b>native Windows service</b> ({s.name}), not a RUNTIDE-managed process.
          </p>
          <p className="text-muted">
            Windows decides whether you are allowed. RUNTIDE does not request administrator rights.
          </p>
        </div>
      ),
      onConfirm: async () => {
        try {
          await call('winservices.control', { name: s.name, action, confirm: true })
          toast({ kind: 'success', title: `${s.name}: ${action} requested` })
        } catch (e) {
          reportError(`Could not ${action} ${s.name}`, e)
        }
        void list.reload()
      },
    })
  return (
    <Panel
      title="Native Windows services"
      actions={<Button onClick={() => setShow(!show)}>{show ? 'Hide' : 'Show list'}</Button>}
    >
      <div className="p-3 text-xs text-muted">
        Separate from RUNTIDE-managed development services. Listing is read-only; changing one asks
        for confirmation and needs rights you already hold.
      </div>
      {show && (
        <>
          <div className="px-3 pb-2">
            <Input
              aria-label="Filter Windows services"
              placeholder="Filter"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="w-64"
            />
          </div>
          {list.loading && !list.data ? (
            <Spinner />
          ) : (
            <div className="max-h-72 overflow-auto">
              <table aria-label="Windows services" className="w-full text-sm">
                <tbody>
                  {rows.map((s) => (
                    <tr key={s.name} className="h-8 border-t border-line/60">
                      <td className="px-3">
                        <div>{s.displayName}</div>
                        <div className="text-xs text-muted">{s.name}</div>
                      </td>
                      <td className="px-3">
                        <Badge tone={s.state === 'running' ? 'ok' : 'neutral'}>{s.state}</Badge>
                      </td>
                      <td className="px-3 text-right">
                        {s.protected ? (
                          <span className="text-xs text-muted">protected</span>
                        ) : s.state === 'running' ? (
                          <Button disabled={!s.canStop} onClick={() => control(s, 'stop')}>
                            Stop…
                          </Button>
                        ) : s.state === 'stopped' ? (
                          <Button onClick={() => control(s, 'start')}>Start…</Button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </Panel>
  )
}

export function ContainersPage() {
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Containers & WSL"
        subtitle="Optional integrations. Everything works without them."
      />
      <div className="grid gap-3 px-6 pb-6">
        <Docker />
        <Wsl />
        <WindowsServices />
      </div>
    </div>
  )
}
