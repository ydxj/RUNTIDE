import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { ServiceForm } from './ServiceForm'

let engine: ReturnType<typeof fakeEngine>
beforeEach(() => resetUI())
afterEach(() => engine.restore())

describe('ServiceForm', () => {
  it('builds a spec from the fields and saves through the engine', async () => {
    engine = fakeEngine({ 'services.save': () => null })
    const onSaved = vi.fn()
    renderWithHosts(<ServiceForm initial={null} onClose={() => {}} onSaved={onSaved} />)
    await userEvent.type(screen.getByPlaceholderText('api'), 'web')
    await userEvent.type(screen.getByPlaceholderText('npm'), 'npm')
    await userEvent.type(screen.getByPlaceholderText('run dev'), 'run dev --port "3 000"')
    await userEvent.type(screen.getByPlaceholderText('PORT=3000'), 'PORT=3000')
    await userEvent.click(screen.getByRole('button', { name: 'Save service' }))
    const call = engine.calls.find((c) => c.method === 'services.save')
    expect(call?.params).toMatchObject({
      name: 'web',
      command: 'npm',
      args: ['run', 'dev', '--port', '3 000'],
      env: { PORT: '3000' },
    })
    expect(onSaved).toHaveBeenCalled()
  })

  it('keeps the dialog open and shows the engine validation error', async () => {
    engine = fakeEngine({
      'services.save': () => {
        throw new Error('validation failed:\n  - service "web": working directory escapes')
      },
    })
    const onSaved = vi.fn()
    renderWithHosts(<ServiceForm initial={null} onClose={() => {}} onSaved={onSaved} />)
    await userEvent.type(screen.getByPlaceholderText('api'), 'web')
    await userEvent.type(screen.getByPlaceholderText('npm'), 'npm')
    await userEvent.click(screen.getByRole('button', { name: 'Save service' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('working directory escapes')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('does not allow saving without a name and command', () => {
    engine = fakeEngine({})
    renderWithHosts(<ServiceForm initial={null} onClose={() => {}} onSaved={() => {}} />)
    expect(screen.getByRole('button', { name: 'Save service' })).toBeDisabled()
  })
})
