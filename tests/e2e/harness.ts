import { chromium, type Browser, type Page } from '@playwright/test'
import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

export interface App {
  proc: ChildProcess
  browser: Browser
  page: Page
  home: string
  cli: (...args: string[]) => { status: number | null; stdout: string; stderr: string }
  stop: () => Promise<void>
}

const root = resolve(__dirname, '..', '..')
export const HOST = process.env.RUNTIDE_E2E_HOST ?? join(root, 'dist', 'e2ehost.exe')
export const CLI = process.env.RUNTIDE_CLI ?? join(root, 'dist', 'runtide.exe')

/**
 * Start the e2e host (real engine + built UI + a bridge replacing the Wails
 * binding) against an isolated data directory and open it in Microsoft Edge.
 */
export async function launch(port = 45900): Promise<App> {
  const home = mkdtempSync(join(tmpdir(), 'runtide-e2e-'))
  const env = { ...process.env, RUNTIDE_HOME: home }
  const proc = spawn(HOST, ['-port', String(port), '-dist', join(root, 'frontend', 'dist')], {
    env,
    stdio: ['pipe', 'pipe', 'inherit'],
    cwd: root,
  })
  await new Promise<void>((resolveReady, reject) => {
    const t = setTimeout(() => reject(new Error('e2e host did not start')), 20_000)
    proc.stdout?.on('data', (d: Buffer) => {
      if (d.toString().includes('e2ehost ready')) {
        clearTimeout(t)
        resolveReady()
      }
    })
    proc.once('exit', () => reject(new Error('e2e host exited early')))
  })
  const browser = await chromium.launch({ channel: process.env.RUNTIDE_E2E_CHANNEL ?? 'msedge' })
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  await page.goto(`http://127.0.0.1:${port}/`)
  await page.waitForSelector('nav[aria-label="Primary"]')
  return {
    proc,
    browser,
    page,
    home,
    cli: (...args) => {
      const r = spawnSync(CLI, args, { env, encoding: 'utf8' })
      return { status: r.status, stdout: r.stdout, stderr: r.stderr }
    },
    stop: async () => {
      await browser.close().catch(() => undefined)
      proc.stdin?.end() // host stops all services and exits when stdin closes
      await new Promise((r) => setTimeout(r, 4000))
      if (proc.exitCode === null) spawnSync('taskkill', ['/PID', String(proc.pid), '/T', '/F'])
      try {
        rmSync(home, { recursive: true, force: true })
      } catch {
        /* best effort */
      }
    },
  }
}

/** Create a project folder with two tiny Node servers and a workspace file. */
export function makeProject(ports: { db: number; api: number }) {
  const dir = mkdtempSync(join(tmpdir(), 'runtide-proj-'))
  mkdirSync(join(dir, 'db'))
  mkdirSync(join(dir, 'api'))
  writeFileSync(
    join(dir, 'db', 'server.js'),
    `require('net').createServer(s=>s.end()).listen(${ports.db},'127.0.0.1',()=>console.log('db ready'))\n`,
  )
  writeFileSync(
    join(dir, 'api', 'server.js'),
    `require('http').createServer((q,r)=>r.end('ok')).listen(${ports.api},'127.0.0.1',()=>console.log('api ready password=hunter2hunter2'))\n`,
  )
  const yaml = `name: e2e-shop
description: end-to-end test workspace
services:
  - name: db
    type: node
    command: node
    args: [server.js]
    working_directory: db
    healthcheck: { type: tcp, port: ${ports.db}, interval_sec: 1 }
  - name: api
    type: node
    command: node
    args: [server.js]
    working_directory: api
    depends_on: [db]
    healthcheck: { type: http, url: "http://127.0.0.1:${ports.api}/", interval_sec: 1 }
`
  writeFileSync(join(dir, 'runtide.yaml'), yaml)
  return { dir, yaml, cleanup: () => rmSync(dir, { recursive: true, force: true }) }
}
