import { useState } from 'react'
import { CheckCircle2, FolderOpen } from 'lucide-react'
import { call, native } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { WorkspacePreview } from '../../types'
import { Button, IconButton, Input } from '../../components/ui/primitives'
import { Modal } from '../../components/ui/Overlays'

export const TEMPLATE = `name: my-project
description: Frontend, API and database for my project
services:
  - name: db
    type: custom
    command: docker
    args: [compose, up, db]
    healthcheck: { type: tcp, port: 3306 }

  - name: backend
    type: laravel
    command: php
    args: [artisan, serve]
    working_directory: backend
    depends_on: [db]
    healthcheck: { type: http, url: "http://localhost:8000" }
    restart: { policy: on-failure, max_retries: 3 }

  - name: frontend
    type: nextjs
    command: npm
    args: [run, dev]
    working_directory: frontend
    depends_on: [backend]
    healthcheck: { type: http, url: "http://localhost:3000" }
`

/** YAML editor with server-side validation and a command preview. Saving never runs anything. */
export function WorkspaceEditor({
  initialYaml,
  initialRoot,
  editing,
  onClose,
  onSaved,
}: {
  initialYaml: string
  initialRoot: string
  editing: boolean
  onClose: () => void
  onSaved: (name: string) => void
}) {
  const [yaml, setYaml] = useState(initialYaml)
  const [root, setRoot] = useState(initialRoot)
  const [preview, setPreview] = useState<WorkspacePreview>()
  const [err, setErr] = useState<string>()
  const toast = useUI((s) => s.toast)

  const validate = async (): Promise<WorkspacePreview | undefined> => {
    try {
      const p = await call<WorkspacePreview>('workspaces.inspect', { yaml, root })
      setPreview(p)
      setErr(undefined)
      return p
    } catch (e) {
      setPreview(undefined)
      setErr(errMsg(e))
      return undefined
    }
  }

  const save = async () => {
    const p = await validate()
    if (!p) return
    try {
      await call('workspaces.save', { yaml, root, replace: editing || p.exists })
      toast({
        kind: 'success',
        title: `Workspace ${p.name} saved`,
        detail: 'Nothing was started. You will be asked to authorise execution when you run it.',
      })
      onSaved(p.name)
    } catch (e) {
      setErr(errMsg(e))
    }
  }

  const browse = async () => {
    try {
      const d = await native.pickDirectory('Choose the project root folder')
      if (d) setRoot(d)
    } catch (e) {
      reportError('Could not open folder picker', e)
    }
  }

  return (
    <Modal
      title={editing ? 'Edit workspace' : 'New workspace'}
      width="max-w-3xl"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button onClick={() => void validate()}>Validate</Button>
          <Button
            variant="primary"
            onClick={() => void save()}
            disabled={!yaml.trim() || !root.trim()}
          >
            Save workspace
          </Button>
        </>
      }
    >
      <label className="mb-3 block">
        <span className="mb-1 block text-xs font-medium text-muted">
          Project root folder (services run inside it; paths cannot escape it)
        </span>
        <div className="flex gap-1">
          <Input
            className="flex-1"
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            placeholder="D:\projects\my-project"
          />
          <IconButton label="Browse for folder" onClick={() => void browse()}>
            <FolderOpen className="h-4 w-4" />
          </IconButton>
        </div>
      </label>
      <label className="block">
        <span className="mb-1 block text-xs font-medium text-muted">
          Workspace definition (YAML)
        </span>
        <textarea
          aria-label="Workspace YAML"
          value={yaml}
          onChange={(e) => {
            setYaml(e.target.value)
            setPreview(undefined)
          }}
          spellCheck={false}
          rows={16}
          className="w-full rounded-md border border-line bg-surface p-2 font-mono text-xs leading-5"
        />
      </label>
      {err && (
        <div
          role="alert"
          className="selectable mt-3 whitespace-pre-wrap rounded border border-bad/40 bg-bad/10 p-2 text-sm text-bad"
        >
          {err}
        </div>
      )}
      {preview && (
        <div className="mt-3 rounded border border-ok/40 bg-ok/10 p-3 text-sm">
          <div className="mb-1 flex items-center gap-1.5 font-medium text-ok">
            <CheckCircle2 className="h-4 w-4" /> Valid. Start order:
          </div>
          <ol className="ml-5 list-decimal">
            {preview.order.map((layer, i) => (
              <li key={i}>
                {layer.join(', ')}
                {layer.length > 1 && <span className="text-muted"> (in parallel)</span>}
              </li>
            ))}
          </ol>
          {(preview.warnings ?? []).map((w) => (
            <div key={w} className="mt-1 text-warn">
              {w}
            </div>
          ))}
        </div>
      )}
    </Modal>
  )
}
