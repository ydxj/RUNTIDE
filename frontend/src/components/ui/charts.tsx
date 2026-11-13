import { useId } from 'react'

/**
 * Minimal SVG area chart for 0..max series (percentages). Axis labels are
 * drawn so the chart can be read without hovering; values are real samples.
 */
export function AreaChart({
  values,
  max = 100,
  height = 96,
  label,
  stroke = 'rgb(var(--accent))',
  unit = '%',
}: {
  values: number[]
  max?: number
  height?: number
  label: string
  stroke?: string
  unit?: string
}) {
  const id = useId()
  const w = 400
  const pad = { l: 28, r: 4, t: 6, b: 4 }
  const iw = w - pad.l - pad.r
  const ih = height - pad.t - pad.b
  const n = values.length
  const pts = values.map((v, i) => {
    const x = pad.l + (n <= 1 ? iw : (i / (n - 1)) * iw)
    const y = pad.t + ih - (Math.min(Math.max(v, 0), max) / max) * ih
    return [x, y] as const
  })
  const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = n > 0 ? `${pad.l},${pad.t + ih} ${line} ${pad.l + iw},${pad.t + ih}` : ''
  return (
    <svg
      role="img"
      aria-label={`${label}: ${n ? values[n - 1]?.toFixed(0) : 'no data'}${unit}`}
      viewBox={`0 0 ${w} ${height}`}
      className="w-full"
      preserveAspectRatio="none"
    >
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={stroke} stopOpacity="0.22" />
          <stop offset="100%" stopColor={stroke} stopOpacity="0.02" />
        </linearGradient>
      </defs>
      {[0, 50, 100].map((g) => {
        const y = pad.t + ih - (g / 100) * ih
        return (
          <g key={g}>
            <line
              x1={pad.l}
              x2={w - pad.r}
              y1={y}
              y2={y}
              stroke="rgb(var(--line))"
              strokeWidth="1"
            />
            <text x={pad.l - 4} y={y + 3} textAnchor="end" fontSize="9" fill="rgb(var(--muted))">
              {Math.round((g / 100) * max)}
            </text>
          </g>
        )
      })}
      {n > 1 && <polygon points={area} fill={`url(#${id})`} />}
      {n > 1 && (
        <polyline
          points={line}
          fill="none"
          stroke={stroke}
          strokeWidth="1.6"
          vectorEffect="non-scaling-stroke"
        />
      )}
    </svg>
  )
}

export function Meter({ value, tone }: { value: number; tone?: 'ok' | 'warn' | 'bad' }) {
  const t = tone ?? (value > 90 ? 'bad' : value > 75 ? 'warn' : 'ok')
  const c = { ok: 'bg-accent', warn: 'bg-warn', bad: 'bg-bad' }[t]
  return (
    <div
      className="h-1.5 w-full overflow-hidden rounded-full bg-line"
      role="meter"
      aria-valuenow={Math.round(value)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div className={`h-full ${c}`} style={{ width: `${Math.min(100, Math.max(0, value))}%` }} />
    </div>
  )
}
