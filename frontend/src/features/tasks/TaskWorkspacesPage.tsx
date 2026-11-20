import { useEffect, useMemo, useState } from 'react'
import { FolderOpen, Play, Plus, RotateCcw, ShieldCheck, Trash2 } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { Execution, Project, TaskWorkspace, WorkspacePlan } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  PageHeader,
  Panel,
  Select,
  Spinner,
  cx,
} from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'
import { ISOLATION, ISOLATION_HELP, StateBadge } from './common'

const TEMPLATE = `version: 1
task: Describe the task
isolation: managed
start:
  - {name: build, command: npm, args: [run, build]}
verify:
  name: acceptance
  checks:
    - {name: tests, command: npm, args: [test]}
policy:
  allow_adhoc: false
`

export function TaskWorkspacesPage() {
  const params = useUI((s) => s.pageParams)
  const go = useUI((s) => s.go)
  const ask = useUI((s) => s.ask)
  const toast = useUI((s) => s.toast)
  const [projectFilter, setProjectFilter] = useState(params['project'] ?? '')
  const [sel, setSel] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const projects = usePoll(() => call<Project[]>('projects.list'), [], 0)
  const list = usePoll(
    () => call<TaskWorkspace[]>('ws.list', { project: projectFilter }),
    [projectFilter],
    3,
  )
  const rows = useMemo(() => list.data ?? [], [list.data])
  useEffect(() => {
    if (!sel && rows[0]) setSel(rows[0].id)
  }, [rows, sel])
  const current = rows.find((w) => w.id === sel)
  const detail = usePoll(
    () =>
      sel
        ? call<{ workspace: TaskWorkspace; commands: string[] }>('ws.get', { ref: sel })
        : Promise.resolve(undefined),
    [sel],
    3,
  )
  const [cfgEdit, setCfgEdit] = useState<string | null>(null)
  const [cfgErr, setCfgErr] = useState<string>()
  const [busy, setBusy] = useState(false)

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    try {
      await fn()
      toast({ kind: 'success', title: ok })
    } catch (e) {
      reportError('Action failed', e)
    } finally {
      setBusy(false)
      void list.reload()
      void detail.reload()
    }
  }

  const approve = () => {
    if (!current) return
    const cmds = detail.data?.commands ?? []
    ask({
      title: `Approve commands of ${current.name}?`,
      confirmLabel: 'Approve',
      body: (
        <div className="space-y-2">
          <p>Executions of this workspace will run these commands on this computer, as you:</p>
          <ul className="selectable max-h-60 space-y-1 overflow-auto rounded border border-line bg-subtle p-2 font-mono text-xs">
            {cmds.length === 0 ? (
              <li>(no commands configured)</li>
            ) : (
              cmds.map((c) => <li key={c}>{c}</li>)
            )}
          </ul>
          <p className="text-xs text-muted">
            Isolation: {ISOLATION[current.isolation]}.{' '}
            {ISOLATION_HELP[ISOLATION[current.isolation] ?? 'managed']} Approval applies to this
            exact configuration; editing it revokes approval.
          </p>
        </div>
      ),
      onConfirm: () =>
        act(() => call('ws.approve', { ref: current.id, confirm: true }), 'Workspace approved'),
    })
  }

  const start = () => {
    if (!current) return
    void act(async () => {
      const x = await call<Execution>('runs.create', { workspace: current.id })
      await call('runs.start', { id: x.id })
      go('executions', { id: x.id })
    }, 'Execution started')
  }

  const removeDir = () => {
    if (!current) return
    ask({
      title: 'Delete the workspace directory?',
      danger: true,
      confirmLabel: 'Delete directory',
      body: `${current.dir} is removed. Only directories RUNTIDE created are ever deleted; your project is never touched. Uncommitted work in it is refused unless forced from the CLI.`,
      onConfirm: () =>
        act(() => call('ws.removeDir', { ref: current.id, confirm: true }), 'Directory removed'),
    })
  }

  const saveCfg = async () => {
    if (!current || cfgEdit == null) return
    try {
      await call('ws.configure', { ref: current.id, yaml: cfgEdit })
      toast({
        kind: 'success',
        title: 'Configuration saved',
        detail: 'Approval was revoked: review and approve again before running.',
      })
      setCfgEdit(null)
      void detail.reload()
      void list.reload()
    } catch (e) {
      setCfgErr(errMsg(e))
    }
  }

  const w = detail.data?.workspace ?? current
  const canStart = w?.state === 'READY'
  const iso = w ? (ISOLATION[w.isolation] ?? 'managed') : ''

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Workspaces"
        subtitle="The environment of one task: directory, isolation, commands, verification."
      >
        <Select
          aria-label="Filter by project"
          value={projectFilter}
          onChange={(e) => setProjectFilter(e.target.value)}
        >
          <option value="">All projects</option>
          {(projects.data ?? []).map((p) => (
            <option key={p.id} value={p.name}>
              {p.name}
            </option>
          ))}
        </Select>
        <Button
          variant="primary"
          icon={<Plus className="h-3.5 w-3.5" />}
          onClick={() => setCreating(true)}
          disabled={(projects.data ?? []).length === 0}
        >
          New workspace
        </Button>
      </PageHeader>
      {list.loading && !list.data ? (
        <Spinner />
      ) : list.error ? (
        <ErrorState message={list.error} onRetry={() => void list.reload()} />
      ) : rows.length === 0 ? (
        <EmptyState
          title="No workspaces yet"
          hint={
            (projects.data ?? []).length === 0
              ? 'Create a project first.'
              : 'A workspace defines how one task is prepared, run and verified.'
          }
          action={
            (projects.data ?? []).length > 0 ? (
              <Button variant="primary" onClick={() => setCreating(true)}>
                Create a workspace
              </Button>
            ) : (
              <Button variant="primary" onClick={() => go('projects')}>
                Go to Projects
              </Button>
            )
          }
        />
      ) : (
        <div className="flex min-h-0 flex-1 gap-3 px-6 pb-4">
          <ul aria-label="Task workspaces" className="w-64 shrink-0 space-y-1 overflow-auto">
            {rows.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  aria-current={r.id === sel}
                  onClick={() => setSel(r.id)}
                  className={cx(
                    'w-full rounded-lg border px-3 py-2 text-left',
                    r.id === sel
                      ? 'border-accent bg-surface'
                      : 'border-line bg-surface hover:bg-subtle',
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate font-medium">{r.name}</span>
                    <StateBadge state={r.state} />
                  </div>
                  <div className="mt-0.5 truncate text-xs text-muted">
                    {r.task || 'no task description'}
                  </div>
                </button>
              </li>
            ))}
          </ul>
          {w && (
            <div className="flex min-w-0 flex-1 flex-col gap-3 overflow-auto">
              <Panel
                title={
                  <span className="flex items-center gap-2">
                    {w.name} <StateBadge state={w.state} />{' '}
                    {w.approved ? (
                      <Badge tone="ok">commands approved</Badge>
                    ) : (
                      <Badge tone="warn">not approved</Badge>
                    )}
                  </span>
                }
                actions={
                  <>
                    {!w.approved && (
                      <Button icon={<ShieldCheck className="h-3.5 w-3.5" />} onClick={approve}>
                        Review & approve…
                      </Button>
                    )}
                    <Button
                      variant="primary"
                      disabled={busy || !canStart || !w.approved}
                      title={
                        !w.approved
                          ? 'Approve the commands first'
                          : !canStart
                            ? `Workspace is ${w.state}`
                            : undefined
                      }
                      icon={<Play className="h-3.5 w-3.5" />}
                      onClick={start}
                    >
                      Start execution
                    </Button>
                  </>
                }
              >
                <dl className="grid grid-cols-2 gap-3 p-3 text-sm">
                  <div className="col-span-2">
                    <dt className="text-xs text-muted">Task</dt>
                    <dd className="selectable">{w.task || '-'}</dd>
                  </div>
                  <div>
                    <dt className="text-xs text-muted">Isolation</dt>
                    <dd>
                      <Badge tone="accent">{iso}</Badge>{' '}
                      <span className="text-xs text-muted">{ISOLATION_HELP[iso]}</span>
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-muted">Directory</dt>
                    <dd className="selectable break-all font-mono text-xs">{w.dir}</dd>
                  </div>
                </dl>
                <div className="flex flex-wrap gap-2 border-t border-line p-3">
                  <Button
                    disabled={busy}
                    onClick={() => {
                      setCfgErr(undefined)
                      setCfgEdit(w.configYaml)
                    }}
                  >
                    Edit configuration…
                  </Button>
                  {[
                    'COMPLETED',
                    'FAILED',
                    'INTERRUPTED',
                    'REQUIRES_ATTENTION',
                    'CANCELLED',
                  ].includes(w.state) && (
                    <Button
                      disabled={busy}
                      icon={<RotateCcw className="h-3.5 w-3.5" />}
                      onClick={() =>
                        void act(() => call('ws.reopen', { ref: w.id }), 'Workspace reopened')
                      }
                    >
                      Reopen
                    </Button>
                  )}
                  {w.worktree && (
                    <Button
                      variant="danger"
                      disabled={busy}
                      icon={<Trash2 className="h-3.5 w-3.5" />}
                      onClick={removeDir}
                    >
                      Remove directory…
                    </Button>
                  )}
                  <Button onClick={() => go('executions', { workspace: w.id })}>Executions</Button>
                </div>
              </Panel>
              <Panel title="Commands this workspace would run">
                <ul className="selectable divide-y divide-line font-mono text-xs">
                  {(detail.data?.commands ?? []).map((c) => (
                    <li key={c} className="px-3 py-1.5">
                      {c}
                    </li>
                  ))}
                  {(detail.data?.commands ?? []).length === 0 && (
                    <li className="px-3 py-2 text-sm text-muted">None configured.</li>
                  )}
                </ul>
              </Panel>
            </div>
          )}
        </div>
      )}
      {creating && (
        <CreateWorkspace
          projects={projects.data ?? []}
          initialProject={projectFilter}
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            setCreating(false)
            setSel(id)
            void list.reload()
          }}
        />
      )}
      {cfgEdit != null && (
        <Modal
          title="Edit workspace configuration"
          width="max-w-3xl"
          onClose={() => setCfgEdit(null)}
          footer={
            <>
              <Button onClick={() => setCfgEdit(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => void saveCfg()}>
                Save (revokes approval)
              </Button>
            </>
          }
        >
          <textarea
            aria-label="Workspace configuration"
            value={cfgEdit}
            onChange={(e) => setCfgEdit(e.target.value)}
            spellCheck={false}
            rows={20}
            className="w-full rounded-md border border-line bg-surface p-2 font-mono text-xs leading-5"
          />
          {cfgErr && (
            <div
              role="alert"
              className="selectable mt-3 whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
            >
              {cfgErr}
            </div>
          )}
        </Modal>
      )}
    </div>
  )
}

