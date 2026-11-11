import { create } from 'zustand'
import type { Settings } from '../types'

export type PageId =
  | 'overview'
  | 'processes'
  | 'ports'
  | 'services'
  | 'workspaces'
  | 'logs'
  | 'resources'
  | 'environment'
  | 'containers'
  | 'doctor'
  | 'settings'

export interface Toast {
  id: number
  kind: 'info' | 'success' | 'error'
  title: string
  detail?: string
}

export interface ConfirmRequest {
  title: string
  body: React.ReactNode
  confirmLabel: string
  danger?: boolean
  onConfirm: () => void | Promise<void>
}

interface UIState {
  page: PageId
  /** Optional context handed to the target page, e.g. a search query. */
  pageParams: Record<string, string>
  theme: 'light' | 'dark' | 'system'
  settings: Settings | null
  toasts: Toast[]
  confirm: ConfirmRequest | null
  paletteOpen: boolean
  go: (page: PageId, params?: Record<string, string>) => void
  setTheme: (t: UIState['theme']) => void
  setSettings: (s: Settings) => void
  toast: (t: Omit<Toast, 'id'>) => void
  dismissToast: (id: number) => void
  ask: (c: ConfirmRequest) => void
  closeConfirm: () => void
  setPalette: (open: boolean) => void
}

let seq = 1

export const useUI = create<UIState>((set, get) => ({
  page: 'overview',
  pageParams: {},
  theme: 'light',
  settings: null,
  toasts: [],
  confirm: null,
  paletteOpen: false,
  go: (page, params = {}) => set({ page, pageParams: params }),
  setTheme: (theme) => set({ theme }),
  setSettings: (settings) => set({ settings, theme: settings.theme }),
  toast: (t) => {
    const id = seq++
    set({ toasts: [...get().toasts, { ...t, id }].slice(-5) })
    setTimeout(() => get().dismissToast(id), t.kind === 'error' ? 8000 : 4000)
  },
  dismissToast: (id) => set({ toasts: get().toasts.filter((x) => x.id !== id) }),
  ask: (confirm) => set({ confirm }),
  closeConfirm: () => set({ confirm: null }),
  setPalette: (paletteOpen) => set({ paletteOpen }),
}))

export function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/** Report a failed action to the user. */
export function reportError(title: string, e: unknown): void {
  useUI.getState().toast({ kind: 'error', title, detail: errMsg(e) })
}
