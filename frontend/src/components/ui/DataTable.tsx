import { useMemo, useState, type ReactNode } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { cx } from './primitives'

export interface Column<T> {
  key: string
  header: string
  width?: string
  align?: 'left' | 'right'
  render: (row: T) => ReactNode
  /** Comparator; when present the column header is sortable. */
  sort?: (a: T, b: T) => number
  mono?: boolean
}

interface Props<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  selectedKey?: string | null
  onSelect?: (row: T) => void
  initialSort?: { key: string; dir: 'asc' | 'desc' }
  label: string
  empty?: ReactNode
}

/** Dense, keyboard-accessible, sortable table. */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  selectedKey,
  onSelect,
  initialSort,
  label,
  empty,
}: Props<T>) {
  const [sort, setSort] = useState(initialSort)
  const sorted = useMemo(() => {
    const col = columns.find((c) => c.key === sort?.key)
    if (!col?.sort || !sort) return rows
    const cmp = col.sort
    const dir = sort.dir === 'asc' ? 1 : -1
    return [...rows].sort((a, b) => cmp(a, b) * dir)
  }, [rows, sort, columns])

  const toggle = (c: Column<T>) => {
    if (!c.sort) return
    setSort((s) =>
      s?.key === c.key
        ? { key: c.key, dir: s.dir === 'asc' ? 'desc' : 'asc' }
        : { key: c.key, dir: 'asc' },
    )
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <table aria-label={label} className="w-full border-collapse text-sm">
        <thead className="sticky top-0 z-10 bg-surface">
          <tr className="border-b border-line">
            {columns.map((c) => {
              const active = sort?.key === c.key
              return (
                <th
                  key={c.key}
                  scope="col"
                  style={{ width: c.width }}
                  aria-sort={
                    active ? (sort?.dir === 'asc' ? 'ascending' : 'descending') : undefined
                  }
                  className={cx(
                    'h-8 px-3 text-xs font-semibold text-muted',
                    c.align === 'right' ? 'text-right' : 'text-left',
                  )}
                >
                  {c.sort ? (
                    <button
                      type="button"
                      onClick={() => toggle(c)}
                      className="inline-flex items-center gap-1 hover:text-ink"
                    >
                      {c.header}
                      {active &&
                        (sort?.dir === 'asc' ? (
                          <ArrowUp className="h-3 w-3" />
                        ) : (
                          <ArrowDown className="h-3 w-3" />
                        ))}
                    </button>
                  ) : (
                    c.header
                  )}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => {
            const k = rowKey(r)
            const selected = selectedKey === k
            return (
              <tr
                key={k}
                tabIndex={onSelect ? 0 : undefined}
                aria-selected={onSelect ? selected : undefined}
                onClick={() => onSelect?.(r)}
                onKeyDown={(e) => {
                  if (onSelect && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault()
                    onSelect(r)
                  }
                }}
                className={cx(
                  'h-8 border-b border-line/60',
                  onSelect && 'cursor-default hover:bg-subtle',
                  selected && 'bg-accent/10',
                )}
              >
                {columns.map((c) => (
                  <td
                    key={c.key}
                    className={cx(
                      'max-w-0 truncate px-3',
                      c.align === 'right' && 'text-right tabular-nums',
                      c.mono && 'font-mono text-xs',
                    )}
                  >
                    {c.render(r)}
                  </td>
                ))}
              </tr>
            )
          })}
        </tbody>
      </table>
      {sorted.length === 0 && empty}
    </div>
  )
}
