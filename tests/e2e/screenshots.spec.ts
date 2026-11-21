import { test } from '@playwright/test'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { launch, type App } from './harness'

// Regenerates docs/screenshots from the real engine. Run on demand:
//   SCREENSHOTS=1 npx playwright test screenshots.spec.ts
test.skip(!process.env.SCREENSHOTS, 'set SCREENSHOTS=1 to regenerate documentation screenshots')

const out = resolve(__dirname, '..', '..', 'docs', 'screenshots')
let app: App

function demoProject() {
  const base = join(process.env.SCREENSHOT_DEMO_DIR ?? tmpdir(), "demo")
  mkdirSync(base, { recursive: true })
  const dir = join(base, "fullstack-demo")
  mkdirSync(dir, { recursive: true })
  const svc = (name: string, body: string) => {
    mkdirSync(join(dir, name), { recursive: true })
    writeFileSync(join(dir, name, 'server.js'), body)
  }
  svc('database', `require('net').createServer(s=>s.end()).listen(43110,'127.0.0.1',()=>console.log('database: accepting connections on 43110'))
setInterval(()=>console.log('database: checkpoint complete'),4000)`)
  svc('api', `const http=require('http');let n=0
http.createServer((q,r)=>{n++;r.end('ok')}).listen(43111,'127.0.0.1',()=>console.log('api: listening on http://127.0.0.1:43111'))
setInterval(()=>{console.log('api: GET /v1/projects 200 4ms');if(n%3===0)console.warn('warning: slow query (212ms) on projects.list')},1500)
setTimeout(()=>console.error('ERROR cache miss storm detected, falling back to database'),5000)
console.log('api: connected using password=correct-horse-battery')`)
  svc('web', `const http=require('http')
http.createServer((q,r)=>r.end('<h1>web</h1>')).listen(43112,'127.0.0.1',()=>console.log('web: ready in 412ms  http://127.0.0.1:43112'))
setInterval(()=>console.log('web: compiled /dashboard in 38ms'),2500)`)
  const yaml = `name: fullstack-demo
description: Next.js frontend, API and database
services:
  - name: database
    type: custom
    command: node
    args: [server.js]
    working_directory: database
    healthcheck: { type: tcp, port: 43110, interval_sec: 2 }
  - name: api
    type: node
    command: node
    args: [server.js]
    working_directory: api
    depends_on: [database]
    healthcheck: { type: http, url: "http://127.0.0.1:43111/", interval_sec: 2 }
  - name: web
    type: nextjs
    command: node
    args: [server.js]
    working_directory: web
    depends_on: [api]
    healthcheck: { type: http, url: "http://127.0.0.1:43112/", interval_sec: 2 }
`
  writeFileSync(join(dir, 'runtide.yaml'), yaml)
  return { dir, file: join(dir, 'runtide.yaml') }
}

test.beforeAll(async () => {
  mkdirSync(out, { recursive: true })
  app = await launch(45901)
})
test.afterAll(async () => {
  await app?.stop()
})

