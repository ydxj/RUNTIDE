import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { fakeEngine, renderWithHosts, resetUI } from '../../test/helpers'
import { useUI } from '../../stores/ui'
import type { Project, ReplayPlan, VerificationProfile } from '../../types'
import { ProjectsPage } from './ProjectsPage'
import { VerificationPage } from './VerificationPage'
import { SettingsPage } from '../settings/SettingsPage'
import { EnvironmentPage } from '../environment/EnvironmentPage'

const proj = (over: Partial<Project> = {}): Project => ({
  id: 'prj_1',
  name: 'shop',
  root: 'C:\\work\\shop',
  description: 'the shop',
  createdAt: '2026-01-01T10:00:00Z',
  archived: false,
  settings: {},
  ...over,
})

let engine: ReturnType<typeof fakeEngine> | undefined
beforeEach(() => resetUI())
afterEach(() => engine?.restore())

describe('Projects: edit, defaults and archive', () => {
  it('saves description and defaults through projects.update', async () => {
    const profile: VerificationProfile = {
      id: 'vp_1',
      name: 'acceptance',
      description: '',
      pipeline: 'name: a',
      createdAt: '2026-01-01T10:00:00Z',
      updatedAt: '2026-01-01T10:00:00Z',
    }
    engine = fakeEngine({
      'projects.list': () => [proj()],
      'profiles.list': () => [profile],
      'projects.update': () => proj(),
    })
    renderWithHosts(<ProjectsPage />)
    await userEvent.click(await screen.findByRole('button', { name: /Edit/ }))
    await userEvent.selectOptions(await screen.findByLabelText('Default isolation'), 'worktree')
    await userEvent.selectOptions(
      screen.getByLabelText('Default verification profile'),
      'acceptance',
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => {
      const c = engine!.calls.find((x) => x.method === 'projects.update')
      expect(c?.params).toMatchObject({
        ref: 'prj_1',
        settings: { defaultIsolation: 'worktree', defaultProfile: 'acceptance' },
      })
    })
  })

  it('asks before archiving, restores without asking, and reports a refusal', async () => {
    let archived = false
    engine = fakeEngine({
      'projects.list': () => [proj({ archived })],
      'profiles.list': () => [],
      'projects.archive': (p) => {
        const a = p as { archived: boolean }
        if (a.archived) throw new Error('project shop has 1 execution(s) in progress')
        archived = false
        return proj()
      },
    })
    renderWithHosts(<ProjectsPage />)
    await userEvent.click(await screen.findByRole('button', { name: /Archive/ }))
    // Nothing is archived before the user confirms.
    expect(engine.calls.filter((c) => c.method === 'projects.archive')).toHaveLength(0)
    const dlg = await screen.findByRole('dialog')
    await userEvent.click(within(dlg).getByRole('button', { name: 'Archive' }))
    await waitFor(() =>
      expect(engine!.calls.some((c) => c.method === 'projects.archive')).toBe(true),
    )
    // The engine's refusal is surfaced, not swallowed.
    expect(await screen.findByText(/in progress/)).toBeInTheDocument()
    expect(useUI.getState().toasts.length).toBeGreaterThan(0)
  })
})

