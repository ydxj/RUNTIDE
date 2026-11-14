import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { setTransport, type Transport } from '../services/api'
import { useUI } from '../stores/ui'
import { ConfirmHost, Toasts } from '../components/ui/Overlays'
import { DEFAULT_SETTINGS } from './fixtures'

export interface Call {
  method: string
  params: unknown
}

/** Install a fake engine: handlers by method name; records every call. */
export function fakeEngine(handlers: Record<string, (params: unknown) => unknown>) {
  const calls: Call[] = []
  const transport: Transport = async (method, params) => {
    calls.push({ method, params })
    const h = handlers[method]
    if (!h) throw new Error(`unexpected engine call: ${method}`)
    return h(params)
  }
  const restore = setTransport(transport)
  return { calls, restore }
}

export function resetUI() {
  useUI.setState({
    page: 'overview',
    pageParams: {},
    settings: DEFAULT_SETTINGS,
    theme: 'light',
    toasts: [],
    confirm: null,
    paletteOpen: false,
  })
}

export function renderWithHosts(ui: ReactElement) {
  return render(
    <>
      {ui}
      <ConfirmHost />
      <Toasts />
    </>,
  )
}
