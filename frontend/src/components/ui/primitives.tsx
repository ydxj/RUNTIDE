import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
} from 'react'
import { AlertTriangle, Inbox, Loader2, RefreshCw } from 'lucide-react'

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ')
}

type Variant = 'default' | 'primary' | 'danger' | 'ghost'

export function Button({
  variant = 'default',
  icon,
  children,
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; icon?: ReactNode }) {
  const styles: Record<Variant, string> = {
    default: 'border border-line bg-surface hover:bg-subtle active:bg-line/60',
    primary: 'border border-accent bg-accent text-accent-ink hover:opacity-90',
    danger: 'border border-bad/40 bg-surface text-bad hover:bg-bad/10',
    ghost: 'border border-transparent hover:bg-subtle active:bg-line/60',
  }
  return (
    <button
      type="button"
      {...rest}
      className={cx(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-sm transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-50',
        styles[variant],
        className,
      )}
    >
      {icon}
      {children}
    </button>
  )
}

export function IconButton({
  label,
  children,
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      {...rest}
      className={cx(
        'inline-flex h-8 w-8 items-center justify-center rounded-md hover:bg-subtle active:bg-line/60',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
    >
      {children}
    </button>
  )
}

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...rest}
      className={cx(
        'h-8 rounded-md border border-line bg-surface px-2.5 text-sm placeholder:text-muted/70',
        'focus:border-accent',
        className,
      )}
    />
  )
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...rest}
      className={cx('h-8 rounded-md border border-line bg-surface px-2 text-sm', className)}
    >
      {children}
    </select>
  )
}

export function Switch({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        'relative h-5 w-10 shrink-0 rounded-full border transition-colors disabled:opacity-50',
        checked ? 'border-accent bg-accent' : 'border-muted/60 bg-transparent',
      )}
    >
      <span
        className={cx(
          'absolute top-0.5 h-3.5 w-3.5 rounded-full transition-all',
          checked ? 'left-[22px] bg-accent-ink' : 'left-0.5 bg-muted',
        )}
      />
    </button>
  )
}

type Tone = 'neutral' | 'ok' | 'warn' | 'bad' | 'accent'
export function Badge({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
  const t: Record<Tone, string> = {
    neutral: 'bg-subtle text-muted border-line',
    ok: 'bg-ok/10 text-ok border-ok/30',
    warn: 'bg-warn/10 text-warn border-warn/30',
    bad: 'bg-bad/10 text-bad border-bad/30',
    accent: 'bg-accent/10 text-accent border-accent/30',
  }
  return (
    <span
      className={cx('inline-flex items-center rounded border px-1.5 text-xs leading-5', t[tone])}
    >
      {children}
    </span>
  )
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-2 p-6 text-muted">
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
      <span>{label}…</span>
    </div>
  )
}

export function EmptyState({
  title,
  hint,
  action,
}: {
  title: string
  hint?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center gap-2 p-10 text-center text-muted">
      <Inbox className="h-8 w-8 opacity-60" aria-hidden />
      <div className="font-medium text-ink">{title}</div>
      {hint && <div className="max-w-md text-sm">{hint}</div>}
      {action}
    </div>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-col items-center gap-2 p-10 text-center">
      <AlertTriangle className="h-8 w-8 text-bad" aria-hidden />
      <div className="font-medium">Something went wrong</div>
      <div className="selectable max-w-xl text-sm text-muted">{message}</div>
      {onRetry && (
        <Button icon={<RefreshCw className="h-3.5 w-3.5" />} onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  )
}

export function Panel({
  title,
  actions,
  children,
  className,
}: {
  title?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cx('rounded-lg border border-line bg-surface', className)}>
      {(title || actions) && (
        <header className="flex h-10 items-center justify-between border-b border-line px-3">
          <h2 className="text-sm font-semibold">{title}</h2>
          <div className="flex items-center gap-1">{actions}</div>
        </header>
      )}
      {children}
    </section>
  )
}

export function PageHeader({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="flex min-h-[56px] items-center justify-between gap-4 px-6 pt-4 pb-2">
      <div>
        <h1 className="text-lg font-semibold leading-6">{title}</h1>
        {subtitle && <p className="text-sm text-muted">{subtitle}</p>}
      </div>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  )
}

export function StatusDot({ tone }: { tone: Tone }) {
  const c: Record<Tone, string> = {
    neutral: 'bg-muted/50',
    ok: 'bg-ok',
    warn: 'bg-warn',
    bad: 'bg-bad',
    accent: 'bg-accent',
  }
  return <span aria-hidden className={cx('inline-block h-2 w-2 rounded-full', c[tone])} />
}
