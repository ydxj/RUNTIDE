import { expect, test } from '@playwright/test'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { launch, type App } from './harness'

let app: App
let dir: string

test.beforeAll(async () => {
  app = await launch(45910)
  dir = mkdtempSync(join(tmpdir(), 'runtide-exec-'))
  mkdirSync(join(dir, 'src'))
  writeFileSync(join(dir, 'src', 'app.txt'), 'v1\n')
})
test.afterAll(async () => {
  await app?.stop()
  rmSync(dir, { recursive: true, force: true })
})

test('execution workflow: run, verify, export receipt, and see it all in the UI', async () => {
  const { page } = app
  const cfg = join(dir, 'workspace.yaml')
  writeFileSync(
    cfg,
    `version: 1
task: Build and verify
start:
  - {name: build, command: node, args: ['-e', "console.log('built ok'); require('fs').writeFileSync('result.txt','ok')"]}
verify:
  name: acceptance
  checks:
    - {name: output-exists, type: file, path: result.txt}
    - {name: node-works, command: node, args: ['-e', 'process.exit(0)']}
`,
  )
  const cli = (...a: string[]) => {
    const r = app.cli(...a)
    expect(r.status, `${a.join(' ')}\n${r.stdout}\n${r.stderr}`).toBe(0)
    return r.stdout
  }
  cli('project', 'create', 'e2e-proj', '--root', dir)
  cli('workspace', 'create', 'e2e-ws', '--project', 'e2e-proj', '--config', cfg, '--yes')
  cli('run', 'create', '--workspace', 'e2e-proj/e2e-ws')
  const runs = JSON.parse(cli('run', 'list', '--json')) as { id: string }[]
  const id = runs[0]!.id

  // Before approval nothing may execute: the CLI refuses to start.
  const refused = app.cli('run', 'start', id)
  expect(refused.status).not.toBe(0)

  cli('run', 'start', id, '--yes')
  cli('run', 'verify', id)
  const rcpt = join(dir, 'out.runtide.zip')
  cli('run', 'export', id, '-o', rcpt)

  // Executions page lists it as completed and verified.
  await page.getByRole('button', { name: /^Executions/ }).click()
  const row = page.getByRole('row', { name: /Build and verify/ })
  await expect(row).toBeVisible({ timeout: 15_000 })
  await expect(row).toContainText('COMPLETED')
  await expect(row).toContainText('passed')

  // Detail: commands, logs and verification come from the recorded evidence.
  await row.click()
  await page.getByRole('tab', { name: 'Logs' }).click()
  await expect(page.getByText('built ok')).toBeVisible({ timeout: 15_000 })
  await page.getByRole('tab', { name: 'Verification' }).click()
  await expect(page.getByText('output-exists')).toBeVisible()

  // Receipts page lists the exported file.
  await page.getByRole('button', { name: /^Receipts/ }).click()
  await expect(page.getByText('e2e-proj').first()).toBeVisible({ timeout: 15_000 })

  // The receipt verifies from the CLI too (hash chain intact).
  expect(cli('receipt', 'verify', rcpt)).toMatch(/valid/i)
})
