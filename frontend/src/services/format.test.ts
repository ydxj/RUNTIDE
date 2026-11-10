import { bytes, duration, joinArgs, parseEnvLines, since, splitArgs } from './format'

describe('format', () => {
  it('formats bytes', () => {
    expect(bytes(0)).toBe('0 B')
    expect(bytes(1536)).toBe('1.5 KB')
    expect(bytes(5 * 1024 ** 3)).toBe('5.0 GB')
    expect(bytes(undefined)).toBe('-')
  })

  it('formats durations', () => {
    expect(duration(5)).toBe('5s')
    expect(duration(125)).toBe('2m 05s')
    expect(duration(3 * 3600 + 120)).toBe('3h 2m')
    expect(duration(3 * 86400)).toBe('3d')
  })

  it('formats relative time and ignores zero-time', () => {
    const now = Date.parse('2026-01-01T12:00:00Z')
    expect(since('2026-01-01T11:59:30Z', now)).toBe('30s ago')
    expect(since('0001-01-01T00:00:00Z', now)).toBe('-')
    expect(since(undefined)).toBe('-')
  })

  it('splits and joins args with quotes', () => {
    expect(splitArgs('run dev --name "my app"')).toEqual(['run', 'dev', '--name', 'my app'])
    expect(joinArgs(['run', 'my app'])).toBe('run "my app"')
    expect(joinArgs(null)).toBe('')
  })

  it('parses env lines', () => {
    expect(parseEnvLines('# c\nPORT=3000\n\nURL=http://x?a=b\nbad')).toEqual({
      PORT: '3000',
      URL: 'http://x?a=b',
    })
  })
})
