import { test } from '@playwright/test'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { launch, type App } from './harness'

// Records the README demo (run -> verify -> receipt -> replay) from the REAL
// engine and UI: every frame is a screenshot of the running application, taken
// while it does the work. Nothing is mocked and no state is faked.
//
//   DEMO=1 DEMO_FRAMES=<empty dir> npx playwright test demo.spec.ts
//   go run ./build/tools/mkgif -manifest <dir>/manifest.json -out docs/screenshots/demo.gif
test.skip(!process.env.DEMO, 'set DEMO=1 (and DEMO_FRAMES) to record the README demo')

const framesDir = process.env.DEMO_FRAMES ?? join(tmpdir(), 'runtide-demo-frames')
let app: App
const root = mkdtempSync(join(tmpdir(), 'runtide-demo-'))

test.beforeAll(async () => {
  mkdirSync(framesDir, { recursive: true })
  app = await launch(45902)
})
test.afterAll(async () => {
  await app?.stop()
  rmSync(root, { recursive: true, force: true })
})

const must = (r: { status: number | null; stdout: string; stderr: string }) => {
  if (r.status !== 0) throw new Error(r.stdout + r.stderr)
  return r.stdout
}

/** A small real git repository: a rate limiter, its tests and a build step. */
function makeRepo() {
  const dir = join(root, 'shop')
  mkdirSync(join(dir, 'src'), { recursive: true })
  writeFileSync(
    join(dir, 'src', 'ratelimit.js'),
    `// Sliding-window limiter: at most \`max\` hits per \`windowMs\`.
exports.limiter = (max, windowMs) => {
  const hits = []
  return (now) => {
    while (hits.length && hits[0] <= now - windowMs) hits.shift()
    if (hits.length >= max) return false
    hits.push(now)
    return true
  }
}
`,
  )
  writeFileSync(
    join(dir, 'build.js'),
    `const fs = require('fs')
fs.mkdirSync('dist', { recursive: true })
fs.copyFileSync('src/ratelimit.js', 'dist/bundle.js')
console.log('compiling 1 module')
console.log('wrote dist/bundle.js')
`,
  )
  writeFileSync(
    join(dir, 'test.js'),
    `const { limiter } = require('./src/ratelimit')
const allow = limiter(3, 1000)
const results = [0, 10, 20, 30, 1500].map((t) => allow(t))
const want = [true, true, true, false, true]
let failed = 0
results.forEach((r, i) => {
  const ok = r === want[i]
  console.log((ok ? 'ok   ' : 'FAIL ') + 'login attempt ' + (i + 1) + ' -> ' + (r ? 'allowed' : 'blocked'))
  if (!ok) failed++
})
console.log(failed ? failed + ' failed' : '5 passed')
process.exit(failed ? 1 : 0)
`,
  )
  const g = (...a: string[]) => {
    const r = spawnSync('git', ['-C', dir, ...a], { encoding: 'utf8' })
    if (r.status !== 0) throw new Error(r.stderr)
  }
  g('init', '-q')
  g('config', 'user.email', 'demo@example.com')
  g('config', 'user.name', 'demo')
  g('add', '.')
  g('commit', '-q', '-m', 'rate limiter')
  const cfg = join(root, 'workspace.yaml')
  writeFileSync(
    cfg,
    `version: 1
task: Add login rate limiting
isolation: worktree
start:
  - {name: build, command: node, args: [build.js]}
verify:
  name: acceptance
  checks:
    - {name: bundle-exists, type: file, path: dist/bundle.js, contains: limiter}
    - {name: unit-tests, command: node, args: [test.js]}
`,
  )
  return { dir, cfg }
}

