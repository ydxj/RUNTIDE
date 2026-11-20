import { useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownToLine, Pause, Play, Trash2 } from 'lucide-react'
import { useEngineEvent } from '../../hooks/useEngineEvent'
import { clock } from '../../services/format'
import type { ExecEvent } from '../../types'
import { Button, EmptyState, Input, PageHeader, Select, cx } from '../../components/ui/primitives'

const MAX = 3000

interface OutputEvent {
  executionId: string
  commandId: string
  name: string
  stream: 'stdout' | 'stderr'
  line: string
  time: string
}
type Row = {
  key: string
  time: string
  exec: string
  kind: string
  subject: string
  text: string
  severity: string
}

/** Live stream of everything the recorder observes, across all executions. */
export function LivePage() {
  const [rows, setRows] = useState<Row[]>([])
  const [paused, setPaused] = useState(false)
  const [frozen, setFrozen] = useState<Row[] | null>(null)
  const [follow, setFollow] = useState(true)
  const [q, setQ] = useState('')
  const [kind, setKind] = useState('')
  const pending = useRef<Row[]>([])
  const seq = useRef(0)
  const box = useRef<HTMLDivElement>(null)

  useEngineEvent<ExecEvent>('exec.event', (e) => {
    pending.current.push({
      key: `e${seq.current++}`,
      time: e.time,
      exec: e.executionId,
      kind: e.type,
      subject: e.subject ?? '',
      text: e.data ? JSON.stringify(e.data) : '',
      severity: e.severity,
    })
  })
  useEngineEvent<OutputEvent>('exec.output', (o) => {
    pending.current.push({
      key: `o${seq.current++}`,
      time: o.time,
      exec: o.executionId,
      kind: o.stream,
      subject: o.name,
      text: o.line,
      severity: o.stream === 'stderr' ? 'warn' : 'info',
    })
  })
  useEffect(() => {
    const t = setInterval(() => {
      if (pending.current.length === 0) return
      const b = pending.current
      pending.current = []
      setRows((r) => r.concat(b).slice(-MAX))
    }, 200)
    return () => clearInterval(t)
  }, [])
  const src = paused && frozen ? frozen : rows
  const shown = useMemo(() => {
    const t = q.trim().toLowerCase()
    return src.filter(
      (r) =>
        (!kind ||
          r.kind === kind ||
          (kind === 'output' && (r.kind === 'stdout' || r.kind === 'stderr'))) &&
        (!t || `${r.subject} ${r.text} ${r.kind}`.toLowerCase().includes(t)),
    )
  }, [src, q, kind])
  useEffect(() => {
    if (follow && !paused && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [shown, follow, paused])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Live Activity"
        subtitle="Events and output from executions, as they happen. Nothing is shown from before you opened this page; use an execution's Timeline for history."
      >
        <Input
          aria-label="Search live activity"
          placeholder="Search"
          className="w-52"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <Select aria-label="Kind" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="">Everything</option>
          <option value="output">Command output</option>
          <option value="command.started">Command started</option>
          <option value="command.finished">Command finished</option>
          <option value="service.unhealthy">Service unhealthy</option>
          <option value="verification.failed">Verification failed</option>
          <option value="state.changed">State changes</option>
        </Select>
        <Button
          icon={paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          onClick={() => {
            if (!paused) setFrozen(rows)
            else setFrozen(null)
            setPaused(!paused)
          }}
        >
          {paused ? 'Resume' : 'Pause'}
        </Button>
        <Button
          aria-pressed={follow}
          icon={<ArrowDownToLine className="h-3.5 w-3.5" />}
          className={cx(follow && 'border-accent text-accent')}
          onClick={() => setFollow(!follow)}
        >
          Follow
        </Button>
        <Button
          icon={<Trash2 className="h-3.5 w-3.5" />}
          onClick={() => {
            setRows([])
            setFrozen(null)
          }}
        >
          Clear
        </Button>
      </PageHeader>
      <div className="mx-6 mb-4 flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        {shown.length === 0 ? (
          <EmptyState
            title="Waiting for activity"
            hint="Start an execution and its events and output stream here."
          />
        ) : (
          <div
            ref={box}
            role="log"
            aria-label="Live activity"
            className="selectable min-h-0 flex-1 overflow-auto font-mono text-xs"
          >
            {shown.map((r) => (
              <div
                key={r.key}
                className={cx(
                  'flex gap-2 px-3 py-0.5 leading-5',
                  r.severity === 'error' && 'bg-bad/10 text-bad',
                  r.severity === 'warn' && 'text-warn',
                )}
              >
                <span className="shrink-0 text-muted">{clock(r.time)}</span>
                <span className="w-16 shrink-0 truncate text-muted" title={r.exec}>
                  {r.exec.slice(3, 9)}
                </span>
                <span className="w-40 shrink-0 truncate text-accent">{r.kind}</span>
                <span className="w-28 shrink-0 truncate">{r.subject}</span>
                <span className="min-w-0 truncate">{r.text}</span>
              </div>
            ))}
          </div>
        )}
        <div className="flex h-7 items-center justify-between border-t border-line px-3 text-xs text-muted">
          <span>
            {shown.length.toLocaleString()} rows{paused && ' · paused'}
          </span>
          <span>Bounded to the last {MAX.toLocaleString()} rows</span>
        </div>
      </div>
    </div>
  )
}
