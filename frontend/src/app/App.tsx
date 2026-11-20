import { useEffect, useState } from 'react'
import { Search } from 'lucide-react'
import { NAV } from './nav'
import { CommandPalette } from './CommandPalette'
import { useUI, type PageId } from '../stores/ui'
import { call, inDesktop, wireRuntimeEvents } from '../services/api'
import type { Settings } from '../types'
import { ConfirmHost, Toasts } from '../components/ui/Overlays'
import { cx } from '../components/ui/primitives'
import { OverviewPage } from '../features/overview/OverviewPage'
import { ProcessesPage } from '../features/processes/ProcessesPage'
import { PortsPage } from '../features/ports/PortsPage'
import { ServicesPage } from '../features/services/ServicesPage'
import { StacksPage } from '../features/workspaces/WorkspacesPage'
import { ProjectsPage } from '../features/tasks/ProjectsPage'
import { TaskWorkspacesPage } from '../features/tasks/TaskWorkspacesPage'
import { ExecutionsPage } from '../features/tasks/ExecutionsPage'
import { LivePage } from '../features/tasks/LivePage'
import { VerificationPage } from '../features/tasks/VerificationPage'
import { ReceiptsPage } from '../features/tasks/ReceiptsPage'
import { ComparePage } from '../features/tasks/ComparePage'
import { IntegrationsPage } from '../features/tasks/IntegrationsPage'
import { LogsPage } from '../features/logs/LogsPage'
import { ResourcesPage } from '../features/resources/ResourcesPage'
import { EnvironmentPage } from '../features/environment/EnvironmentPage'
import { ContainersPage } from '../features/containers/ContainersPage'
import { DoctorPage } from '../features/doctor/DoctorPage'
import { SettingsPage } from '../features/settings/SettingsPage'

const PAGES: Record<PageId, () => JSX.Element | null> = {
  overview: OverviewPage,
  processes: ProcessesPage,
  ports: PortsPage,
  services: ServicesPage,
  stacks: StacksPage,
  projects: ProjectsPage,
  workspaces: TaskWorkspacesPage,
  executions: ExecutionsPage,
  live: LivePage,
  verification: VerificationPage,
  receipts: ReceiptsPage,
  compare: ComparePage,
  integrations: IntegrationsPage,
  logs: LogsPage,
  resources: ResourcesPage,
  environment: EnvironmentPage,
  containers: ContainersPage,
  doctor: DoctorPage,
  settings: SettingsPage,
}

function applyTheme(theme: Settings['theme']) {
  const dark =
    theme === 'dark' ||
    (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

export function App() {
  const page = useUI((s) => s.page)
  const go = useUI((s) => s.go)
  const theme = useUI((s) => s.theme)
  const setSettings = useUI((s) => s.setSettings)
  const setPalette = useUI((s) => s.setPalette)
  const [bootError, setBootError] = useState<string>()

  useEffect(() => {
    wireRuntimeEvents()
    call<Settings>('settings.get')
      .then(setSettings)
      .catch((e: unknown) => setBootError(e instanceof Error ? e.message : String(e)))
  }, [setSettings])

  useEffect(() => {
    applyTheme(theme)
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => applyTheme('system')
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [theme])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette(true)
      }
      if (e.ctrlKey && /^[1-9]$/.test(e.key)) {
        const item = NAV[Number(e.key) - 1]
        if (item) {
          e.preventDefault()
          go(item.id)
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [go, setPalette])

  const Page = PAGES[page]

  return (
    <div className="flex h-full">
      <nav aria-label="Primary" className="flex w-[216px] shrink-0 flex-col gap-0.5 px-2 pt-3 pb-2">
        <div className="mb-2 flex items-center gap-2 px-2.5 py-1">
          <img src="/icon.png" alt="" className="h-6 w-6" />
          <span className="text-[15px] font-semibold tracking-wide">RUNTIDE</span>
        </div>
        <button
          type="button"
          onClick={() => setPalette(true)}
          className="mb-2 flex h-8 items-center gap-2 rounded-md border border-line bg-surface px-2.5 text-sm text-muted hover:bg-subtle"
        >
          <Search className="h-3.5 w-3.5" aria-hidden />
          <span className="flex-1 text-left">Search</span>
          <kbd className="rounded border border-line px-1 text-[11px]">Ctrl K</kbd>
        </button>
        <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto">
          {NAV.map((n, i) => {
            const active = n.id === page
            return (
              <div key={n.id} className="flex flex-col gap-0.5">
                {(i === 0 || NAV[i - 1]?.group !== n.group) && (
                  <div className="px-2.5 pt-2 pb-1 text-[11px] font-semibold tracking-wide text-muted uppercase">
                    {n.group}
                  </div>
                )}
                <button
                  type="button"
                  aria-current={active ? 'page' : undefined}
                  onClick={() => go(n.id)}
                  title={i < 9 ? `${n.label} (Ctrl+${i + 1})` : n.label}
                  className={cx(
                    'relative flex h-8 items-center gap-3 rounded-md px-2.5 text-sm',
                    active
                      ? 'bg-surface font-medium shadow-[0_0_0_1px_rgb(var(--line))]'
                      : 'hover:bg-surface/70',
                  )}
                >
                  {active && (
                    <span className="absolute left-0 h-4 w-[3px] rounded-full bg-accent" />
                  )}
                  <n.icon className={cx('h-4 w-4', active && 'text-accent')} aria-hidden />
                  {n.label}
                </button>
              </div>
            )
          })}
        </div>
        <div className="px-2.5 pt-3 px-2.5 pt-3 text-xs text-muted">
          {inDesktop() ? 'Local engine' : 'No engine connected'}
        </div>
      </nav>
      <main className="m-2 ml-0 flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-subtle">
        {bootError ? (
          <div role="alert" className="m-6 rounded-lg border border-bad/40 bg-surface p-4">
            <div className="font-medium">Cannot reach the RUNTIDE engine</div>
            <div className="selectable mt-1 text-sm text-muted">{bootError}</div>
          </div>
        ) : (
          <div key={page} className="fade-in flex min-h-0 flex-1 flex-col">
            <Page />
          </div>
        )}
      </main>
      <CommandPalette />
      <ConfirmHost />
      <Toasts />
    </div>
  )
}
