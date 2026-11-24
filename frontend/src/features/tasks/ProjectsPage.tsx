import { useState } from 'react'
import { Archive, ArchiveRestore, FolderOpen, GitBranch, Pencil, Plus } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { Project, VerificationProfile } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  PageHeader,
  Select,
  Spinner,
  Switch,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { Modal } from '../../components/ui/Overlays'

export function ProjectsPage() {
  const go = useUI((s) => s.go)
  const toast = useUI((s) => s.toast)
  const ask = useUI((s) => s.ask)
  const [showArchived, setShowArchived] = useState(false)
  const { data, error, loading, reload } = usePoll(
    () => call<Project[]>('projects.list', { includeArchived: showArchived }),
    [showArchived],
    0,
  )
  const profiles = usePoll(() => call<VerificationProfile[]>('profiles.list'), [], 0)
  const [editing, setEditing] = useState<Project | null>(null)
  const [eDesc, setEDesc] = useState('')
  const [eIso, setEIso] = useState('')
  const [eProfile, setEProfile] = useState('')
  const [eErr, setEErr] = useState<string>()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [root, setRoot] = useState('')
  const [desc, setDesc] = useState('')
  const [err, setErr] = useState<string>()

  const save = async () => {
    try {
      await call('projects.create', { name: name.trim(), root: root.trim(), description: desc })
      toast({ kind: 'success', title: `Project ${name} created` })
      setOpen(false)
      setName('')
      setRoot('')
      setDesc('')
      void reload()
    } catch (e) {
      setErr(errMsg(e))
    }
  }
  const startEdit = (p: Project) => {
    setEditing(p)
    setEDesc(p.description)
    setEIso(p.settings?.defaultIsolation ?? '')
    setEProfile(p.settings?.defaultProfile ?? '')
    setEErr(undefined)
  }
  const saveEdit = async () => {
    if (!editing) return
    try {
      await call('projects.update', {
        ref: editing.id,
        description: eDesc,
        settings: { defaultIsolation: eIso, defaultProfile: eProfile },
      })
      toast({ kind: 'success', title: `Project ${editing.name} updated` })
      setEditing(null)
      void reload()
    } catch (e) {
      setEErr(errMsg(e))
    }
  }
  const archive = (p: Project, archived: boolean) => {
    const run = async () => {
      try {
        await call('projects.archive', { ref: p.id, archived })
        toast({ kind: 'success', title: archived ? `${p.name} archived` : `${p.name} restored` })
        void reload()
      } catch (e) {
        reportError(archived ? 'Could not archive' : 'Could not restore', e)
      }
    }
    if (!archived) return void run()
    ask({
      title: `Archive ${p.name}?`,
      body: 'The project is hidden and cannot get new workspaces. Its workspaces, executions and receipts are kept, and you can restore it at any time.',
      confirmLabel: 'Archive',
      onConfirm: run,
    })
  }
  const browse = async () => {
    try {
      const d = await native.pickDirectory('Choose the project directory')
      if (d) setRoot(d)
    } catch (e) {
      reportError('Could not open folder picker', e)
    }
  }

  const cols: Column<Project>[] = [
    {
      key: 'name',
      header: 'Project',
      sort: (a, b) => a.name.localeCompare(b.name),
      render: (p) => (
        <span className="flex items-center gap-2">
          <span className="font-medium">{p.name}</span>
          {p.archived && <Badge>archived</Badge>}
        </span>
      ),
    },
    {
      key: 'defaults',
      header: 'Defaults',
      width: '220px',
      render: (p) => (
        <span className="text-muted">
          {p.settings?.defaultIsolation || 'managed'}
          {p.settings?.defaultProfile ? ` · ${p.settings.defaultProfile}` : ''}
        </span>
      ),
    },
    {
      key: 'git',
      header: 'Git',
      width: '70px',
      render: (p) =>
        p.gitRoot ? (
          <span className="flex items-center gap-1 text-muted">
            <GitBranch className="h-3.5 w-3.5" aria-hidden /> yes
          </span>
        ) : (
          <span className="text-muted">no</span>
        ),
    },
    { key: 'root', header: 'Root', mono: true, render: (p) => p.root },
    {
      key: 'act',
      header: '',
      width: '420px',
      render: (p) => (
        <span className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>
          <Button variant="ghost" onClick={() => go('workspaces', { project: p.name })}>
            Workspaces
          </Button>
          <Button variant="ghost" onClick={() => go('executions', { project: p.name })}>
            History
          </Button>
          <Button
            variant="ghost"
            icon={<Pencil className="h-3.5 w-3.5" />}
            onClick={() => startEdit(p)}
          >
            Edit
          </Button>
          <Button
            variant="ghost"
            icon={
              p.archived ? (
                <ArchiveRestore className="h-3.5 w-3.5" />
              ) : (
                <Archive className="h-3.5 w-3.5" />
              )
            }
            onClick={() => archive(p, !p.archived)}
          >
            {p.archived ? 'Restore' : 'Archive'}
          </Button>
        </span>
      ),
    },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Projects"
        subtitle="Directories RUNTIDE executes tasks for. Registering a project does not change it."
      >
        <label className="flex items-center gap-2 text-sm text-muted">
          <Switch checked={showArchived} onChange={setShowArchived} label="Show archived" />
          Show archived
        </label>
        <Button
          variant="primary"
          icon={<Plus className="h-3.5 w-3.5" />}
          onClick={() => setOpen(true)}
        >
          New project
        </Button>
      </PageHeader>
      <div className="mx-6 mb-4 flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        {loading && !data ? (
          <Spinner />
        ) : error ? (
          <ErrorState message={error} onRetry={() => void reload()} />
        ) : (
          <DataTable
            label="Projects"
            columns={cols}
            rows={data ?? []}
            rowKey={(p) => p.id}
            initialSort={{ key: 'name', dir: 'asc' }}
            empty={
              <EmptyState
                title="No projects yet"
                hint="A project is the directory (usually a git repository) where tasks are executed."
                action={
                  <Button variant="primary" onClick={() => setOpen(true)}>
                    Create a project
                  </Button>
                }
              />
            }
          />
        )}
      </div>
      {editing && (
        <Modal
          title={`Edit ${editing.name}`}
          onClose={() => setEditing(null)}
          footer={
            <>
              <Button onClick={() => setEditing(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => void saveEdit()}>
                Save
              </Button>
            </>
          }
        >
          <div className="space-y-3 text-sm">
            {eErr && (
              <div role="alert" className="rounded border border-bad/40 bg-bad/10 p-2 text-bad">
                {eErr}
              </div>
            )}
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Description</span>
              <Input
                aria-label="Project description"
                value={eDesc}
                onChange={(e) => setEDesc(e.target.value)}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">
                Default isolation for new workspaces
              </span>
              <Select
                aria-label="Default isolation"
                value={eIso}
                onChange={(e) => setEIso(e.target.value)}
              >
                <option value="">managed (no default)</option>
                <option value="worktree">worktree (needs a git repository)</option>
                <option value="docker">docker (needs Docker)</option>
                <option value="wsl">wsl (needs WSL2)</option>
              </Select>
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">
                Default verification profile
              </span>
              <Select
                aria-label="Default verification profile"
                value={eProfile}
                onChange={(e) => setEProfile(e.target.value)}
              >
                <option value="">none</option>
                {(profiles.data ?? []).map((p) => (
                  <option key={p.id} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </Select>
              <span className="mt-1 block text-xs text-muted">
                Used when a workspace has no verify section of its own.
              </span>
            </label>
            <p className="text-xs text-muted">Root: {editing.root}</p>
          </div>
        </Modal>
      )}
      {open && (
        <Modal
          title="New project"
          onClose={() => setOpen(false)}
          footer={
            <>
              <Button onClick={() => setOpen(false)}>Cancel</Button>
              <Button variant="primary" disabled={!name || !root} onClick={() => void save()}>
                Create project
              </Button>
            </>
          }
        >
          <div className="space-y-3">
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Name</span>
              <Input
                className="w-full"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="shop"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Project directory</span>
              <div className="flex gap-1">
                <Input
                  className="flex-1"
                  value={root}
                  onChange={(e) => setRoot(e.target.value)}
                  placeholder="D:\projects\shop"
                />
                <IconButton label="Browse for folder" onClick={() => void browse()}>
                  <FolderOpen className="h-4 w-4" />
                </IconButton>
              </div>
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Description</span>
              <Input className="w-full" value={desc} onChange={(e) => setDesc(e.target.value)} />
            </label>
            {err && (
              <div
                role="alert"
                className="rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
              >
                {err}
              </div>
            )}
            <Badge>Nothing is created in the directory: RUNTIDE only remembers it.</Badge>
          </div>
        </Modal>
      )}
    </div>
  )
}
