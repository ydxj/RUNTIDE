import { useState, type ReactNode } from 'react'
import { DatabaseBackup, RotateCcw } from 'lucide-react'
import { call } from '../../services/api'
import { bytes } from '../../services/format'
import { reportError, useUI } from '../../stores/ui'
import type { DbInfo, Settings } from '../../types'
import { usePoll } from '../../hooks/usePoll'
import { Button, Input, PageHeader, Panel, Select, Switch } from '../../components/ui/primitives'

function Row({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-6 border-b border-line px-4 py-3 last:border-0">
      <div>
        <div className="text-sm font-medium">{title}</div>
        {hint && <div className="max-w-md text-xs text-muted">{hint}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

export function SettingsPage() {
  const settings = useUI((s) => s.settings)
  const setSettings = useUI((s) => s.setSettings)
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const go = useUI((s) => s.go)
  const db = usePoll(() => call<DbInfo>('db.info'), [], 0)
  const [patterns, setPatterns] = useState((settings?.extraRedaction ?? []).join('\n'))
  const [pruneDays, setPruneDays] = useState(90)

  const update = async (patch: Partial<Settings>) => {
    try {
      setSettings(await call<Settings>('settings.set', patch))
    } catch (e) {
      reportError('Setting was not saved', e)
    }
  }

  if (!settings) return null
  const num = (key: keyof Settings, min: number, max: number) => (
    <Input
      type="number"
      aria-label={String(key)}
      className="w-24"
      min={min}
      max={max}
      defaultValue={Number(settings[key])}
      onBlur={(e) => {
        const v = Number(e.target.value)
        if (v !== settings[key]) void update({ [key]: v } as Partial<Settings>)
      }}
    />
  )

  const pruneOld = async () => {
    try {
      const n = await call<{ executions: number }>('runs.prune', {
        days: pruneDays,
        dryRun: true,
      })
      if (n.executions === 0) {
        toast({ kind: 'info', title: `No executions ended more than ${pruneDays} days ago` })
        return
      }
      ask({
        title: `Delete ${n.executions} recorded execution(s)?`,
        danger: true,
        confirmLabel: 'Delete executions',
        body: (
          <p>
            Executions that ended more than {pruneDays} days ago lose their recorded events, output,
            file changes and verification results. Receipt files you exported are kept. This cannot
            be undone.
          </p>
        ),
        onConfirm: async () => {
          try {
            const r = await call<{ executions: number }>('runs.prune', { days: pruneDays })
            toast({ kind: 'success', title: `Deleted ${r.executions} execution(s)` })
          } catch (e) {
            reportError('Could not delete executions', e)
          }
        },
      })
    } catch (e) {
      reportError('Could not count executions', e)
    }
  }

  const backup = async () => {
    try {
      const r = await call<{ path: string }>('db.backup')
      toast({ kind: 'success', title: 'Database backed up', detail: r.path })
    } catch (e) {
      reportError('Backup failed', e)
    }
  }
  const reset = () =>
    ask({
      title: 'Reset RUNTIDE to defaults?',
      danger: true,
      confirmLabel: 'Delete all data and reset',
      body: (
        <div className="space-y-2">
          <p>
            This deletes every workspace, service, log, history entry and setting stored by RUNTIDE.
          </p>
          <p className="text-muted">
            A safety backup is created first. Project files are never touched. Services must be
            stopped.
          </p>
        </div>
      ),
      onConfirm: async () => {
        try {
          const r = await call<{ backup: string }>('db.reset', { confirm: true })
          setSettings(await call<Settings>('settings.get'))
          toast({ kind: 'success', title: 'RUNTIDE was reset', detail: `Backup: ${r.backup}` })
          void db.reload()
        } catch (e) {
          reportError('Reset failed', e)
        }
      },
    })

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Settings"
        subtitle="Everything is stored locally. Nothing leaves this machine."
      />
      <div className="grid max-w-3xl gap-3 px-6 pb-8">
        <Panel title="Appearance & refresh">
          <Row title="Theme">
            <Select
              aria-label="Theme"
              value={settings.theme}
              onChange={(e) => void update({ theme: e.target.value as Settings['theme'] })}
            >
              <option value="light">Light</option>
              <option value="dark">Dark</option>
              <option value="system">Follow Windows</option>
            </Select>
          </Row>
          <Row
            title="Refresh interval (seconds)"
            hint="How often tables and the dashboard update. Polling pauses while the window is hidden."
          >
            {num('refreshSec', 1, 60)}
          </Row>
          <Row title="Resource sampling (seconds)" hint="CPU and memory history sampling interval.">
            {num('sampleSec', 1, 300)}
          </Row>
        </Panel>
        <Panel title="Logs">
          <Row title="Lines kept in memory per service">{num('logMemoryLines', 100, 100000)}</Row>
          <Row title="Keep saved logs for (days)">{num('logRetentionDays', 1, 365)}</Row>
          <Row title="Maximum saved log rows">{num('logMaxRows', 1000, 5000000)}</Row>
          <Row
            title="Redact secrets"
            hint="Masks passwords, tokens, API keys and credential patterns before logs are stored or shown."
          >
            <Switch
              label="Redact secrets"
              checked={settings.redaction}
              onChange={(v) => void update({ redaction: v })}
            />
          </Row>
          <div className="px-4 py-3">
            <label className="text-sm font-medium" htmlFor="extra-redaction">
              Extra redaction patterns (regular expressions, one per line)
            </label>
            <textarea
              id="extra-redaction"
              value={patterns}
              onChange={(e) => setPatterns(e.target.value)}
              onBlur={() =>
                void update({
                  extraRedaction: patterns
                    .split('\n')
                    .map((p) => p.trim())
                    .filter(Boolean),
                })
              }
              rows={2}
              className="mt-1 w-full rounded-md border border-line bg-surface p-2 font-mono text-xs"
            />
          </div>
        </Panel>
        <Panel title="Execution evidence">
          <Row
            title="Keep executions for (days)"
            hint="0 keeps every execution forever (the default). Otherwise ended executions older than this are deleted hourly. Exported receipts are never deleted."
          >
            {num('execRetentionDays', 0, 3650)}
          </Row>
          <Row
            title="Stored output per command (MB)"
            hint="Output beyond this is counted but not stored, and the command is marked truncated."
          >
            {num('execMaxOutputMB', 1, 256)}
          </Row>
          <Row
            title="Events per execution"
            hint="Further events are not stored; the timeline says so once."
          >
            {num('execMaxEvents', 1000, 1000000)}
          </Row>
          <div className="flex items-center gap-2 border-t border-line p-3 text-sm">
            <span>Delete executions that ended more than</span>
            <Input
              type="number"
              aria-label="Days"
              className="w-20"
              min={1}
              max={3650}
              value={pruneDays}
              onChange={(e) => setPruneDays(Number(e.target.value))}
            />
            <span>days ago</span>
            <Button variant="danger" onClick={() => void pruneOld()}>
              Review and delete…
            </Button>
          </div>
        </Panel>
        <Panel title="Security, MCP and server">
          <Row
            title="Receipt signing and trusted signers"
            hint="Create a signing key and choose which signer fingerprints you trust."
          >
            <Button onClick={() => go('integrations')}>Open Integrations</Button>
          </Row>
          <Row
            title="MCP server for coding agents"
            hint="Choose the permission level and copy the registration command. Agents can never approve commands."
          >
            <Button onClick={() => go('integrations')}>Open Integrations</Button>
          </Row>
          <Row
            title="Self-hosted server"
            hint="How to start the receipt server and push receipts to it. The server never executes commands."
          >
            <Button onClick={() => go('integrations')}>Open Integrations</Button>
          </Row>
        </Panel>
        <Panel title="Windows integration">
          <Row
            title="Minimise to tray when closing"
            hint="Closing the window keeps RUNTIDE and your services running in the notification area."
          >
            <Switch
              label="Minimise to tray"
              checked={settings.minimizeToTray}
              onChange={(v) => void update({ minimizeToTray: v })}
            />
          </Row>
          <Row
            title="Native notifications"
            hint="Notify when a service crashes or becomes unhealthy."
          >
            <Switch
              label="Notifications"
              checked={settings.notifications}
              onChange={(v) => void update({ notifications: v })}
            />
          </Row>
          <Row
            title="Start with Windows"
            hint="Adds a per-user startup entry (no administrator rights, no background service). Off by default."
          >
            <Switch
              label="Start with Windows"
              checked={settings.startWithWindows}
              onChange={(v) => void update({ startWithWindows: v })}
            />
          </Row>
        </Panel>
        <Panel title="Data">
          <Row title="Database" hint={db.data ? `${db.data.path}` : undefined}>
            <span className="text-sm text-muted">
              {db.data ? `${bytes(db.data.sizeBytes)} · schema v${db.data.schemaVersion}` : '…'}
            </span>
          </Row>
          {db.data && (
            <div className="selectable px-4 py-2 text-xs text-muted">
              {Object.entries(db.data.rows)
                .map(([k, v]) => `${k}: ${v}`)
                .join(' · ')}
              {db.data.recovered && <div className="mt-1 text-warn">{db.data.recovered}</div>}
            </div>
          )}
          <div className="flex gap-2 border-t border-line p-3">
            <Button icon={<DatabaseBackup className="h-3.5 w-3.5" />} onClick={() => void backup()}>
              Back up now
            </Button>
            <Button variant="danger" icon={<RotateCcw className="h-3.5 w-3.5" />} onClick={reset}>
              Reset to defaults…
            </Button>
          </div>
          <p className="px-4 pb-3 text-xs text-muted">
            Restore a backup with <code>runtide config restore &lt;file&gt;</code> while RUNTIDE is
            closed.
          </p>
        </Panel>
      </div>
    </div>
  )
}
