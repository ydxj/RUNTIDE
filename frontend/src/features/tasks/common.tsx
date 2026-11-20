import type { RunState } from '../../types'
import { Badge } from '../../components/ui/primitives'

export const ISOLATION: Record<number, string> = {
  1: 'managed',
  2: 'worktree',
  3: 'docker',
  4: 'wsl',
}

export const ISOLATION_HELP: Record<string, string> = {
  managed: 'Native Windows process in a Job Object. Does not isolate files or network.',
  worktree:
    'Separate working directory (git worktree or filtered copy). Processes can still reach your user profile.',
  docker: 'Container with only the workspace mounted. Not a guarantee against malicious code.',
  wsl: 'Runs inside a WSL2 distribution. Windows drives stay mounted: not a security boundary.',
}

export function stateTone(s: RunState | string): 'ok' | 'bad' | 'warn' | 'accent' | 'neutral' {
  switch (s) {
    case 'COMPLETED':
      return 'ok'
    case 'FAILED':
    case 'INTERRUPTED':
      return 'bad'
    case 'CANCELLED':
    case 'REQUIRES_ATTENTION':
      return 'warn'
    case 'RUNNING':
    case 'VERIFYING':
    case 'CONFIGURING':
      return 'accent'
    default:
      return 'neutral'
  }
}

export function StateBadge({ state }: { state: RunState | string }) {
  return <Badge tone={stateTone(state)}>{state}</Badge>
}

export const verdictTone = (v?: string) =>
  v === 'passed' ? 'ok' : v === 'failed' ? 'bad' : 'neutral'

export function statusTone(s: string): 'ok' | 'bad' | 'warn' | 'neutral' {
  if (s === 'passed') return 'ok'
  if (s === 'failed' || s === 'timed_out') return 'bad'
  if (s === 'skipped') return 'warn'
  return 'neutral'
}

export function ms(n: number | undefined): string {
  if (n == null) return '-'
  if (n < 1000) return `${n} ms`
  if (n < 60000) return `${(n / 1000).toFixed(1)} s`
  return `${Math.floor(n / 60000)}m ${Math.round((n % 60000) / 1000)}s`
}

export function List({
  title,
  items,
  tone,
}: {
  title: string
  items: string[] | null | undefined
  tone?: 'bad' | 'ok' | 'warn'
}) {
  if (!items || items.length === 0) return null
  const c =
    tone === 'bad' ? 'text-bad' : tone === 'ok' ? 'text-ok' : tone === 'warn' ? 'text-warn' : ''
  return (
    <div className="px-3 py-2 text-sm">
      <div className="mb-1 text-xs font-semibold text-muted">{title}</div>
      <ul className="selectable space-y-0.5">
        {items.map((i, k) => (
          <li key={k} className={`break-all font-mono text-xs ${c}`}>
            {i}
          </li>
        ))}
      </ul>
    </div>
  )
}
