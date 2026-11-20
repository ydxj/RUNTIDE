import { useState } from 'react'
import { FolderOpen, GitBranch, Plus } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call, native } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { Project } from '../../types'
import {
  Badge,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  PageHeader,
  Spinner,
} from '../../components/ui/primitives'
import { DataTable, type Column } from '../../components/ui/DataTable'
import { Modal } from '../../components/ui/Overlays'

export function ProjectsPage() {
  const go = useUI((s) => s.go)
  const toast = useUI((s) => s.toast)
  const { data, error, loading, reload } = usePoll(() => call<Project[]>('projects.list'), [], 0)
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
      render: (p) => <span className="font-medium">{p.name}</span>,
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
      width: '170px',
      render: (p) => (
        <Button variant="ghost" onClick={() => go('workspaces', { project: p.name })}>
          Workspaces
        </Button>
      ),
    },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title="Projects"
        subtitle="Directories RUNTIDE executes tasks for. Registering a project does not change it."
      >
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
