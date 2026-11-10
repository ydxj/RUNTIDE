import { call } from '../../services/api'
import { reportError, useUI } from '../../stores/ui'

interface Target {
  pid: number
  name: string
  startTime: string
  protection: string
  protectionNote?: string
  managed?: string
}

interface KillResult {
  terminated: boolean
  detail: string
}

/**
 * Ask for explicit confirmation, terminate through the engine, and report the
 * real outcome. Protected processes are refused before any dialog is shown.
 */
export function confirmKill(t: Target, onDone: () => void): void {
  const { ask, toast } = useUI.getState()
  if (t.protection === 'protected') {
    toast({ kind: 'error', title: `${t.name} is protected`, detail: t.protectionNote })
    return
  }
  ask({
    title: `Terminate ${t.name}?`,
    danger: true,
    confirmLabel: 'Terminate',
    body: (
      <div className="space-y-2">
        <p>
          This forcefully ends <b>{t.name}</b> (PID {t.pid}). Unsaved work in that process is lost.
        </p>
        {t.managed && (
          <p className="rounded border border-warn/40 bg-warn/10 p-2">
            It belongs to RUNTIDE service <b>{t.managed}</b>. Prefer stopping the service from the
            Services page.
          </p>
        )}
        {t.protection === 'caution' && (
          <p className="rounded border border-warn/40 bg-warn/10 p-2">{t.protectionNote}</p>
        )}
        <p className="text-muted">RUNTIDE does not elevate privileges; Windows may refuse.</p>
      </div>
    ),
    onConfirm: async () => {
      try {
        const r = await call<KillResult>('processes.kill', { pid: t.pid, startTime: t.startTime })
        toast({
          kind: r.terminated ? 'success' : 'error',
          title: r.terminated ? `${t.name} terminated` : `${t.name} was not terminated`,
          detail: r.detail,
        })
      } catch (e) {
        reportError(`Could not terminate ${t.name}`, e)
      } finally {
        onDone()
      }
    },
  })
}
