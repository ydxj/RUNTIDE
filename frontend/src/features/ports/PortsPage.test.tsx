import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { listener, proc } from '../../test/fixtures'
import { useUI } from '../../stores/ui'
import { PortsPage } from './PortsPage'

const ls = [
  listener({
    port: 3000,
    pid: 100,
    processName: 'node.exe',
    managed: 'web',
    conflict: 'port is bound by multiple processes',
  }),
  listener({
    port: 3000,
    pid: 200,
    processName: 'old-server.exe',
    address: '0.0.0.0',
    exposure: 'all-interfaces',
    conflict: 'port is bound by multiple processes',
  }),
  listener({ port: 5432, pid: 300, processName: 'postgres.exe' }),
]

let engine: ReturnType<typeof fakeEngine>
beforeEach(() => resetUI())
afterEach(() => engine.restore())

describe('PortsPage', () => {
  it('shows listeners, exposure, conflicts and managed ownership', async () => {
    engine = fakeEngine({ 'ports.list': () => ls })
    renderWithHosts(<PortsPage />)
    const table = await screen.findByRole('table', { name: 'Listening ports' })
    expect(within(table).getByText('old-server.exe')).toBeInTheDocument()
    expect(within(table).getByText('web')).toBeInTheDocument()
    expect(within(table).getAllByText(/shared by several processes/)).toHaveLength(2)
    expect(within(table).getByText('all interfaces')).toBeInTheDocument()
  })

  it('navigates to the owning process', async () => {
    engine = fakeEngine({ 'ports.list': () => ls })
    renderWithHosts(<PortsPage />)
    await userEvent.click(await screen.findByText('postgres.exe'))
    await userEvent.click(screen.getByRole('button', { name: /Go to process/ }))
    expect(useUI.getState().page).toBe('processes')
    expect(useUI.getState().pageParams).toEqual({ query: '300' })
  })

  it('terminates only after confirmation, re-resolving the process first', async () => {
    engine = fakeEngine({
      'ports.list': () => ls,
      'processes.list': () => [proc({ pid: 200, name: 'old-server.exe' })],
      'processes.kill': () => ({ terminated: true, detail: 'Process exited.' }),
    })
    renderWithHosts(<PortsPage />)
    await userEvent.click(await screen.findByText('old-server.exe'))
    await userEvent.click(screen.getByRole('button', { name: /Terminate/ }))
    const dlg = await screen.findByRole('dialog')
    await userEvent.click(within(dlg).getByRole('button', { name: 'Terminate' }))
    await waitFor(() =>
      expect(engine.calls.find((c) => c.method === 'processes.kill')?.params).toMatchObject({
        pid: 200,
      }),
    )
    expect(await screen.findByText('old-server.exe terminated')).toBeInTheDocument()
  })
})
