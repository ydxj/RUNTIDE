import { expect, test } from '@playwright/test'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { launch, type App } from './harness'

let app: App
let dir: string

test.beforeAll(async () => {
  app = await launch(45920)
  dir = mkdtempSync(join(tmpdir(), 'runtide-mgmt-'))
  writeFileSync(join(dir, 'a.txt'), 'x')
})
test.afterAll(async () => {
  await app?.stop()
  rmSync(dir, { recursive: true, force: true })
})

const cli = (...a: string[]) => {
  const r = app.cli(...a)
  expect(r.status, `${a.join(' ')}\n${r.stdout}\n${r.stderr}`).toBe(0)
  return r.stdout
}

test('projects: edit defaults and archive/restore, verified against the engine', async () => {
  const { page } = app
  cli('project', 'create', 'webshop', '--root', dir)
  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /^Projects/ }).click()
  const row = page.getByRole('row', { name: /webshop/ })
  await expect(row).toBeVisible({ timeout: 15_000 })

  await row.getByRole('button', { name: 'Edit' }).click()
  await page.getByLabel('Project description').fill('the demo shop')
  await page.getByLabel('Default isolation').selectOption('worktree')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect.poll(() => cli('project', 'show', 'webshop')).toContain('default isolation: worktree')
  expect(cli('project', 'show', 'webshop')).toContain('the demo shop')

  // Archiving asks first, hides the project, and keeps it under "Show archived".
  await row.getByRole('button', { name: 'Archive' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Archive' }).click()
  await expect(page.getByRole('row', { name: /webshop/ })).toHaveCount(0, { timeout: 15_000 })
  expect(cli('project', 'list')).not.toContain('webshop')
  expect(cli('project', 'list', '--all')).toContain('webshop (archived)')
  await page.getByRole('switch', { name: 'Show archived' }).click()
  const archived = page.getByRole('row', { name: /webshop/ })
  await expect(archived).toContainText('archived')
  await archived.getByRole('button', { name: 'Restore' }).click()
  await expect.poll(() => cli('project', 'list')).toContain('webshop')
})

test('verification profiles: create with validation, list, delete', async () => {
  const { page } = app
  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /^Verification/ }).click()
  await page.getByRole('button', { name: /New profile/ }).click()
  await page.getByLabel('Profile name').fill('smoke')
  await page.getByLabel('Profile pipeline').fill('name: smoke\nchecks: []\n')
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.getByRole('alert')).toContainText('no checks')
  expect(cli('verification', 'list')).not.toContain('smoke')

  await page.getByLabel('Profile pipeline').fill(
    'name: smoke\nchecks:\n  - {name: has-a, type: file, path: a.txt}\n',
  )
  await page.getByRole('button', { name: 'Save profile' }).click()
  await expect(page.getByText('smoke').first()).toBeVisible({ timeout: 15_000 })
  expect(cli('verification', 'show', 'smoke')).toContain('has-a')

  // Use the profile on a real execution through the CLI to prove it is applied.
  const cfg = join(dir, 'w.yaml')
  writeFileSync(cfg, "version: 1\ntask: noop\nstart:\n  - {name: hi, command: node, args: ['-e', \"console.log('hi')\"]}\n")
  cli('workspace', 'create', 'w', '--project', 'webshop', '--config', cfg, '--yes')
  cli('run', 'create', '--workspace', 'webshop/w')
  const id = (JSON.parse(cli('run', 'list', '--json')) as { id: string }[])[0]!.id
  cli('run', 'start', id, '--yes')
  expect(cli('run', 'verify', id, '--profile', 'smoke')).toContain('Overall: passed')

  await page.getByRole('row').filter({ hasText: 'smoke' }).count()
  await page.getByRole('listitem').filter({ hasText: 'smoke' }).getByRole('button', { name: 'Delete' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click()
  await expect.poll(() => cli('verification', 'list')).not.toContain('smoke')
})

test('environment shows the isolation levels that can really run here', async () => {
  const { page } = app
  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /^Environment/ }).click()
  const table = page.getByRole('table', { name: 'Isolation availability' })
  await expect(table).toBeVisible({ timeout: 30_000 })
  await expect(table.getByRole('row', { name: /managed/ })).toContainText('available')
  // Docker and WSL answers come from real probes: whichever they are, they must agree with the CLI.
  const probe = JSON.parse(cli('diagnostics', 'isolation', '--json')) as {
    name: string
    available: boolean
  }[]
  for (const p of probe) {
    await expect(table.getByRole('row', { name: new RegExp(p.name) })).toContainText(
      p.available ? 'available' : 'unavailable',
      { timeout: 30_000 },
    )
  }
})

test('settings: execution limits persist in the engine', async () => {
  const { page } = app
  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /^Settings/ }).click()
  const mb = page.getByLabel('execMaxOutputMB')
  await expect(mb).toHaveValue('8')
  await mb.fill('16')
  await mb.blur()
  await expect.poll(() => cli('config', '--json')).toContain('"execMaxOutputMB": 16')
  await page.getByRole('button', { name: /Review and delete/ }).click()
  await expect(page.getByText(/No executions ended more than/)).toBeVisible()
})

test('receipt replay pre-flight renders with real data and stays blocked without a checkout', async () => {
  const { page } = app
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text())
  })
  // A receipt from a real execution (no verify section, no services: empty lists must not crash the UI).
  const cfg = join(dir, 'bare.yaml')
  writeFileSync(cfg, "version: 1\ntask: bare\nstart:\n  - {name: hi, command: node, args: ['-e', \"console.log('x')\"]}\n")
  cli('workspace', 'create', 'bare', '--project', 'webshop', '--config', cfg, '--yes')
  cli('run', 'create', '--workspace', 'webshop/bare')
  const runs = JSON.parse(cli('run', 'list', '--json', '--state', 'CREATED')) as { id: string }[]
  cli('run', 'start', runs[0]!.id, '--yes')
  cli('run', 'finish', runs[0]!.id)
  cli('run', 'export', runs[0]!.id, '-o', join(dir, 'bare.runtide.zip'))

  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /^Receipts/ }).click()
  await page.getByRole('button', { name: 'Inspect', exact: true }).first().click()
  await expect(page.getByText(/Integrity verified/)).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText(/signature unavailable/)).toBeVisible()
  await page.getByRole('button', { name: /Replay pre-flight/ }).click()
  // The pre-flight probes the installed tool versions, which can take several seconds on a cold CI runner.
  await expect(page.getByText('Permissions this replay needs')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByText('Dependencies', { exact: true })).toBeVisible()
  await expect(page.getByText(/project directory containing the recorded source is required/)).toBeVisible()
  await expect(page.getByRole('button', { name: /Review and run replay/ })).toBeDisabled()
  expect(errors, errors.join('\n')).toEqual([])
})
