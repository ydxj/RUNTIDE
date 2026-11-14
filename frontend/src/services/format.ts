export function bytes(n: number | undefined | null): string {
  if (n == null || Number.isNaN(n)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

export function duration(sec: number): string {
  if (sec < 60) return `${Math.floor(sec)}s`
  if (sec < 3600)
    return `${Math.floor(sec / 60)}m ${Math.floor(sec % 60)
      .toString()
      .padStart(2, '0')}s`
  if (sec < 172800) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`
  return `${Math.floor(sec / 86400)}d`
}

export function since(iso: string | undefined | null, now = Date.now()): string {
  if (!iso) return '-'
  const t = Date.parse(iso)
  if (Number.isNaN(t) || t < 86400000) return '-'
  return duration(Math.max(0, (now - t) / 1000)) + ' ago'
}

export function clock(iso: string): string {
  const d = new Date(iso)
  const p = (n: number, w = 2) => n.toString().padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

export function pct(n: number | undefined | null, digits = 0): string {
  if (n == null || Number.isNaN(n)) return '-'
  return `${n.toFixed(digits)}%`
}

/** Split a command-line style string into args, honouring double quotes. */
export function splitArgs(s: string): string[] {
  const out: string[] = []
  const re = /"([^"]*)"|(\S+)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(s)) !== null) out.push(m[1] ?? m[2] ?? '')
  return out
}

export function joinArgs(args: string[] | null | undefined): string {
  return (args ?? []).map((a) => (/\s/.test(a) ? `"${a}"` : a)).join(' ')
}

export function parseEnvLines(text: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const i = line.indexOf('=')
    if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1)
  }
  return env
}
