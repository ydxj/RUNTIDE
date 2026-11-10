import { expect, test } from '@playwright/test'
import { createServer } from 'node:net'
import { launch, makeProject, type App } from './harness'

let app: App
let project: ReturnType<typeof makeProject>
const DB = 45871
const API = 45872

test.beforeAll(async () => {
  app = await launch()
  project = makeProject({ db: DB, api: API })
})
test.afterAll(async () => {
  project?.cleanup()
  await app?.stop()
})

test('overview shows real machine data', async () => {
  const { page } = app
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  const procs = page.getByRole('button', { name: /^Processes/ }).filter({ hasText: /listening ports/ })
  await expect(procs).toBeVisible()
  const text = (await procs.innerText()).replace(/\s+/g, ' ')
  const n = Number(/Processes (\d+)/.exec(text)?.[1])
  expect(n).toBeGreaterThan(20)
})

test('port manager lists a listener started by the test, with its owner', async () => {
  const { page } = app
  const srv = createServer()
  await new Promise<void>((r) => srv.listen(45870, '127.0.0.1', r))
  try {
    await page.getByRole('button', { name: /^Ports/ }).click()
    await page.getByLabel('Filter ports').fill('45870')
    const row = page.getByRole('row', { name: /45870/ })
    await expect(row).toBeVisible({ timeout: 15_000 })
    await expect(row).toContainText('node.exe')
    await row.click()
    await expect(page.getByRole('complementary', { name: 'Port details' })).toContainText('this machine only')
  } finally {
    srv.close()
  }
})

test('workspace lifecycle through the UI: create, authorise, start in order, logs, stop', async () => {
  const { page } = app
  await page.getByRole('button', { name: /^Workspaces/ }).click()
  await page.getByRole('button', { name: 'New workspace' }).first().click()
  await page.getByPlaceholder(/projects/).fill(project.dir)
  await page.getByLabel('Workspace YAML').fill(project.yaml)
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByText('Valid. Start order:')).toBeVisible()
  await page.getByRole('button', { name: 'Save workspace' }).click()
  await expect(page.getByText('not authorised')).toBeVisible()

  // Nothing is running yet: the port must be free.
  expect(await portOpen(DB)).toBe(false)

  await page.getByRole('button', { name: /Start/ }).first().click()
  const dialog = page.getByRole('dialog', { name: /Authorise e2e-shop/ })
  await expect(dialog).toContainText('node server.js')
  await expect(dialog).toContainText('Authorisation applies to this exact definition')
  expect(await portOpen(DB)).toBe(false) // still nothing started before consent
  await dialog.getByRole('button', { name: /Authorise and start/ }).click()

  await expect(page.getByText('e2e-shop: is up')).toBeVisible({ timeout: 60_000 })
  const services = page.getByRole('table', { name: 'Workspace services' })
  await expect(services.getByRole('row', { name: /db.*running.*healthy/ })).toBeVisible()
  await expect(services.getByRole('row', { name: /api.*running.*healthy/ })).toBeVisible()
  expect(await portOpen(DB)).toBe(true)
  expect(await portOpen(API)).toBe(true)

  // The same engine is reachable from the CLI while the app is open.
  const ls = app.cli('services', '--json')
  expect(ls.status).toBe(0)
  expect(JSON.parse(ls.stdout).map((s: { key: string }) => s.key)).toContain('e2e-shop/api')

  // Logs: real stdout, secret redacted.
  await page.getByRole('button', { name: /^Logs/ }).click()
  const log = page.getByRole('log', { name: 'Log output' })
  await expect(log).toContainText('api ready', { timeout: 15_000 })
  await expect(log).not.toContainText('hunter2hunter2')
  await expect(log).toContainText('redacted')

  // Stop from the UI and verify the OS agrees.
  await page.getByRole('button', { name: /^Workspaces/ }).click()
  await page.getByRole('button', { name: /^Stop/ }).click()
  await expect(page.getByText('e2e-shop: stopped')).toBeVisible({ timeout: 30_000 })
  await expect.poll(() => portOpen(DB), { timeout: 10_000 }).toBe(false)
  await expect.poll(() => portOpen(API), { timeout: 10_000 }).toBe(false)
})

test('doctor runs real checks', async () => {
  const { page } = app
  await page.getByRole('button', { name: /^Doctor/ }).click()
  await page.getByRole('button', { name: 'Run diagnostics' }).click()
  await expect(page.getByRole('button', { name: /^All \(/ })).toBeVisible({ timeout: 60_000 })
})

test('environment page detects the node binary running this test', async () => {
  const { page } = app
  await page.getByRole('button', { name: /^Environment/ }).click()
  const row = page.getByRole('row', { name: /^Node\.js/ })
  await expect(row).toBeVisible({ timeout: 60_000 })
  await expect(row).toContainText('installed')
})

test('dark theme and command palette', async () => {
  const { page } = app
  await page.keyboard.press('Control+k')
  await page.getByLabel('Search commands').fill('dark theme')
  await page.keyboard.press('Enter')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.keyboard.press('Control+k')
  await page.getByLabel('Search commands').fill('light theme')
  await page.keyboard.press('Enter')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
})

async function portOpen(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const s = new (require('node:net').Socket)()
    s.setTimeout(800)
    s.once('connect', () => {
      s.destroy()
      resolve(true)
    })
    s.once('error', () => resolve(false))
    s.once('timeout', () => {
      s.destroy()
      resolve(false)
    })
    s.connect(port, '127.0.0.1')
  })
}