function CreateWorkspace({
  projects,
  initialProject,
  onClose,
  onCreated,
}: {
  projects: Project[]
  initialProject: string
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const [project, setProject] = useState(initialProject || projects[0]?.name || '')
  const [name, setName] = useState('')
  const [task, setTask] = useState('')
  const [isolation, setIsolation] = useState('managed')
  const [yaml, setYaml] = useState(TEMPLATE)
  const [dir, setDir] = useState('')
  const [allowDirty, setAllowDirty] = useState(false)
  const [plan, setPlan] = useState<WorkspacePlan>()
  const [err, setErr] = useState<string>()

  const req = {
    project,
    name,
    task,
    isolation,
    configYaml: yaml
      .replace(/^task: .*$/m, `task: ${JSON.stringify(task || 'task')}`)
      .replace(/^isolation: .*$/m, `isolation: ${isolation}`),
    dir,
    acceptDirty: allowDirty,
  }
  const preview = async () => {
    try {
      setPlan(await call<WorkspacePlan>('ws.plan', req))
      setErr(undefined)
    } catch (e) {
      setPlan(undefined)
      setErr(errMsg(e))
    }
  }
  const create = async () => {
    try {
      const w = await call<TaskWorkspace>('ws.create', { ...req, confirm: true })
      onCreated(w.id)
    } catch (e) {
      setErr(errMsg(e))
    }
  }
  const browse = async () => {
    const d = await native.pickDirectory('Directory to run in')
    if (d) setDir(d)
  }
  const blocked = (plan?.blockers ?? []).length > 0

  return (
    <Modal
      title="New workspace"
      width="max-w-3xl"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button onClick={() => void preview()} disabled={!project || !name}>
            Preview what will be created
          </Button>
          <Button variant="primary" onClick={() => void create()} disabled={!plan || blocked}>
            Create workspace
          </Button>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-3">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Project</span>
          <Select
            className="w-full"
            value={project}
            onChange={(e) => {
              setProject(e.target.value)
              setPlan(undefined)
            }}
          >
            {projects.map((p) => (
              <option key={p.id}>{p.name}</option>
            ))}
          </Select>
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Workspace name</span>
          <Input
            className="w-full"
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              setPlan(undefined)
            }}
            placeholder="implement-auth"
          />
        </label>
        <label className="col-span-2 block">
          <span className="mb-1 block text-xs font-medium text-muted">Task</span>
          <Input
            className="w-full"
            value={task}
            onChange={(e) => setTask(e.target.value)}
            placeholder="Implement authentication"
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Isolation</span>
          <Select
            className="w-full"
            value={isolation}
            onChange={(e) => {
              setIsolation(e.target.value)
              setPlan(undefined)
            }}
          >
            <option value="managed">1 · managed process</option>
            <option value="worktree">2 · filesystem-separated (git worktree)</option>
            <option value="docker">3 · Docker container</option>
            <option value="wsl">4 · WSL2</option>
          </Select>
          <span className="mt-1 block text-xs text-muted">{ISOLATION_HELP[isolation]}</span>
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">
            Directory (managed/docker/wsl; default: project root)
          </span>
          <div className="flex gap-1">
            <Input className="flex-1" value={dir} onChange={(e) => setDir(e.target.value)} />
            <IconButton label="Browse for folder" onClick={() => void browse()}>
              <FolderOpen className="h-4 w-4" />
            </IconButton>
          </div>
        </label>
        <label className="col-span-2 block">
          <span className="mb-1 block text-xs font-medium text-muted">Configuration (YAML)</span>
          <textarea
            aria-label="Configuration YAML"
            value={yaml}
            onChange={(e) => {
              setYaml(e.target.value)
              setPlan(undefined)
            }}
            spellCheck={false}
            rows={12}
            className="w-full rounded-md border border-line bg-surface p-2 font-mono text-xs leading-5"
          />
        </label>
        <label className="col-span-2 flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={allowDirty}
            onChange={(e) => {
              setAllowDirty(e.target.checked)
              setPlan(undefined)
            }}
          />{' '}
          Allow a worktree from the last commit while the source has uncommitted changes (they are
          never modified)
        </label>
      </div>
      {err && (
        <div
          role="alert"
          className="selectable mt-3 whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {err}
        </div>
      )}
      {plan && (
        <div
          className="mt-3 space-y-2 rounded border border-line bg-subtle p-3 text-sm"
          aria-label="Creation plan"
        >
          <div className="font-medium">This will create:</div>
          <ul className="list-disc pl-5">
            {(plan.creates ?? []).map((c) => (
              <li key={c} className="selectable">
                {c}
              </li>
            ))}
          </ul>
          {(plan.warnings ?? []).map((x) => (
            <div key={x} className="text-warn">
              {x}
            </div>
          ))}
          {(plan.blockers ?? []).map((x) => (
            <div key={x} className="text-bad">
              Blocked: {x}
            </div>
          ))}
          <div className="text-xs text-muted">
            Working directory: <span className="font-mono">{plan.dir}</span>
          </div>
        </div>
      )}
    </Modal>
  )
}
