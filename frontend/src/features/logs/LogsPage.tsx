import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownToLine, Download, Pause, Play, Trash2 } from 'lucide-react'
import { call, native } from '../../services/api'
import { clock } from '../../services/format'
import { useEngineEvent } from '../../hooks/useEngineEvent'
import { reportError, useUI } from '../../stores/ui'
import type { LogEntry, ServiceView } from '../../types'
import { Button, EmptyState, Input, PageHeader, Select, cx } from '../../components/ui/primitives'

const MAX_LINES = 5000
const ROW = 20

export function filterLogs(
  lines: LogEntry[],
  services: Set<string>,
  level: string,
  q: string,
): LogEntry[] {
  const t = q.trim().toLowerCase()
  return lines.filter(
    (l) =>
      (services.size === 0 || services.has(l.service)) &&
      (!level || l.level === level || (level === 'error' && l.stream === 'stderr' && !l.level)) &&
      (!t || l.line.toLowerCase().includes(t)),
  )
}

export function LogsPage() {
  const params = useUI((s) => s.pageParams)
  const toast = useUI((s) => s.toast)
  const [lines, setLines] = useState<LogEntry[]>([])
  const [paused, setPaused] = useState(false)
  const [frozen, setFrozen] = useState<LogEntry[] | null>(null)
  const [follow, setFollow] = useState(true)
  const [chosen, setChosen] = useState<Set<string>>(
    new Set(params['service'] ? [params['service']] : []),
  )
  const [known, setKnown] = useState<string[]>([])
  const [level, setLevel] = useState('')
  const [q, setQ] = useState('')
  const box = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [height, setHeight] = useState(500)
  const pending = useRef<LogEntry[]>([])

  const loadKnown = useCallback(() => {
    void call<string[]>('logs.services')
      .then((s) => setKnown(s ?? []))
      .catch(() => undefined)
    void call<ServiceView[]>('services.list')
      .then((s) =>
        setKnown((k) => Array.from(new Set([...k, ...(s ?? []).map((x) => x.key)])).sort()),
      )
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    loadKnown()
    call<LogEntry[]>('logs.recent', { n: MAX_LINES })
      .then((l) => setLines(l ?? []))
      .catch((e) => reportError('Could not load logs', e))
  }, [loadKnown])

  // Incoming lines are buffered and flushed at most every 200 ms to keep rendering cheap.
  useEngineEvent<LogEntry>('log', (e) => pending.current.push(e))
  useEffect(() => {
    const t = setInterval(() => {
      if (pending.current.length === 0) return
      const batch = pending.current
      pending.current = []
      setLines((l) => {
        const next = l.concat(batch)
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
      })
    }, 200)
    return () => clearInterval(t)
  }, [])

  const source = paused && frozen ? frozen : lines
  const shown = useMemo(() => filterLogs(source, chosen, level, q), [source, chosen, level, q])

  useEffect(() => {
    const el = box.current
    if (!el) return
    setHeight(el.clientHeight)
    const ro = new ResizeObserver(() => setHeight(el.clientHeight))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  useEffect(() => {
    if (follow && !paused && box.current) box.current.scrollTop = box.current.scrollHeight
  }, [shown, follow, paused])

  const start = Math.max(0, Math.floor(scrollTop / ROW) - 10)
  const end = Math.min(shown.length, Math.ceil((scrollTop + height) / ROW) + 10)

  const togglePause = () => {
    if (!paused) setFrozen(lines)
    else setFrozen(null)
    setPaused(!paused)
  }
  const toggleService = (s: string) =>
    setChosen((c) => {
      const n = new Set(c)
      if (n.has(s)) n.delete(s)
      else n.add(s)
      return n
    })

  const exportLogs = async () => {
    try {
      const p = await native.pickSaveFile('Export logs', 'runtide-logs.txt')
      if (!p) return
      await call('logs.export', { services: Array.from(chosen), n: 0, path: p })
      toast({ kind: 'success', title: 'Logs exported', detail: p })
    } catch (e) {
      reportError('Export failed', e)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Logs"
        subtitle="Output of processes RUNTIDE launched, secrets redacted. Not Windows Event Logs."
      >
        <Input
          aria-label="Search logs"
          placeholder="Search"
          className="w-52"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <Select aria-label="Level filter" value={level} onChange={(e) => setLevel(e.target.value)}>
          <option value="">All levels</option>
          <option value="error">Errors</option>
          <option value="warn">Warnings</option>
          <option value="info">Info</option>
          <option value="debug">Debug</option>
        </Select>
        <Button
          icon={paused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          onClick={togglePause}
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
            setLines([])
            setFrozen(null)
            void call('logs.clear', { services: Array.from(chosen) })
          }}
        >
          Clear
        </Button>
        <Button icon={<Download className="h-3.5 w-3.5" />} onClick={() => void exportLogs()}>
          Export…
        </Button>
      </PageHeader>
      <div
        className="flex flex-wrap items-center gap-1.5 px-6 pb-2"
        role="group"
        aria-label="Services"
      >
        {known.length === 0 && (
          <span className="text-xs text-muted">No service has produced output yet.</span>
        )}
        {known.map((s) => (
          <button
            key={s}
            type="button"
            aria-pressed={chosen.has(s)}
            onClick={() => toggleService(s)}
            className={cx(
              'rounded-full border px-2.5 py-0.5 text-xs',
              chosen.has(s)
                ? 'border-accent bg-accent/10 text-accent'
                : 'border-line bg-surface hover:bg-subtle',
            )}
          >
            {s}
          </button>
        ))}
        {chosen.size > 0 && (
          <button
            type="button"
            className="px-1 text-xs text-accent"
            onClick={() => setChosen(new Set())}
          >
            Show all
          </button>
        )}
      </div>
      <div className="mx-6 mb-4 flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        {shown.length === 0 ? (
          <EmptyState
            title="No log lines"
            hint="Start a service and its output appears here live. Output is kept in a bounded buffer and saved to the local database."
          />
        ) : (
          <div
            ref={box}
            role="log"
            aria-label="Log output"
            className="selectable min-h-0 flex-1 overflow-auto font-mono text-xs"
            onScroll={(e) => {
              const el = e.currentTarget
              setScrollTop(el.scrollTop)
              if (el.scrollHeight - el.scrollTop - el.clientHeight > 60 && follow) setFollow(false)
            }}
          >
            <div style={{ height: shown.length * ROW, position: 'relative' }}>
              {shown.slice(start, end).map((l, i) => (
                <div
                  key={l.id}
                  className={cx(
                    'absolute right-0 left-0 flex gap-2 whitespace-pre px-3 leading-5',
                    l.level === 'error' && 'bg-bad/10 text-bad',
                    l.level === 'warn' && 'bg-warn/10 text-warn',
                    l.stream === 'system' && 'text-muted italic',
                  )}
                  style={{ top: (start + i) * ROW, height: ROW }}
                >
                  <span className="shrink-0 text-muted">{clock(l.time)}</span>
                  <span className="w-44 shrink-0 truncate text-accent">{l.service}</span>
                  <span className="truncate">{l.line}</span>
                </div>
              ))}
            </div>
          </div>
        )}
        <div className="flex h-7 items-center justify-between border-t border-line px-3 text-xs text-muted">
          <span>
            {shown.length.toLocaleString()} lines{paused && ' · paused'}
          </span>
          <span>Memory buffer capped at {MAX_LINES.toLocaleString()} lines</span>
        </div>
      </div>
    </div>
  )
}
