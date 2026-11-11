// All backend access goes through `call`. In the desktop app it forwards to the
// Go engine via the single Wails binding; tests install a fake transport.

export type Transport = (method: string, params?: unknown) => Promise<unknown>

interface WailsApp {
  Call(method: string, params: string): Promise<string>
  Info(): Promise<{ mode: string; version: string }>
  PickDirectory(title: string): Promise<string>
  PickFile(title: string): Promise<string>
  PickSaveFile(title: string, name: string): Promise<string>
  ReadTextFile(path: string): Promise<string>
  WriteTextFile(path: string, content: string): Promise<void>
  Quit(): Promise<void>
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void
}

declare global {
  interface Window {
    go?: { desktop?: { App?: WailsApp } }
    runtime?: WailsRuntime
  }
}

export class EngineError extends Error {}

function wailsApp(): WailsApp | undefined {
  return window.go?.desktop?.App
}

export const inDesktop = (): boolean => wailsApp() !== undefined

let transport: Transport = async (method, params) => {
  const app = wailsApp()
  if (!app)
    throw new EngineError('RUNTIDE engine is not available (not running inside the desktop app).')
  try {
    const raw = await app.Call(method, params === undefined ? '' : JSON.stringify(params))
    return raw ? JSON.parse(raw) : null
  } catch (e) {
    throw new EngineError(typeof e === 'string' ? e : e instanceof Error ? e.message : String(e))
  }
}

/** Replace the transport (tests only). Returns a restore function. */
export function setTransport(t: Transport): () => void {
  const prev = transport
  transport = t
  return () => {
    transport = prev
  }
}

export function call<T>(method: string, params?: unknown): Promise<T> {
  return transport(method, params) as Promise<T>
}

export const native = {
  pickDirectory: (title: string) => wailsApp()?.PickDirectory(title) ?? Promise.resolve(''),
  pickFile: (title: string) => wailsApp()?.PickFile(title) ?? Promise.resolve(''),
  pickSaveFile: (title: string, name: string) =>
    wailsApp()?.PickSaveFile(title, name) ?? Promise.resolve(''),
  readTextFile: (path: string) => wailsApp()?.ReadTextFile(path) ?? Promise.resolve(''),
  writeTextFile: (path: string, content: string) =>
    wailsApp()?.WriteTextFile(path, content) ?? Promise.resolve(),
  quit: () => wailsApp()?.Quit() ?? Promise.resolve(),
}

type Handler = (data: unknown) => void
const listeners = new Map<string, Set<Handler>>()

/** Subscribe to engine events ('service' | 'progress' | 'notify' | 'log'). */
export function onEngineEvent(topic: string, h: Handler): () => void {
  let set = listeners.get(topic)
  if (!set) listeners.set(topic, (set = new Set()))
  set.add(h)
  return () => {
    set?.delete(h)
  }
}

export function emitEngineEvent(topic: string, data: unknown): void {
  listeners.get(topic)?.forEach((h) => h(data))
}

let wired = false
/** Connect Wails runtime events to the in-page bus (idempotent). */
export function wireRuntimeEvents(): void {
  if (wired || !window.runtime) return
  wired = true
  window.runtime.EventsOn('runtide', (topic, data) => emitEngineEvent(String(topic), data))
  window.runtime.EventsOn('log-batch', (batch) => {
    if (Array.isArray(batch)) batch.forEach((e) => emitEngineEvent('log', e))
  })
}
