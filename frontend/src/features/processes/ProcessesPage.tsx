import { useMemo, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  List,
  ListTree,
  RefreshCw,
  ShieldAlert,
  Skull,
} from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { bytes, since } from '../../services/format'
import { useUI } from '../../stores/ui'
import type { Process, ProcessNode } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Spinner,
  cx,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { confirmKill } from './killProcess'

function TreeRows({
  nodes,
  depth,
  open,
  toggle,
  sel,
  onSel,
  q,
}: {
  nodes: ProcessNode[]
  depth: number
  open: Set<number>
  toggle: (pid: number) => void
  sel: number | null
  onSel: (p: Process) => void
  q: string
}) {
  return (
    <>
      {nodes.map((n) => {
        const kids = n.children ?? []
        const expanded = open.has(n.pid)
        const hit = q && n.name.toLowerCase().includes(q)
        return (
          <div key={n.pid}>
            <div
              role="treeitem"
              aria-expanded={kids.length ? expanded : undefined}
              aria-selected={sel === n.pid}
              tabIndex={0}
              onClick={() => onSel(n)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') onSel(n)
                if (e.key === 'ArrowRight' && kids.length && !expanded) toggle(n.pid)
                if (e.key === 'ArrowLeft' && expanded) toggle(n.pid)
              }}
              className={cx(
                'flex h-7 cursor-default items-center gap-1 pr-3 text-sm hover:bg-subtle',
                sel === n.pid && 'bg-accent/10',
                hit && 'font-semibold',
              )}
              style={{ paddingLeft: 8 + depth * 16 }}
            >
              {kids.length > 0 ? (
                <button
                  type="button"
                  aria-label={expanded ? 'Collapse' : 'Expand'}
                  onClick={(e) => {
                    e.stopPropagation()
                    toggle(n.pid)
                  }}
                  className="h-4 w-4 text-muted"
                >
                  {expanded ? (
                    <ChevronDown className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                </button>
              ) : (
                <span className="w-4" />
              )}
              <span className="truncate">{n.name}</span>
              <span className="ml-1 text-xs text-muted tabular-nums">{n.pid}</span>
              {n.managed && <Badge tone="accent">{n.managed}</Badge>}
              {n.protection === 'protected' && (
                <ShieldAlert className="h-3.5 w-3.5 text-muted" aria-label="protected" />
              )}
              <span className="ml-auto text-xs text-muted tabular-nums">
                {bytes(n.memoryBytes)}
              </span>
            </div>
            {expanded && kids.length > 0 && (
              <TreeRows
                nodes={kids}
                depth={depth + 1}
                open={open}
                toggle={toggle}
                sel={sel}
                onSel={onSel}
                q={q}
              />
            )}
          </div>
        )
      })}
    </>
  )
}

export function ProcessesPage() {
  const params = useUI((s) => s.pageParams)
  const [query, setQuery] = useState(params['query'] ?? '')
  const [tree, setTree] = useState(false)
  const [sel, setSel] = useState<Process | null>(null)
  const [open, setOpen] = useState<Set<number>>(new Set())
  const list = usePoll(
    () => call<Process[]>('processes.list', { query: tree ? '' : query, cmdLine: true }),
    [query, tree],
  )
  const nodes = usePoll(
    () => (tree ? call<ProcessNode[]>('processes.tree') : Promise.resolve([])),
    [tree],
  )

  const cols: Column<Process>[] = useMemo(
    () => [
      {
        key: 'name',
        header: 'Name',
        sort: (a, b) => a.name.localeCompare(b.name),
        render: (p) => (
          <span className="flex items-center gap-1.5">
            {p.protection === 'protected' && (
              <ShieldAlert className="h-3.5 w-3.5 shrink-0 text-muted" aria-label="protected" />
            )}
            <span className="truncate">{p.name}</span>
            {p.managed && <Badge tone="accent">{p.managed}</Badge>}
          </span>
        ),
      },
      {
        key: 'pid',
        header: 'PID',
        width: '72px',
        align: 'right',
        sort: (a, b) => a.pid - b.pid,
        render: (p) => p.pid,
      },
      {
        key: 'ppid',
        header: 'Parent',
        width: '72px',
        align: 'right',
        sort: (a, b) => a.ppid - b.ppid,
        render: (p) => p.ppid,
      },
      {
        key: 'cpu',
        header: 'CPU',
        width: '72px',
        align: 'right',
        sort: (a, b) => a.cpuPercent - b.cpuPercent,
        render: (p) => p.cpuPercent.toFixed(1) + '%',
      },
      {
        key: 'mem',
        header: 'Memory',
        width: '92px',
        align: 'right',
        sort: (a, b) => a.memoryBytes - b.memoryBytes,
        render: (p) => bytes(p.memoryBytes),
      },
      {
        key: 'started',
        header: 'Started',
        width: '110px',
        sort: (a, b) => a.startTime.localeCompare(b.startTime),
        render: (p) => since(p.startTime),
      },
    ],
    [],
  )

  const toggle = (pid: number) =>
    setOpen((s) => {
      const n = new Set(s)
      if (n.has(pid)) n.delete(pid)
      else n.add(pid)
      return n
    })

  const loading = tree ? nodes.loading && !nodes.data : list.loading && !list.data
  const err = tree ? nodes.error : list.error
  const refresh = () => void (tree ? nodes.reload() : list.reload())

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Processes"
        subtitle={`${list.data?.length ?? 0} shown. CPU is sampled between refreshes.`}
      >
        <Input
          aria-label="Filter processes"
          placeholder="Filter by name, PID, path or command"
          className="w-72"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Button
          icon={tree ? <List className="h-3.5 w-3.5" /> : <ListTree className="h-3.5 w-3.5" />}
          onClick={() => setTree(!tree)}
        >
          {tree ? 'List' : 'Tree'}
        </Button>
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={refresh}>
          Refresh
        </Button>
      </PageHeader>
      <div className="flex min-h-0 flex-1 gap-3 px-6 pb-4">
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
          {loading ? (
            <Spinner label="Enumerating processes" />
          ) : err ? (
            <ErrorState message={err} onRetry={refresh} />
          ) : tree ? (
            <div
              role="tree"
              aria-label="Process tree"
              className="min-h-0 flex-1 overflow-auto py-1"
            >
              <TreeRows
                nodes={(nodes.data ?? []).filter(
                  (n) => !query || JSON.stringify(n).toLowerCase().includes(query.toLowerCase()),
                )}
                depth={0}
                open={open}
                toggle={toggle}
                sel={sel?.pid ?? null}
                onSel={setSel}
                q={query.toLowerCase()}
              />
            </div>
          ) : (
            <DataTable
              label="Processes"
              columns={cols}
              rows={list.data ?? []}
              rowKey={(p) => String(p.pid)}
              selectedKey={sel ? String(sel.pid) : null}
              onSelect={setSel}
              initialSort={{ key: 'mem', dir: 'desc' }}
              empty={
                <EmptyState
                  title="No processes match"
                  hint="Clear the filter to see everything running."
                />
              }
            />
          )}
        </div>
        {sel && (
          <aside
            aria-label="Process details"
            className="selectable w-80 shrink-0 overflow-auto rounded-lg border border-line bg-surface p-4 text-sm"
          >
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="text-base font-semibold">{sel.name}</div>
                <div className="text-muted">
                  PID {sel.pid} · parent {sel.ppid}
                </div>
              </div>
              {sel.protection && (
                <Badge tone={sel.protection === 'protected' ? 'neutral' : 'warn'}>
                  {sel.protection}
                </Badge>
              )}
            </div>
            <dl className="mt-3 space-y-2">
              <Detail
                k="Path"
                v={sel.path || (sel.accessible ? '-' : `unavailable (${sel.accessError})`)}
              />
              <Detail k="Command line" v={sel.cmdLine || 'unavailable for this process'} />
              <Detail
                k="Memory"
                v={`${bytes(sel.memoryBytes)} working set · ${bytes(sel.privateBytes)} private`}
              />
              <Detail
                k="CPU"
                v={`${sel.cpuPercent.toFixed(1)}% now · ${sel.cpuSeconds.toFixed(1)} s total`}
              />
              <Detail k="Threads" v={String(sel.threads)} />
              <Detail
                k="Started"
                v={sel.startTime ? new Date(sel.startTime).toLocaleString() : '-'}
              />
              {sel.managed && <Detail k="Managed by RUNTIDE" v={sel.managed} />}
            </dl>
            {sel.protectionNote && (
              <p className="mt-3 rounded border border-line bg-subtle p-2 text-xs text-muted">
                {sel.protectionNote}
              </p>
            )}
            <div className="mt-4 flex gap-2">
              <Button
                variant="danger"
                disabled={sel.protection === 'protected'}
                title={sel.protection === 'protected' ? sel.protectionNote : undefined}
                icon={<Skull className="h-3.5 w-3.5" />}
                onClick={() =>
                  confirmKill(sel, () => {
                    setSel(null)
                    refresh()
                  })
                }
              >
                Terminate…
              </Button>
              <Button onClick={() => setSel(null)}>Close</Button>
            </div>
          </aside>
        )}
      </div>
    </div>
  )
}

function Detail({ k, v }: { k: string; v: string }) {
  return (
    <div>
      <dt className="text-xs text-muted">{k}</dt>
      <dd className="break-all">{v}</dd>
    </div>
  )
}
