import type { Listener, Process, Settings } from '../types'

export const DEFAULT_SETTINGS: Settings = {
  theme: 'light',
  refreshSec: 3,
  sampleSec: 2,
  logMemoryLines: 5000,
  logRetentionDays: 7,
  logMaxRows: 200000,
  redaction: true,
  extraRedaction: null,
  activityDays: 30,
  healthDays: 14,
  historyDays: 90,
  minimizeToTray: true,
  notifications: true,
  startWithWindows: false,
  stopServicesOnExit: true,
  trustedKeys: null,
}

export function proc(over: Partial<Process> & { pid: number; name: string }): Process {
  return {
    ppid: 1,
    path: `C:\\apps\\${over.name}`,
    cmdLine: `${over.name} --flag`,
    threads: 4,
    memoryBytes: 10_000_000,
    privateBytes: 8_000_000,
    cpuPercent: 0,
    cpuSeconds: 1,
    startTime: '2026-01-01T10:00:00Z',
    accessible: true,
    protection: '',
    ...over,
  }
}

export function listener(
  over: Partial<Listener> & { port: number; pid: number; processName: string },
): Listener {
  return {
    protocol: 'tcp',
    family: 'IPv4',
    address: '127.0.0.1',
    processPath: '',
    exposure: 'loopback',
    protection: '',
    stale: false,
    ...over,
  }
}
