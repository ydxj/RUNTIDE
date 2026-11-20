import { useState } from 'react'
import { Copy, KeyRound } from 'lucide-react'
import { usePoll } from '../../hooks/usePoll'
import { call } from '../../services/api'
import { errMsg, reportError, useUI } from '../../stores/ui'
import type { Settings } from '../../types'
import { Badge, Button, Input, PageHeader, Panel } from '../../components/ui/primitives'

function Snippet({ title, text }: { title: string; text: string }) {
  const toast = useUI((s) => s.toast)
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs font-medium text-muted">
        {title}
        <button
          type="button"
          className="flex items-center gap-1 text-accent"
          onClick={() =>
            void navigator.clipboard
              .writeText(text)
              .then(() => toast({ kind: 'info', title: 'Copied' }))
          }
        >
          <Copy className="h-3 w-3" /> Copy
        </button>
      </div>
      <pre className="selectable overflow-auto rounded border border-line bg-subtle p-2 font-mono text-xs leading-5">
        {text}
      </pre>
    </div>
  )
}

/** Agent integration (MCP), receipt signing, and the self-hosted server. */
export function IntegrationsPage() {
  const settings = useUI((s) => s.settings)
  const setSettings = useUI((s) => s.setSettings)
  const key = usePoll(
    () => call<{ exists: boolean; fingerprint?: string; path?: string }>('receipts.keyinfo'),
    [],
    0,
  )
  const ver = usePoll(() => call<{ home: string }>('version'), [], 0)
  const [trust, setTrust] = useState('')
  const [err, setErr] = useState<string>()
  const trusted = settings?.trustedKeys ?? []

  const keygen = async () => {
    try {
      await call('receipts.keygen')
      void key.reload()
    } catch (e) {
      reportError('Could not create key', e)
    }
  }
  const addTrust = async () => {
    try {
      setSettings(
        await call<Settings>('settings.set', {
          trustedKeys: [...trusted, trust.trim().toLowerCase()],
        }),
      )
      setTrust('')
      setErr(undefined)
    } catch (e) {
      setErr(errMsg(e))
    }
  }
  const removeTrust = async (k: string) =>
    setSettings(
      await call<Settings>('settings.set', { trustedKeys: trusted.filter((x) => x !== k) }),
    )

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      <PageHeader
        title="Integrations"
        subtitle="Connect coding agents, sign receipts, and share them with a self-hosted server."
      />
      <div className="grid max-w-4xl gap-3 px-6 pb-8">
        <Panel title="AI agents (Model Context Protocol)">
          <div className="space-y-3 p-3 text-sm">
            <p>
              The <code>runtide-mcp</code> server lets an MCP-capable agent create workspaces, run
              verification and read evidence. The permission level is chosen here, by you, when you
              register it: the agent cannot raise it. Agents can never approve a workspace's
              commands; you do that in this app or with <code>runtide workspace approve</code>.
            </p>
            <Snippet
              title="Claude Code (read + controlled actions)"
              text={'claude mcp add runtide -- "<install dir>\\runtide-mcp.exe" --level controlled'}
            />
            <Snippet
              title="Read-only (inspection only)"
              text={
                'claude mcp add runtide-readonly -- "<install dir>\\runtide-mcp.exe" --level read'
              }
            />
            <p className="text-xs text-muted">
              Levels: <b>read</b> inspect · <b>controlled</b> + create workspaces/executions, start
              approved workspaces, verify, export · <b>sensitive</b> + ad-hoc commands, reconfigure,
              cancel, delete (workspace policy still applies). Other agents: any MCP client that can
              launch a stdio server, or the documented CLI with <code>--json</code>. See
              docs/MCP_INTEGRATION.md.
            </p>
          </div>
        </Panel>
        <Panel title="Receipt signing">
          <div className="space-y-3 p-3 text-sm">
            {key.data?.exists ? (
              <p className="selectable">
                Signing key fingerprint: <code>{key.data.fingerprint}</code>{' '}
                <span className="text-muted">
                  (private key at {key.data.path}; keep it private)
                </span>
              </p>
            ) : (
              <div className="flex items-center gap-3">
                <span className="text-muted">No signing key yet.</span>
                <Button icon={<KeyRound className="h-3.5 w-3.5" />} onClick={() => void keygen()}>
                  Create signing key
                </Button>
              </div>
            )}
            <div>
              <div className="mb-1 text-xs font-medium text-muted">Trusted signer fingerprints</div>
              <ul className="mb-2 space-y-1">
                {trusted.map((k) => (
                  <li key={k} className="flex items-center gap-2">
                    <code>{k}</code>
                    <Button variant="ghost" onClick={() => void removeTrust(k)}>
                      Remove
                    </Button>
                  </li>
                ))}
                {trusted.length === 0 && (
                  <li className="text-muted">
                    None. Signed receipts verify, but show as "not trusted".
                  </li>
                )}
              </ul>
              <div className="flex gap-2">
                <Input
                  aria-label="Fingerprint to trust"
                  className="w-64 font-mono"
                  placeholder="16 hex characters"
                  value={trust}
                  onChange={(e) => setTrust(e.target.value)}
                />
                <Button onClick={() => void addTrust()} disabled={trust.length !== 16}>
                  Trust key
                </Button>
              </div>
              {err && (
                <div role="alert" className="mt-1 text-sm text-bad">
                  {err}
                </div>
              )}
              <p className="mt-1 text-xs text-muted">
                Get a colleague's fingerprint from them directly (not from the receipt itself): a
                signature only proves the same key signed it.
              </p>
            </div>
          </div>
        </Panel>
        <Panel title="Self-hosted server" actions={<Badge>optional</Badge>}>
          <div className="space-y-3 p-3 text-sm">
            <p>
              The server stores receipts and execution history for a team, with accounts and project
              roles. It never runs commands. It listens on loopback only until you configure TLS and
              remote access (docs/SELF_HOSTING.md).
            </p>
            <Snippet title="Start locally" text="runtide server start" />
            <Snippet
              title="Upload a receipt (token from the server: POST /api/v1/tokens)"
              text={
                '$env:RUNTIDE_SERVER_TOKEN = "rtk_..."\nruntide receipt push receipt.runtide.zip --server https://runtide.example.com --project shop'
              }
            />
          </div>
        </Panel>
        <p className="text-xs text-muted">
          Data directory: <span className="selectable font-mono">{ver.data?.home}</span>
        </p>
      </div>
    </div>
  )
}