test('capture documentation screenshots', async () => {
  test.setTimeout(300_000)
  const { page } = app
  const proj = demoProject()
  const imp = app.cli('stack', 'import', proj.file, '--yes')
  if (imp.status !== 0) throw new Error(imp.stderr)
  const legacy = join(proj.dir, "..", "legacy-admin")
  mkdirSync(legacy, { recursive: true })
  writeFileSync(join(legacy, "runtide.yaml"), `name: legacy-admin
description: Laravel admin panel (needs PHP)
services:
  - name: admin
    type: laravel
    command: php
    args: [artisan, serve, "--port=43111"]
    working_directory: admin
    healthcheck: { type: http, url: "http://127.0.0.1:43111/", interval_sec: 5 }
`)
  const imp2 = app.cli("stack", "import", join(legacy, "runtide.yaml"), "--yes")
  if (imp2.status !== 0) throw new Error(imp2.stderr)
  const up = app.cli('stack', 'up', 'fullstack-demo', '--yes')
  if (up.status !== 0) throw new Error(up.stderr)
  await page.waitForTimeout(7000) // let health checks settle and logs accumulate

  // Real executions: one that passes verification and one that fails it.
  const execDir = join(process.env.SCREENSHOT_DEMO_DIR ?? tmpdir(), 'exec-demo')
  rmSync(execDir, { recursive: true, force: true }) // regenerated each run
  mkdirSync(join(execDir, 'src'), { recursive: true })
  writeFileSync(join(execDir, 'src', 'app.txt'), 'v1\n')
  const mkCfg = (name: string, task: string, ok: boolean) => {
    const p = join(execDir, `${name}.yaml`)
    writeFileSync(
      p,
      `version: 1
task: ${task}
isolation: managed
start:
  - {name: build, command: node, args: ['-e', "console.log('compiling 12 modules'); console.log('build finished'); require('fs').writeFileSync('result.txt','ok')"]}
verify:
  name: acceptance
  checks:
    - {name: build-output, type: file, path: result.txt}
    - {name: unit-tests, command: node, args: ['-e', "console.log('${ok ? '18 passed' : '16 passed, 2 failed'}'); process.exit(${ok ? 0 : 1})"]}
`,
    )
    return p
  }
  const must = (r: { status: number | null; stdout: string; stderr: string }) => {
    if (r.status !== 0) throw new Error(r.stdout + r.stderr)
    return r.stdout
  }
  must(app.cli('project', 'create', 'webshop', '--root', execDir))
  for (const [name, task, ok] of [
    ['good', 'Add login rate limiting', true],
    ['bad', 'Refactor checkout totals', false],
  ] as const) {
    must(app.cli('workspace', 'create', name, '--project', 'webshop', '--config', mkCfg(name, task, ok), '--yes'))
    must(app.cli('run', 'create', '--workspace', `webshop/${name}`))
    const list = JSON.parse(must(app.cli('run', 'list', '--json'))) as { id: string; task: string }[]
    const id = list.find((x) => x.task === task)!.id
    must(app.cli('run', 'start', id, '--yes'))
    app.cli('run', 'verify', id) // the failing pipeline exits non-zero: that is the point
    must(app.cli('run', 'export', id, '-o', join(execDir, `${name}.runtide.zip`)))
  }

  const shot = async (name: string, nav: string | RegExp, prep?: () => Promise<void>) => {
    await page.getByRole('button', { name: nav }).first().click()
    await page.waitForTimeout(1500)
    if (prep) await prep()
    await page.screenshot({ path: join(out, `${name}.png`) })
  }

  // A reusable verification profile for the Verification page.
  const profFile = join(execDir, 'acceptance.yaml')
  writeFileSync(
    profFile,
    `name: acceptance
checks:
  - {name: build-output, type: file, path: result.txt}
  - {name: unit-tests, command: node, args: ['-e', "process.exit(0)"], timeout_sec: 60}
`,
  )
  must(app.cli('verification', 'save', 'acceptance', profFile, '--description', 'Build output exists and unit tests exit 0'))
  must(app.cli('project', 'update', 'webshop', '--default-profile', 'acceptance', '--description', 'Demo web shop'))

  await shot('overview', /^Overview/, async () => {
    await page.waitForTimeout(6000) // overview polls executions every 5s
  })
  await shot('processes', /^Processes/, async () => {
    await page.getByLabel("Filter processes").fill("server.js")
    await page.waitForTimeout(1500)
    await page.getByRole('row', { name: /node\.exe/ }).first().click()
  })
  await shot('ports', /^Ports/, async () => {
    await page.getByLabel('Filter ports').fill('4311')
    await page.waitForTimeout(1500)
    await page.getByRole('row', { name: /43111/ }).first().click()
  })
  await shot('services', /^Services/)
  await shot('stacks', /^Stacks/)
  await shot('projects', /^Projects/)
  await shot('task-workspaces', /^Workspaces/)
  await shot('executions', /^Executions/)
  await shot('execution-timeline', /^Executions/, async () => {
    await page.getByRole('row', { name: /Refactor checkout totals/ }).click()
    await page.getByRole('tab', { name: 'Timeline' }).click()
    await page.waitForTimeout(800)
  })
  await page.getByRole('tab', { name: 'Verification' }).click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: join(out, 'execution-verification.png') })
  await shot('verification', /^Verification/)
  await shot('receipts', /^Receipts/)
  await shot('compare', /^Compare/, async () => {
    const opts = await page.getByLabel('Baseline (A)').locator('option').allTextContents()
    const pick = (s: string) => opts.find((o) => o.includes(s))!
    await page.getByLabel('Baseline (A)').selectOption({ label: pick('Add login rate limiting') })
    await page.getByLabel('Candidate (B)').selectOption({ label: pick('Refactor checkout totals') })
    await page.getByRole('main').getByRole('button', { name: 'Compare', exact: true }).click()
    await page.waitForTimeout(1500)
  })
  await shot('integrations', /^Integrations/)
  await shot('receipt-replay-preflight', /^Receipts/, async () => {
    await page.getByRole('row', { name: /FAILED/ }).first().getByRole('button', { name: 'Inspect', exact: true }).click()
    await page.getByRole('button', { name: /Replay pre-flight/ }).click()
    await page.getByText('Permissions this replay needs').waitFor()
    await page.waitForTimeout(500)
  })
  await page.keyboard.press('Escape')
  await shot('settings', /^Settings/)

  await shot('logs', /^Service logs/)
  await shot('resources', /^Resources/)
  await shot('environment', /^Environment/, async () => {
    await page.getByRole('row', { name: /^Git/ }).waitFor({ timeout: 60_000 })
  })
  await shot('doctor', /^Doctor/, async () => {
    await page.getByRole('button', { name: 'Run diagnostics' }).click()
    await page.getByRole('button', { name: /^All \(/ }).waitFor({ timeout: 60_000 })
  })
  await page.keyboard.press('Control+k')
  await page.getByLabel('Search commands').fill('dark')
  await page.keyboard.press('Enter')
  await page.waitForTimeout(500)
  await shot('overview-dark', /^Overview/, async () => {
    await page.waitForTimeout(1000)
  })
  await shot('stacks-dark', /^Stacks/)

  app.cli('stack', 'down', 'fullstack-demo')
})
