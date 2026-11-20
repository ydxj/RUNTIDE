import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { useUI } from '../../stores/ui'
import { NAV } from '../../app/nav'
import type { Execution, ReceiptRecord } from '../../types'
import { ExecutionsPage } from './ExecutionsPage'
import { ReceiptsPage } from './ReceiptsPage'
import { VerificationPage } from './VerificationPage'

const ex = (over: Partial<Execution>): Execution => ({
  id: 'run_1',
  workspaceId: 'ws_1',
  projectId: 'prj_1',
  task: 'fix login',
  state: 'COMPLETED',
  isolation: 2,
  createdAt: '2026-01-01T10:00:00Z',
  runtideVersion: '0.2.0',
  ...over,
})

let engine: ReturnType<typeof fakeEngine> | undefined
beforeEach(() => resetUI())
afterEach(() => engine?.restore())

describe('navigation', () => {
  it('has unique ids, shortcuts for the first nine and both groups', () => {
    expect(new Set(NAV.map((n) => n.id)).size).toBe(NAV.length)
    expect(new Set(NAV.map((n) => n.group))).toEqual(new Set(['Tasks', 'Machine']))
    expect(NAV[0]?.id).toBe('overview')
  })
})

describe('ExecutionsPage', () => {
  it('lists executions with state, verification and isolation', async () => {
    engine = fakeEngine({
      'runs.list': () => [
        ex({ verification: 'passed' }),
        ex({ id: 'run_2', task: 'broken build', state: 'FAILED', isolation: 1 }),
      ],
    })
    renderWithHosts(<ExecutionsPage />)
    const table = await screen.findByRole('table', { name: 'Executions' })
    expect(within(table).getByText('fix login')).toBeInTheDocument()
    expect(within(table).getByText('broken build')).toBeInTheDocument()
    expect(within(table).getByText('passed')).toBeInTheDocument()
    expect(within(table).getByText('worktree')).toBeInTheDocument()
    expect(within(table).getByText('managed')).toBeInTheDocument()
  })

  it('filters by state through the engine', async () => {
    engine = fakeEngine({ 'runs.list': () => [] })
    renderWithHosts(<ExecutionsPage />)
    await screen.findByText('No executions yet')
    await userEvent.selectOptions(screen.getByLabelText('Filter by state'), 'FAILED')
    await waitFor(() =>
      expect(
        engine!.calls.some(
          (c) => c.method === 'runs.list' && JSON.stringify(c.params).includes('FAILED'),
        ),
      ).toBe(true),
    )
  })

  it('opens the detail view for the selected execution', async () => {
    engine = fakeEngine({ 'runs.list': () => [ex({})] })
    renderWithHosts(<ExecutionsPage />)
    await userEvent.click(await screen.findByText('fix login'))
    expect(useUI.getState().pageParams['id']).toBe('run_1')
  })
})

describe('ReceiptsPage', () => {
  it('lists receipts and shows imported ones as such', async () => {
    const r: ReceiptRecord = {
      id: 'rc_1',
      executionId: 'run_1',
      projectName: 'shop',
      path: 'C:\\r\\a.runtide.zip',
      sha256: 'ab'.repeat(32),
      size: 1234,
      imported: true,
      status: 'COMPLETED',
      createdAt: '2026-01-01T10:00:00Z',
    }
    engine = fakeEngine({ 'receipts.list': () => [r] })
    renderWithHosts(<ReceiptsPage />)
    expect(await screen.findByText('shop')).toBeInTheDocument()
    expect(screen.getByText(/imported/i)).toBeInTheDocument()
  })
})

describe('VerificationPage', () => {
  it('counts verified, failed and unverified executions from recorded results', async () => {
    const detail = (x: Execution, overall: string) => ({
      execution: x,
      verification: overall === 'none' ? [] : [{ name: 'tests', status: overall }],
      verificationSummary: { overall },
    })
    engine = fakeEngine({
      'runs.list': () => [ex({}), ex({ id: 'run_2', state: 'FAILED' }), ex({ id: 'run_3' })],
      'runs.get': (p) => {
        const id = (p as { id: string }).id
        if (id === 'run_1') return detail(ex({}), 'passed')
        if (id === 'run_2') return detail(ex({ id: 'run_2', state: 'FAILED' }), 'failed')
        return detail(ex({ id: 'run_3' }), 'none')
      },
    })
    renderWithHosts(<VerificationPage />)
    expect(await screen.findByText('Not verified')).toBeInTheDocument()
    expect(screen.getByText('No verification results (', { exact: false })).toBeInTheDocument()
  })
})
