import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from './App'
import { fakeEngine, resetUI } from '../test/helpers'
import { DEFAULT_SETTINGS } from '../test/fixtures'
import { useUI } from '../stores/ui'

const overview = {
  processes: 321,
  listening: 17,
  services: { Total: 2, Running: 1, Failed: 0, Unhealthy: 0 },
  resources: {
    sample: {
      cpuPercent: 12,
      memPercent: 55,
      memUsed: 8e9,
      memTotal: 16e9,
      memAvailable: 8e9,
      commitUsed: 0,
      commitTotal: 0,
      time: '',
    },
    disks: [],
    interfaces: [],
    intervalSec: 2,
  },
  recentProcesses: [],
  recentFailures: [],
  workspaces: [],
  warnings: ['Drive D:\\ has less than 10% free space.'],
  generatedAt: new Date().toISOString(),
}

let engine: ReturnType<typeof fakeEngine>
beforeEach(() => {
  resetUI()
  engine = fakeEngine({
    'settings.get': () => DEFAULT_SETTINGS,
    'settings.set': (p) => ({ ...DEFAULT_SETTINGS, ...(p as object) }),
    overview: () => overview,
    'resources.history': () => [],
    'services.list': () => [
      {
        key: 'api',
        name: 'api',
        workspace: '',
        root: '',
        spec: { type: 'node' },
        runtime: { state: 'running' },
      },
    ],
    'processes.list': () => [],
    'ports.list': () => [],
  })
})
afterEach(() => engine.restore())

describe('App shell', () => {
  it('shows measured overview data and real warnings', async () => {
    render(<App />)
    expect(await screen.findByText('321')).toBeInTheDocument()
    expect(screen.getByText('17 listening ports')).toBeInTheDocument()
    expect(screen.getByText(/less than 10% free space/)).toBeInTheDocument()
  })

  it('navigates with the sidebar and marks the current page', async () => {
    render(<App />)
    await userEvent.click(screen.getByRole('button', { name: /^Ports/ }))
    expect(await screen.findByRole('heading', { name: 'Ports' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Ports/ })).toHaveAttribute('aria-current', 'page')
  })

  it('opens the command palette with Ctrl+K and runs a navigation command', async () => {
    render(<App />)
    await userEvent.keyboard('{Control>}k{/Control}')
    const box = await screen.findByLabelText('Search commands')
    await userEvent.type(box, 'doctor')
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(useUI.getState().page).toBe('doctor'))
  })

  it('starts a service from the palette', async () => {
    engine.restore()
    engine = fakeEngine({
      'settings.get': () => DEFAULT_SETTINGS,
      overview: () => overview,
      'resources.history': () => [],
      'services.list': () => [
        {
          key: 'api',
          name: 'api',
          workspace: '',
          root: '',
          spec: { type: 'node' },
          runtime: { state: 'stopped' },
        },
      ],
      'services.start': () => null,
    })
    render(<App />)
    await userEvent.keyboard('{Control>}k{/Control}')
    await userEvent.type(await screen.findByLabelText('Search commands'), 'start api')
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(engine.calls.some((c) => c.method === 'services.start')).toBe(true))
  })

  it('applies the dark theme and Ctrl+number shortcuts (Ctrl+4 opens Executions)', async () => {
    render(<App />)
    await userEvent.keyboard('{Control>}4{/Control}')
    await waitFor(() => expect(useUI.getState().page).toBe('executions'))
    useUI.getState().setTheme('dark')
    await waitFor(() => expect(document.documentElement.dataset['theme']).toBe('dark'))
  })

  it('shows a clear message when the engine is unreachable', async () => {
    engine.restore()
    engine = fakeEngine({
      'settings.get': () => {
        throw new Error('engine offline')
      },
    })
    render(<App />)
    expect(await screen.findByText('Cannot reach the RUNTIDE engine')).toBeInTheDocument()
    expect(screen.getByText('engine offline')).toBeInTheDocument()
  })
})
