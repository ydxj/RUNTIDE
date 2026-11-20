import { useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { since } from '../../services/format'
import { useUI } from '../../stores/ui'
import type { Execution, RunState } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  PageHeader,
  Select,
  Spinner,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { ISOLATION, StateBadge, verdictTone } from './common'
import { ExecutionDetailView } from './ExecutionDetail'

const STATES: RunState[] = [
  'CREATED',
  'CONFIGURING',
  'READY',
  'RUNNING',
  'VERIFYING',
  'COMPLETED',
  'FAILED',
  'CANCELLED',
  'INTERRUPTED',
  'REQUIRES_ATTENTION',
]

export function ExecutionsPage() {
  const params = useUI((s) => s.pageParams)
  const go = useUI((s) => s.go)
  const [state, setState] = useState('')
  const { data, error, loading, reload } = usePoll(
    () =>
      call<Execution[]>('runs.list', {
        workspace: params['workspace'] ?? '',
        states: state ? [state] : [],
        limit: 200,
      }),
    [state, params['workspace']],
    3,
  )
  const id = params['id']
  if (id) return <ExecutionDetailView id={id} onBack={() => go('executions')} />

  const cols: Column<Execution>[] = [
    {
      key: 'task',
      header: 'Task',
      sort: (a, b) => a.task.localeCompare(b.task),
      render: (x) => <span className="font-medium">{x.task || '(no description)'}</span>,
    },
    {
      key: 'state',
      header: 'State',
      width: '170px',
      render: (x) => <StateBadge state={x.state} />,
    },
    {
      key: 'ver',
      header: 'Verification',
      width: '120px',
      render: (x) =>
        x.verification ? (
          <Badge tone={verdictTone(x.verification)}>{x.verification}</Badge>
        ) : (
          <span className="text-muted">-</span>
        ),
    },
    { key: 'iso', header: 'Isolation', width: '100px', render: (x) => ISOLATION[x.isolation] },
    {
      key: 'created',
      header: 'Created',
      width: '110px',
      sort: (a, b) => a.createdAt.localeCompare(b.createdAt),
      render: (x) => since(x.createdAt),
    },
    { key: 'id', header: 'ID', width: '240px', mono: true, render: (x) => x.id },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title="Executions" subtitle="Every run of a task, with its recorded evidence.">
        <Select
          aria-label="Filter by state"
          value={state}
          onChange={(e) => setState(e.target.value)}
        >
          <option value="">All states</option>
          {STATES.map((s) => (
            <option key={s}>{s}</option>
          ))}
        </Select>
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={() => void reload()}>
          Refresh
        </Button>
      </PageHeader>
      <div className="mx-6 mb-4 flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        {loading && !data ? (
          <Spinner />
        ) : error ? (
          <ErrorState message={error} onRetry={() => void reload()} />
        ) : (
          <DataTable
            label="Executions"
            columns={cols}
            rows={data ?? []}
            rowKey={(x) => x.id}
            onSelect={(x) => go('executions', { id: x.id })}
            initialSort={{ key: 'created', dir: 'desc' }}
            empty={
              <EmptyState
                title="No executions yet"
                hint="Create a workspace and start an execution; its commands, output, services, file changes and verification are recorded here."
                action={
                  <Button variant="primary" onClick={() => go('workspaces')}>
                    Go to Workspaces
                  </Button>
                }
              />
            }
          />
        )}
      </div>
    </div>
  )
}