test('record the run -> verify -> receipt -> replay demo', async () => {
  test.setTimeout(240_000)
  const { page } = app
  await page.setViewportSize({ width: 1100, height: 690 })
  const repo = makeRepo()
  must(app.cli('project', 'create', 'shop', '--root', repo.dir))
  must(app.cli('workspace', 'create', 'auth', '--project', 'shop', '--config', repo.cfg, '--yes'))

  const frames: { file: string; delayMs: number }[] = []
  let last = Date.now()
  let n = 0
  const snap = async (holdMs?: number) => {
    const file = String(n++).padStart(3, '0') + '.png'
    await page.screenshot({ path: join(framesDir, file) })
    const now = Date.now()
    frames.push({ file, delayMs: holdMs ?? Math.min(now - last, 700) })
    last = Date.now()
  }
  /** Capture frames as the app works until `done` is true (or the time is up). */
  // Waiting time is shortened (each frame is shown for at most 40 ms) so the
  // demo fits in about 20 seconds; what is on screen is still the real state.
  const watch = async (done: () => Promise<boolean>, maxMs: number) => {
    const end = Date.now() + maxMs
    while (Date.now() < end) {
      await snap(40)
      if (await done()) return
    }
  }
  const step = async (active: number) => {
    await page.evaluate((a) => {
      let el = document.getElementById('demo-steps')
      if (!el) {
        el = document.createElement('div')
        el.id = 'demo-steps'
        el.style.cssText =
          'position:fixed;left:50%;bottom:12px;transform:translateX(-50%);z-index:2147483647;display:flex;gap:8px;pointer-events:none;font:600 13px system-ui,sans-serif'
        document.body.appendChild(el)
      }
      const names = ['1  Run', '2  Verify', '3  Receipt', '4  Replay']
      el.innerHTML = names
        .map(
          (t, i) =>
            `<span style="padding:6px 14px;border-radius:999px;color:#fff;background:${
              i === a ? '#2563eb' : 'rgba(30,41,59,.82)'
            };opacity:${i <= a ? 1 : 0.55}">${t}</span>`,
        )
        .join('')
    }, active)
  }

  // ---- 1. Run ------------------------------------------------------------
  await step(0)
  await page.getByRole('button', { name: /^Workspaces/ }).first().click()
  await page.getByRole('button', { name: /auth/ }).first().click()
  await page.getByRole('button', { name: /Review & approve/ }).waitFor()
  await snap(1500)
  await page.getByRole('button', { name: /Review & approve/ }).click()
  await page.getByRole('button', { name: 'Approve', exact: true }).waitFor()
  await page.waitForTimeout(600) // let the dialog finish its fade-in
  await snap(2200)
  await page.getByRole('button', { name: 'Approve', exact: true }).click()
  await page.getByText('commands approved').first().waitFor()
  await snap(900)
  await page.getByRole('button', { name: /Start execution/ }).click()
  await page.getByRole('tab', { name: 'Timeline' }).waitFor()
  await page.getByRole('tab', { name: 'Timeline' }).click()
  await page.getByRole('button', { name: 'Verify', exact: true }).waitFor({ timeout: 20_000 })
  await snap(2000) // the recorded timeline of the run
  await page.getByRole('tab', { name: 'Commands' }).click()
  await snap(1500)

  // ---- 2. Verify ---------------------------------------------------------
  await step(1)
  await page.getByRole('tab', { name: 'Verification' }).click()
  await snap(700)
  await page.getByRole('button', { name: 'Verify', exact: true }).click()
  await watch(() => page.getByText(/passed/i).first().isVisible(), 20_000)
  await snap(2200)

  // ---- 3. Receipt --------------------------------------------------------
  await step(2)
  const list = JSON.parse(must(app.cli('run', 'list', '--json'))) as { id: string }[]
  const receipt = join(root, 'auth.runtide.zip')
  must(app.cli('run', 'export', list[0].id, '-o', receipt))
  await page.getByRole('button', { name: /^Receipts/ }).first().click()
  await page.getByRole('button', { name: 'Inspect', exact: true }).first().waitFor()
  await snap(1500)
  await page.getByRole('button', { name: 'Inspect', exact: true }).first().click()
  await page.getByText(/Reproducibility/).waitFor()
  await page.waitForTimeout(600) // let the dialog finish its fade-in
  await snap(2800)

  // ---- 4. Replay ---------------------------------------------------------
  await step(3)
  await page.getByRole('button', { name: /Replay pre-flight/ }).click()
  await page.getByLabel('Project directory').fill(repo.dir)
  await page.getByLabel('Project directory').blur()
  await page.getByText('Permissions this replay needs').waitFor()
  await page.waitForTimeout(600) // let the dialog finish its fade-in
  await snap(2800)
  await page.getByRole('button', { name: /Review and run replay/ }).click()
  await page.getByRole('button', { name: 'Approve and run replay' }).waitFor()
  await page.waitForTimeout(600) // let the dialog finish its fade-in
  await snap(2200)
  await page.getByRole('button', { name: 'Approve and run replay' }).click()
  await watch(() => page.getByText(/reproduced/i).first().isVisible(), 40_000)
  await snap(3500)

  writeFileSync(join(framesDir, 'manifest.json'), JSON.stringify(frames, null, 1))
})
