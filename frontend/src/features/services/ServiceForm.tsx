import { useState } from 'react'
import { FolderOpen } from 'lucide-react'
import type { HealthSpec, ServiceSpec } from '../../types'
import { joinArgs, parseEnvLines, splitArgs } from '../../services/format'
import { call, native } from '../../services/api'
import { reportError, useUI } from '../../stores/ui'
import { Button, IconButton, Input, Select } from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'

const TYPES = [
  'custom',
  'node',
  'nextjs',
  'php',
  'laravel',
  'python',
  'django',
  'flask',
  'java',
  'worker',
]

export function blankService(): ServiceSpec {
  return {
    name: '',
    type: 'custom',
    command: '',
    args: [],
    workingDirectory: '',
    env: {},
    dependsOn: [],
    health: null,
    restart: { policy: 'never', maxRetries: 3, backoffMs: 1000, maxBackoffMs: 30000 },
    stopTimeoutSec: 10,
    startTimeoutSec: 60,
    autostart: false,
  }
}

function Field({
  label,
  children,
  hint,
}: {
  label: string
  children: React.ReactNode
  hint?: string
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-muted">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-muted">{hint}</span>}
    </label>
  )
}

/** Create or edit a standalone service. Validation is performed by the engine. */
export function ServiceForm({
  initial,
  onClose,
  onSaved,
}: {
  initial: ServiceSpec | null
  onClose: () => void
  onSaved: () => void
}) {
  const editing = initial !== null
  const base = initial ?? blankService()
  const [name, setName] = useState(base.name)
  const [type, setType] = useState(base.type)
  const [command, setCommand] = useState(base.command)
  const [args, setArgs] = useState(joinArgs(base.args))
  const [dir, setDir] = useState(base.workingDirectory)
  const [env, setEnv] = useState(
    Object.entries(base.env ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join('\n'),
  )
  const [policy, setPolicy] = useState(base.restart.policy)
  const [retries, setRetries] = useState(base.restart.maxRetries)
  const [hType, setHType] = useState<HealthSpec['type'] | 'none'>(base.health?.type ?? 'none')
  const [hUrl, setHUrl] = useState(base.health?.url ?? 'http://localhost:3000')
  const [hPort, setHPort] = useState(base.health?.port ?? 3000)
  const [hCmd, setHCmd] = useState(base.health?.command ?? '')
  const [interval, setIntervalSec] = useState(base.health?.intervalSec ?? 10)
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)
  const toast = useUI((s) => s.toast)

  const save = async () => {
    setBusy(true)
    setErr(undefined)
    const health: HealthSpec | null =
      hType === 'none'
        ? null
        : {
            type: hType,
            url: hType === 'http' ? hUrl : undefined,
            port: hType === 'tcp' ? hPort : undefined,
            command: hType === 'command' ? hCmd : undefined,
            intervalSec: interval,
            timeoutSec: 3,
            startPeriodSec: 0,
            failureThreshold: 3,
          }
    const spec: ServiceSpec = {
      ...base,
      name: name.trim(),
      type,
      command: command.trim(),
      args: splitArgs(args),
      workingDirectory: dir.trim(),
      env: parseEnvLines(env),
      health,
      restart: { ...base.restart, policy, maxRetries: retries },
    }
    try {
      await call('services.save', spec)
      toast({ kind: 'success', title: `Service ${spec.name} saved` })
      onSaved()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const pick = async () => {
    try {
      const d = await native.pickDirectory('Choose working directory')
      if (d) setDir(d)
    } catch (e) {
      reportError('Could not open folder picker', e)
    }
  }

  return (
    <Modal
      title={editing ? `Edit ${base.name}` : 'Add service'}
      onClose={onClose}
      width="max-w-2xl"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={busy || !name || !command}
            onClick={() => void save()}
          >
            Save service
          </Button>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-3">
        <Field label="Name">
          <Input
            className="w-full"
            value={name}
            disabled={editing}
            onChange={(e) => setName(e.target.value)}
            placeholder="api"
          />
        </Field>
        <Field label="Type">
          <Select className="w-full" value={type} onChange={(e) => setType(e.target.value)}>
            {TYPES.map((t) => (
              <option key={t}>{t}</option>
            ))}
          </Select>
        </Field>
        <Field
          label="Executable"
          hint="Run directly, never through a shell. e.g. npm, php, python, C:\tools\app.exe"
        >
          <Input
            className="w-full"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder="npm"
          />
        </Field>
        <Field label="Arguments" hint='Space separated; use "quotes" for values with spaces'>
          <Input
            className="w-full"
            value={args}
            onChange={(e) => setArgs(e.target.value)}
            placeholder="run dev"
          />
        </Field>
        <div className="col-span-2">
          <Field label="Working directory (absolute path)">
            <div className="flex gap-1">
              <Input
                className="flex-1"
                value={dir}
                onChange={(e) => setDir(e.target.value)}
                placeholder="D:\projects\app"
              />
              <IconButton label="Browse for folder" onClick={() => void pick()}>
                <FolderOpen className="h-4 w-4" />
              </IconButton>
            </div>
          </Field>
        </div>
        <div className="col-span-2">
          <Field
            label="Environment variables"
            hint="One KEY=value per line. Values are stored in the RUNTIDE database; use env files for real secrets."
          >
            <textarea
              value={env}
              onChange={(e) => setEnv(e.target.value)}
              rows={3}
              className="w-full rounded-md border border-line bg-surface p-2 font-mono text-xs"
              placeholder="PORT=3000"
            />
          </Field>
        </div>
        <Field label="Restart policy">
          <Select
            className="w-full"
            value={policy}
            onChange={(e) => setPolicy(e.target.value as typeof policy)}
          >
            <option value="never">Never restart</option>
            <option value="on-failure">Restart on failure</option>
            <option value="always">Always restart</option>
          </Select>
        </Field>
        <Field
          label="Max consecutive restarts"
          hint="Exponential back-off; RUNTIDE gives up after this many."
        >
          <Input
            type="number"
            min={0}
            max={100}
            className="w-full"
            disabled={policy === 'never'}
            value={retries}
            onChange={(e) => setRetries(Number(e.target.value))}
          />
        </Field>
        <Field label="Health check">
          <Select
            className="w-full"
            value={hType}
            onChange={(e) => setHType(e.target.value as typeof hType)}
          >
            <option value="none">None</option>
            <option value="http">HTTP</option>
            <option value="tcp">TCP port</option>
            <option value="process">Process alive</option>
            <option value="command">Custom command</option>
          </Select>
        </Field>
        {hType !== 'none' && (
          <Field label="Interval (seconds)">
            <Input
              type="number"
              min={1}
              max={3600}
              className="w-full"
              value={interval}
              onChange={(e) => setIntervalSec(Number(e.target.value))}
            />
          </Field>
        )}
        {hType === 'http' && (
          <div className="col-span-2">
            <Field label="URL (localhost only)">
              <Input className="w-full" value={hUrl} onChange={(e) => setHUrl(e.target.value)} />
            </Field>
          </div>
        )}
        {hType === 'tcp' && (
          <Field label="Port">
            <Input
              type="number"
              className="w-full"
              value={hPort}
              onChange={(e) => setHPort(Number(e.target.value))}
            />
          </Field>
        )}
        {hType === 'command' && (
          <div className="col-span-2">
            <Field label="Command (exit code 0 = healthy)">
              <Input className="w-full" value={hCmd} onChange={(e) => setHCmd(e.target.value)} />
            </Field>
          </div>
        )}
      </div>
      {err && (
        <div
          role="alert"
          className="selectable mt-3 whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {err}
        </div>
      )}
    </Modal>
  )
}
