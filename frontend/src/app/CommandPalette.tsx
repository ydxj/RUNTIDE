import { useEffect, useMemo, useRef, useState } from 'react'
import { CornerDownLeft, Moon, RefreshCw, Stethoscope, type LucideIcon } from 'lucide-react'
import { NAV } from './nav'
import { useUI } from '../stores/ui'
import { call } from '../services/api'
import type { ServiceView, Settings } from '../types'
import { cx } from '../components/ui/primitives'

interface Cmd {
  id: string
  label: string
  hint: string
  icon: LucideIcon
  keywords: string
  run: () => void | Promise<void>
}

export function CommandPalette() {
  const open = useUI((s) => s.paletteOpen)
  const setOpen = useUI((s) => s.setPalette)
  const go = useUI((s) => s.go)
  const theme = useUI((s) => s.theme)
  const settings = useUI((s) => s.settings)
  const setSettings = useUI((s) => s.setSettings)
  const toast = useUI((s) => s.toast)
  const [q, setQ] = useState('')
  const [sel, setSel] = useState(0)
  const [services, setServices] = useState<ServiceView[]>([])
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setQ('')
    setSel(0)
    input.current?.focus()
    call<ServiceView[]>('services.list')
      .then((s) => setServices(s ?? []))
      .catch(() => setServices([]))
  }, [open])

  const cmds = useMemo<Cmd[]>(() => {
    const list: Cmd[] = NAV.map((n) => ({
      id: 'go-' + n.id,
      label: `Go to ${n.label}`,
      hint: 'Navigate',
      icon: n.icon,
      keywords: n.keywords,
      run: () => go(n.id),
    }))
    list.push(
      {
        id: 'doctor',
        label: 'Run diagnostics',
        hint: 'Action',
        icon: Stethoscope,
        keywords: 'doctor check problems',
        run: () => go('doctor', { autorun: '1' }),
      },
      {
        id: 'theme',
        label: theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme',
        hint: 'Action',
        icon: Moon,
        keywords: 'dark light appearance',
        run: async () => {
          if (!settings) return
          const next: Settings['theme'] = theme === 'dark' ? 'light' : 'dark'
          setSettings(await call<Settings>('settings.set', { theme: next }))
        },
      },
    )
    for (const s of services) {
      const running = s.runtime?.state === 'running'
      list.push({
        id: 'svc-' + s.key,
        label: `${running ? 'Stop' : 'Start'} ${s.key}`,
        hint: 'Service',
        icon: RefreshCw,
        keywords: s.spec.type,
        run: async () => {
          try {
            await call(running ? 'services.stop' : 'services.start', { key: s.key })
            toast({ kind: 'success', title: `${s.key} ${running ? 'stopped' : 'started'}` })
          } catch (e) {
            toast({
              kind: 'error',
              title: `Could not ${running ? 'stop' : 'start'} ${s.key}`,
              detail: String(e),
            })
          }
        },
      })
    }
    return list
  }, [go, theme, settings, setSettings, services, toast])

  const results = useMemo(() => {
    const t = q.trim().toLowerCase()
    if (!t) return cmds.slice(0, 12)
    return cmds.filter((c) => `${c.label} ${c.keywords}`.toLowerCase().includes(t)).slice(0, 12)
  }, [q, cmds])

  if (!open) return null
  const run = (c: Cmd | undefined) => {
    if (!c) return
    setOpen(false)
    void c.run()
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[14vh]"
      onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="fade-in w-[560px] overflow-hidden rounded-lg border border-line bg-surface shadow-2xl"
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false)
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setSel((s) => Math.min(s + 1, results.length - 1))
          }
          if (e.key === 'ArrowUp') {
            e.preventDefault()
            setSel((s) => Math.max(s - 1, 0))
          }
          if (e.key === 'Enter') run(results[sel])
        }}
      >
        <input
          ref={input}
          value={q}
          onChange={(e) => {
            setQ(e.target.value)
            setSel(0)
          }}
          placeholder="Type a command or page name…"
          aria-label="Search commands"
          className="h-12 w-full border-b border-line bg-transparent px-4 text-base outline-none"
        />
        <ul role="listbox" className="max-h-[320px] overflow-auto p-1">
          {results.length === 0 && (
            <li className="p-4 text-sm text-muted">No matching commands.</li>
          )}
          {results.map((c, i) => (
            <li
              key={c.id}
              role="option"
              aria-selected={i === sel}
              onMouseEnter={() => setSel(i)}
              onClick={() => run(c)}
              className={cx(
                'flex h-9 cursor-default items-center gap-3 rounded-md px-3 text-sm',
                i === sel && 'bg-accent/10',
              )}
            >
              <c.icon className="h-4 w-4 text-muted" aria-hidden />
              <span className="flex-1">{c.label}</span>
              <span className="text-xs text-muted">{c.hint}</span>
              {i === sel && <CornerDownLeft className="h-3 w-3 text-muted" aria-hidden />}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
