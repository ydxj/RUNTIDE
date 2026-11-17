import { test } from '@playwright/test'
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs'
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
  const { page } = app
  const proj = demoProject()
  const imp = app.cli('workspace', 'import', proj.file, '--yes')
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
  const imp2 = app.cli("workspace", "import", join(legacy, "runtide.yaml"), "--yes")
  if (imp2.status !== 0) throw new Error(imp2.stderr)
  const up = app.cli('workspace', 'up', 'fullstack-demo', '--yes')
  if (up.status !== 0) throw new Error(up.stderr)
  await page.waitForTimeout(7000) // let health checks settle and logs accumulate

  const shot = async (name: string, nav: string | RegExp, prep?: () => Promise<void>) => {
    await page.getByRole('button', { name: nav }).first().click()
    await page.waitForTimeout(1500)
    if (prep) await prep()
    await page.screenshot({ path: join(out, `${name}.png`) })
  }

  await shot('overview', /^Overview/)
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
  await shot('workspaces', /^Workspaces/)
  await shot('logs', /^Logs/)
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
  await shot('overview-dark', /^Overview/)
  await shot('workspaces-dark', /^Workspaces/)

  app.cli('workspace', 'down', 'fullstack-demo')
})