describe('Verification profiles', () => {
  it('validates on save and shows the engine error', async () => {
    engine = fakeEngine({
      'runs.list': () => [],
      'profiles.list': () => [],
      'profiles.save': () => {
        throw new Error('profile x: validation failed: pipeline has no checks')
      },
    })
    renderWithHosts(<VerificationPage />)
    await userEvent.click(await screen.findByRole('button', { name: /New profile/ }))
    await userEvent.type(await screen.findByLabelText('Profile name'), 'acceptance')
    await userEvent.click(screen.getByRole('button', { name: 'Save profile' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('pipeline has no checks')
    const call = engine.calls.find((c) => c.method === 'profiles.save')
    expect(call?.params).toMatchObject({ name: 'acceptance' })
  })

  it('lists existing profiles and deletes only after confirmation', async () => {
    engine = fakeEngine({
      'runs.list': () => [],
      'profiles.list': () => [
        {
          id: 'vp_1',
          name: 'acceptance',
          description: 'basic checks',
          pipeline: 'name: a',
          createdAt: '',
          updatedAt: '',
        },
      ],
      'profiles.delete': () => null,
    })
    renderWithHosts(<VerificationPage />)
    expect(await screen.findByText('basic checks')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(engine.calls.some((c) => c.method === 'profiles.delete')).toBe(false)
    const dlg = await screen.findByRole('dialog')
    await userEvent.click(within(dlg).getByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(engine!.calls.some((c) => c.method === 'profiles.delete')).toBe(true),
    )
  })
})

describe('Settings: execution evidence', () => {
  it('shows limits, saves one, and counts before deleting anything', async () => {
    engine = fakeEngine({
      'db.info': () => ({ path: 'x', sizeBytes: 1, schemaVersion: 3, rows: {}, recovered: '' }),
      'settings.set': (p) => ({ ...useUI.getState().settings, ...(p as object) }),
      'runs.prune': () => ({ executions: 0, dryRun: true }),
    })
    renderWithHosts(<SettingsPage />)
    const mb = await screen.findByLabelText('execMaxOutputMB')
    expect(mb).toHaveValue(8)
    await userEvent.clear(mb)
    await userEvent.type(mb, '16')
    await userEvent.tab()
    await waitFor(() =>
      expect(engine!.calls.find((c) => c.method === 'settings.set')?.params).toEqual({
        execMaxOutputMB: 16,
      }),
    )
    await userEvent.click(screen.getByRole('button', { name: /Review and delete/ }))
    await waitFor(() => {
      const c = engine!.calls.filter((x) => x.method === 'runs.prune')
      expect(c).toHaveLength(1)
      expect(c[0]?.params).toMatchObject({ dryRun: true })
    })
  })
})

describe('Environment: isolation availability', () => {
  it('reports Docker as unavailable and WSL as available from real status calls', async () => {
    engine = fakeEngine({
      'tools.list': () => [
        {
          id: 'git',
          name: 'Git',
          category: 'vcs',
          installed: true,
          version: '2.45.0',
          path: 'x',
          onPath: true,
          method: 'PATH',
          others: null,
        },
      ],
      'docker.status': () => ({
        cliInstalled: true,
        cliPath: '',
        clientVersion: '27',
        desktopInstalled: true,
        desktopRunning: false,
        daemonAvailable: false,
        serverVersion: '',
        message: 'Docker Desktop is not running',
      }),
      'wsl.status': () => ({
        installed: true,
        path: '',
        distros: [{ name: 'Ubuntu', state: 'Running', version: 2, default: true }],
        message: '',
      }),
      'workspaces.list': () => [],
    })
    renderWithHosts(<EnvironmentPage />)
    const table = await screen.findByRole('table', { name: 'Isolation availability' })
    const row = (n: string) => within(table).getByText(n).closest('tr') as HTMLElement
    await waitFor(() => expect(within(row('docker')).getByText('unavailable')).toBeInTheDocument())
    expect(within(row('docker')).getByText(/not running/)).toBeInTheDocument()
    expect(within(row('wsl')).getByText('available')).toBeInTheDocument()
    expect(within(row('wsl')).getByText(/Ubuntu/)).toBeInTheDocument()
    expect(within(row('worktree')).getByText('available')).toBeInTheDocument()
    expect(within(row('managed')).getByText(/Isolates only the process tree/)).toBeInTheDocument()
  })
})

export type { ReplayPlan }

describe('Receipts: replay pre-flight and authorisation', () => {
  const inspect = {
    path: 'receipts/a.runtide.zip',
    verification: {
      path: 'x',
      sha256: 'ab'.repeat(32),
      valid: true,
      problems: [],
      warnings: [],
      filesChecked: 12,
      integrity: 'verified',
      signatureState: 'unavailable',
      authenticity: 'Integrity checks show the files match the manifest.',
    },
    receipt: {
      sha256: 'ab'.repeat(32),
      manifest: {
        executionId: 'ex_1',
        projectName: 'shop',
        task: 'build',
        status: 'COMPLETED',
        verification: 'passed',
        isolation: 'managed',
        reproducibility: 'partially_reconstructable',
        runtideVersion: '1.0.0',
        createdAt: '2026-01-01T10:00:00Z',
        redaction: { commandsWithMaskedArgs: 0, sensitiveFilesExcluded: 0, diffsIncluded: false },
      },
      reproduction: { class: 'partially_reconstructable', reasons: [], missing: [] },
      commands: [],
      events: [],
      fileChanges: [],
      verification: { summary: { overall: 'passed' }, results: [] },
    },
  }
  const plan: ReplayPlan = {
    class: 'partially_reconstructable',
    reasons: [],
    steps: ['start cleanup: rm -rf build'],
    needsEnv: [],
    toolIssues: [],
    missing: [],
    blockers: [],
    dependencies: [
      {
        kind: 'tool',
        name: 'Node.js',
        required: '20.1.0',
        available: '22.0.0',
        status: 'different',
      },
    ],
    destructive: ['start cleanup: rm -rf build: deletes files'],
    permissions: ['run 1 configured step(s) as your Windows user, without elevation'],
  }

  it('shows dependencies and destructive steps, and runs nothing until approved', async () => {
    const { ReceiptsPage } = await import('./ReceiptsPage')
    engine = fakeEngine({
      'receipts.list': () => [
        {
          id: 'rc_1',
          executionId: 'ex_1',
          projectName: 'shop',
          path: 'p',
          sha256: 'ab'.repeat(32),
          size: 10,
          imported: false,
          status: 'COMPLETED',
          createdAt: '2026-01-01T10:00:00Z',
        },
      ],
      'receipts.inspect': () => inspect,
      'receipts.plan': () => plan,
      'receipts.replay': () => ({
        plan,
        executionId: 'ex_2',
        state: 'COMPLETED',
        receiptPath: 'n',
        outcome: 'partially_reproduced',
        outcomeReasons: ['tool versions differ'],
      }),
    })
    renderWithHosts(<ReceiptsPage />)
    await userEvent.click(await screen.findByRole('button', { name: /Inspect/ }))
    expect(await screen.findByText(/Integrity verified/)).toBeInTheDocument()
    expect(screen.getByText(/signature unavailable/)).toBeInTheDocument()
    await userEvent.click(await screen.findByRole('button', { name: /Replay pre-flight/ }))
    expect(await screen.findByText('Node.js')).toBeInTheDocument()
    expect(screen.getAllByText(/rm -rf build/).length).toBeGreaterThan(0)

    await userEvent.click(screen.getByRole('button', { name: /Review and run replay/ }))
    const dlg = await screen.findAllByRole('dialog')
    expect(within(dlg[dlg.length - 1]!).getByText(/Potentially destructive/)).toBeInTheDocument()
    expect(engine.calls.some((c) => c.method === 'receipts.replay')).toBe(false)
    await userEvent.click(
      within(dlg[dlg.length - 1]!).getByRole('button', { name: /Approve and run replay/ }),
    )
    await waitFor(() => {
      const c = engine!.calls.find((x) => x.method === 'receipts.replay')
      expect(c?.params).toMatchObject({ approve: true, ref: 'rc_1' })
    })
    expect(await screen.findByText('partially reproduced')).toBeInTheDocument()
  })

  it('cannot run a blocked (metadata-only) receipt', async () => {
    const { ReceiptsPage } = await import('./ReceiptsPage')
    engine = fakeEngine({
      'receipts.list': () => [
        {
          id: 'rc_1',
          executionId: 'ex_1',
          projectName: 'shop',
          path: 'p',
          sha256: 'ab'.repeat(32),
          size: 10,
          imported: true,
          status: 'COMPLETED',
          createdAt: '2026-01-01T10:00:00Z',
        },
      ],
      'receipts.inspect': () => inspect,
      'receipts.plan': () => ({
        ...plan,
        steps: [],
        destructive: [],
        blockers: ['this receipt is classified "metadata_only": it has no replayable steps'],
        outcome: 'metadata_only',
      }),
    })
    renderWithHosts(<ReceiptsPage />)
    await userEvent.click(await screen.findByRole('button', { name: /Inspect/ }))
    await userEvent.click(await screen.findByRole('button', { name: /Replay pre-flight/ }))
    expect(await screen.findByText(/no replayable steps/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Review and run replay/ })).toBeDisabled()
  })
})
