import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { since } from '../../services/format'
import { useUI } from '../../stores/ui'
import { useState } from 'react'
import { Plus } from 'lucide-react'
import { errMsg, reportError } from '../../stores/ui'
import type { Execution, ExecutionDetail, VerificationProfile } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  Input,
  ErrorState,
  PageHeader,
  Panel,
  Spinner,
} from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'
import { StateBadge, statusTone, verdictTone } from './common'

/** Verification verdicts across recent executions (each loaded from its recorded results). */
const PROFILE_TEMPLATE = `name: acceptance
checks:
  - {name: build-output, type: file, path: dist/app.js}
  - {name: unit-tests, command: npm, args: [test], timeout_sec: 300}
  - {name: api-health, type: http, url: "http://127.0.0.1:3000/health", expected_status: 200}
`

function ProfilesPanel() {
  const toast = useUI((s) => s.toast)
  const ask = useUI((s) => s.ask)
  const profiles = usePoll(() => call<VerificationProfile[]>('profiles.list'), [], 0)
  const [editing, setEditing] = useState<{
    name: string
    description: string
    yaml: string
    isNew: boolean
  } | null>(null)
  const [err, setErr] = useState<string>()
  const save = async () => {
    if (!editing) return
    try {
      await call('profiles.save', {
        name: editing.name.trim(),
        description: editing.description,
        pipeline: editing.yaml,
      })
      toast({ kind: 'success', title: `Profile ${editing.name} saved` })
      setEditing(null)
      void profiles.reload()
    } catch (e) {
      setErr(errMsg(e))
    }
  }
  const open = (p?: VerificationProfile) => {
    setErr(undefined)
    setEditing(
      p
        ? { name: p.name, description: p.description, yaml: p.pipeline, isNew: false }
        : { name: '', description: '', yaml: PROFILE_TEMPLATE, isNew: true },
    )
  }
  const del = (p: VerificationProfile) =>
    ask({
      title: `Delete profile ${p.name}?`,
      body: 'Projects that use it as their default are cleared. Verification results already recorded are kept.',
      confirmLabel: 'Delete',
      danger: true,
      onConfirm: async () => {
        try {
          await call('profiles.delete', { ref: p.id })
          void profiles.reload()
        } catch (e) {
          reportError('Could not delete profile', e)
        }
      },
    })
  return (
    <Panel
      title="Verification profiles"
      actions={
        <Button icon={<Plus className="h-3.5 w-3.5" />} onClick={() => open()}>
          New profile
        </Button>
      }
    >
      {(profiles.data ?? []).length === 0 ? (
        <div className="p-3 text-sm text-muted">
          No profiles yet. A profile is a reusable set of checks you can apply to any execution, or
          set as a project&apos;s default.
        </div>
      ) : (
        <ul className="divide-y divide-line text-sm">
          {(profiles.data ?? []).map((p) => (
            <li key={p.id} className="flex items-center gap-3 px-3 py-1.5">
              <span className="w-48 font-medium">{p.name}</span>
              <span className="min-w-0 flex-1 truncate text-muted">{p.description}</span>
              <Button variant="ghost" onClick={() => open(p)}>
                Edit
              </Button>
              <Button variant="ghost" onClick={() => del(p)}>
                Delete
              </Button>
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <Modal
          title={editing.isNew ? 'New verification profile' : `Edit ${editing.name}`}
          width="max-w-2xl"
          onClose={() => setEditing(null)}
          footer={
            <>
              <Button onClick={() => setEditing(null)}>Cancel</Button>
              <Button variant="primary" disabled={!editing.name} onClick={() => void save()}>
                Save profile
              </Button>
            </>
          }
        >
          <div className="space-y-3 text-sm">
            {err && (
              <div
                role="alert"
                className="selectable whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-bad"
              >
                {err}
              </div>
            )}
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Name</span>
              <Input
                aria-label="Profile name"
                value={editing.name}
                disabled={!editing.isNew}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Description</span>
              <Input
                aria-label="Profile description"
                value={editing.description}
                onChange={(e) => setEditing({ ...editing, description: e.target.value })}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">
                Checks (YAML: command, http, tcp, file, services_healthy, no_running_services)
              </span>
              <textarea
                aria-label="Profile pipeline"
                value={editing.yaml}
                onChange={(e) => setEditing({ ...editing, yaml: e.target.value })}
                spellCheck={false}
                rows={14}
                className="w-full rounded-md border border-line bg-surface p-2 font-mono text-xs leading-5"
              />
            </label>
            <p className="text-xs text-muted">
              Command checks run on this machine when the profile is applied. The pipeline is
              validated when you save.
            </p>
          </div>
        </Modal>
      )}
    </Panel>
  )
}

export function VerificationPage() {
  const go = useUI((s) => s.go)
  const { data, error, loading, reload } = usePoll(
    async () => {
      const xs = await call<Execution[]>('runs.list', { limit: 15 })
      const ds = await Promise.all(
        (xs ?? []).map((x) => call<ExecutionDetail>('runs.get', { id: x.id })),
      )
      return ds
    },
    [],
    5,
  )
  if (loading && !data) return <Spinner />
  if (error && !data) return <ErrorState message={error} onRetry={() => void reload()} />
  const ds = data ?? []
  const counts = { passed: 0, failed: 0, none: 0 }
  ds.forEach((d) => {
    const v = d.verificationSummary.overall
    if (v === 'passed') counts.passed++
    else if (v === 'failed') counts.failed++
    else counts.none++
  })
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Verification"
        subtitle="Whether tasks actually met their acceptance criteria. A task is verified only when every required check ran and passed."
      />
      <div className="grid gap-3 px-6 pb-6">
        <ProfilesPanel />
        <div className="grid grid-cols-3 gap-3 text-sm">
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Passed</div>
              <div className="text-xl font-semibold text-ok">{counts.passed}</div>
            </div>
          </Panel>
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Failed</div>
              <div className="text-xl font-semibold text-bad">{counts.failed}</div>
            </div>
          </Panel>
          <Panel>
            <div className="p-3">
              <div className="text-xs text-muted">Not verified</div>
              <div className="text-xl font-semibold">{counts.none}</div>
            </div>
          </Panel>
        </div>
        {ds.length === 0 && (
          <EmptyState
            title="Nothing to verify yet"
            hint="Executions appear here with the results of their verification pipelines."
          />
        )}
        {ds.map((d) => (
          <Panel
            key={d.execution.id}
            title={
              <button
                type="button"
                className="text-left hover:underline"
                onClick={() => go('executions', { id: d.execution.id })}
              >
                {d.execution.task || d.execution.id}
              </button>
            }
            actions={
              <>
                <StateBadge state={d.execution.state} />
                <Badge tone={verdictTone(d.verificationSummary.overall)}>
                  {d.verificationSummary.overall === 'none'
                    ? 'not verified'
                    : d.verificationSummary.overall}
                </Badge>
              </>
            }
          >
            {d.verification.length === 0 ? (
              <div className="p-3 text-sm text-muted">
                No verification results ({since(d.execution.createdAt)}).
              </div>
            ) : (
              <ul className="divide-y divide-line text-sm">
                {d.verification.map((v) => (
                  <li key={v.name} className="flex items-center gap-3 px-3 py-1.5">
                    <Badge tone={statusTone(v.status)}>{v.status.replace('_', ' ')}</Badge>
                    <span className="w-48 font-medium">{v.name}</span>
                    <span className="selectable min-w-0 flex-1 truncate text-muted" title={v.error}>
                      {v.error}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        ))}
      </div>
    </div>
  )
}
