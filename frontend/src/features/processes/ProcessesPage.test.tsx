import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { proc } from '../../test/fixtures'
import { ProcessesPage } from './ProcessesPage'

const data = [
  proc({ pid: 100, name: 'node.exe', memoryBytes: 200_000_000, managed: 'shop/frontend' }),
  proc({
    pid: 4,
    name: 'System',
    protection: 'protected',
    protectionNote: 'Windows kernel process',
  }),
  proc({ pid: 300, name: 'chrome.exe', memoryBytes: 900_000_000 }),
]

let engine: ReturnType<typeof fakeEngine>
beforeEach(() => resetUI())
afterEach(() => engine.restore())

describe('ProcessesPage', () => {
  it('lists real rows sorted by memory and passes the filter to the engine', async () => {
    engine = fakeEngine({
      'processes.list': (p) => {
        const q = (p as { query: string }).query
        return data.filter((d) => !q || d.name.includes(q))
      },
    })
    renderWithHosts(<ProcessesPage />)
    const table = await screen.findByRole('table', { name: 'Processes' })
    const names = within(table)
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '')
    expect(names[0]).toContain('chrome.exe')
    expect(names[1]).toContain('node.exe')
    expect(within(table).getByText('shop/frontend')).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText('Filter processes'), 'node')
    await waitFor(() =>
      expect(engine.calls.some((c) => (c.params as { query?: string })?.query === 'node')).toBe(
        true,
      ),
    )
    await waitFor(() =>
      expect(
        within(screen.getByRole('table', { name: 'Processes' })).queryByText('chrome.exe'),
      ).toBeNull(),
    )
  })

  it('disables termination for protected processes and explains why', async () => {
    engine = fakeEngine({ 'processes.list': () => data })
    renderWithHosts(<ProcessesPage />)
    await userEvent.click(await screen.findByText('System'))
    const btn = screen.getByRole('button', { name: /Terminate/ })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('title', 'Windows kernel process')
    expect(screen.getByText('Windows kernel process', { selector: 'p' })).toBeInTheDocument()
  })

  it('requires confirmation and reports what Windows actually did', async () => {
    engine = fakeEngine({
      'processes.list': () => data,
      'processes.kill': () => ({ terminated: false, detail: 'Windows denied access.' }),
    })
    renderWithHosts(<ProcessesPage />)
    await userEvent.click(await screen.findByText('chrome.exe'))
    await userEvent.click(screen.getByRole('button', { name: /Terminate/ }))
    const dialog = await screen.findByRole('dialog', { name: /Terminate chrome.exe/ })
    expect(engine.calls.some((c) => c.method === 'processes.kill')).toBe(false)
    await userEvent.click(within(dialog).getByRole('button', { name: 'Terminate' }))
    await waitFor(() => expect(engine.calls.some((c) => c.method === 'processes.kill')).toBe(true))
    const toast = await screen.findByRole('alert')
    expect(toast).toHaveTextContent('was not terminated')
    expect(toast).toHaveTextContent('Windows denied access.')
  })

  it('shows an error state when the engine fails', async () => {
    engine = fakeEngine({
      'processes.list': () => {
        throw new Error('engine exploded')
      },
    })
    renderWithHosts(<ProcessesPage />)
    expect(await screen.findByText('engine exploded')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Try again/ })).toBeInTheDocument()
  })
})
