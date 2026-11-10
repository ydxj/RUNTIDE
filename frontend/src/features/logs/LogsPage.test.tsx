import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { emitEngineEvent } from '../../services/api'
import type { LogEntry } from '../../types'
import { filterLogs, LogsPage } from './LogsPage'

const e = (
  id: number,
  service: string,
  line: string,
  level = '',
  stream: LogEntry['stream'] = 'stdout',
): LogEntry => ({
  id,
  service,
  time: '2026-01-01T10:00:00Z',
  stream,
  level,
  line,
})

describe('filterLogs', () => {
  const lines = [
    e(1, 'api', 'ready'),
    e(2, 'api', 'boom', 'error'),
    e(3, 'web', 'Compiled', 'info'),
    e(4, 'web', 'oops', '', 'stderr'),
  ]
  it('filters by service, level and text', () => {
    expect(filterLogs(lines, new Set(['web']), '', '')).toHaveLength(2)
    expect(filterLogs(lines, new Set(), 'error', '').map((l) => l.id)).toEqual([2, 4])
    expect(filterLogs(lines, new Set(), '', 'COMPILED').map((l) => l.id)).toEqual([3])
  })
})

describe('LogsPage', () => {
  let engine: ReturnType<typeof fakeEngine>
  beforeEach(() => resetUI())
  afterEach(() => engine.restore())

  it('loads history, streams new lines live, and can pause', async () => {
    engine = fakeEngine({
      'logs.recent': () => [e(1, 'api', 'first line')],
      'logs.services': () => ['api'],
      'services.list': () => [],
    })
    renderWithHosts(<LogsPage />)
    expect(await screen.findByText('first line')).toBeInTheDocument()

    act(() => emitEngineEvent('log', e(2, 'api', 'live line')))
    expect(await screen.findByText('live line')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Pause/ }))
    act(() => emitEngineEvent('log', e(3, 'api', 'while paused')))
    await new Promise((r) => setTimeout(r, 350))
    expect(screen.queryByText('while paused')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: /Resume/ }))
    expect(await screen.findByText('while paused')).toBeInTheDocument()
  })

  it('filters by service chip and search box', async () => {
    engine = fakeEngine({
      'logs.recent': () => [e(1, 'api', 'alpha'), e(2, 'web', 'beta')],
      'logs.services': () => ['api', 'web'],
      'services.list': () => [],
    })
    renderWithHosts(<LogsPage />)
    await screen.findByText('alpha')
    await userEvent.click(
      within(screen.getByRole('group', { name: 'Services' })).getByRole('button', { name: 'web' }),
    )
    expect(screen.queryByText('alpha')).toBeNull()
    expect(screen.getByText('beta')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Show all' }))
    await userEvent.type(screen.getByLabelText('Search logs'), 'alp')
    expect(screen.getByText('alpha')).toBeInTheDocument()
    expect(screen.queryByText('beta')).toBeNull()
  })
})
