// Types mirror the JSON produced by the Go engine (internal/app and friends).

export interface Process {
  pid: number
  ppid: number
  name: string
  path: string
  cmdLine: string
  threads: number
  memoryBytes: number
  privateBytes: number
  cpuPercent: number
  cpuSeconds: number
  startTime: string
  accessible: boolean
  accessError?: string
  protection: '' | 'protected' | 'caution'
  protectionNote?: string
  managed?: string
}

export interface ProcessNode extends Process {
  children: ProcessNode[] | null
}

export interface Listener {
  protocol: 'tcp' | 'udp'
  family: 'IPv4' | 'IPv6'
  address: string
  port: number
  pid: number
  processName: string
  processPath: string
  exposure: 'loopback' | 'all-interfaces' | 'network'
  managed?: string
  protection: string
  conflict?: string
  stale: boolean
}

export interface RestartSpec {
  policy: 'never' | 'on-failure' | 'always'
  maxRetries: number
  backoffMs: number
  maxBackoffMs: number
}

export interface HealthSpec {
  type: 'http' | 'tcp' | 'process' | 'command'
  url?: string
  expectStatus?: number
  host?: string
  port?: number
  command?: string
  args?: string[]
  intervalSec: number
  timeoutSec: number
  startPeriodSec: number
  failureThreshold: number
}

export interface ServiceSpec {
  name: string
  type: string
  command: string
  args: string[] | null
  workingDirectory: string
  env: Record<string, string> | null
  envFile?: string
  dependsOn: string[] | null
  health?: HealthSpec | null
  restart: RestartSpec
  stopTimeoutSec: number
  startTimeoutSec: number
  autostart: boolean
}

export type ServiceState =
  'stopped' | 'starting' | 'running' | 'stopping' | 'backoff' | 'exited' | 'failed'
export type HealthStatus = 'healthy' | 'unhealthy' | 'starting' | 'unknown' | 'not-monitored'

export interface ServiceRuntime {
  key: string
  state: ServiceState
  pid: number
  startedAt: string
  uptimeSec: number
  exitCode: number | null
  lastError?: string
  restarts: number
  totalRestarts: number
  health: HealthStatus
  healthDetail?: string
  nextRetryAt?: string
}

export interface ServiceView {
  key: string
  workspace: string
  name: string
  spec: ServiceSpec
  root: string
  runtime?: ServiceRuntime
}

export interface WorkspaceRow {
  name: string
  root: string
  description: string
  yaml: string
  approved: boolean
  createdAt: string
  updatedAt: string
  health: string
  running: number
  total: number
}

export interface WorkspaceStatus {
  name: string
  health: string
  running: number
  total: number
  services: { name: string; layer: number; spec: ServiceSpec; runtime?: ServiceRuntime }[]
}

export interface WorkspacePreview {
  name: string
  root: string
  services: string[]
  commands: string[]
  order: string[][]
  warnings: string[] | null
  exists: boolean
}

export interface WorkspaceProgress {
  workspace: string
  service: string
  phase: string
  detail: string
  step: number
  total: number
}

export interface WorkspaceHistory {
  workspace: string
  time: string
  action: string
  outcome: string
  detail: string
}

export interface LogEntry {
  id: number
  service: string
  time: string
  stream: 'stdout' | 'stderr' | 'system'
  level: string
  line: string
}

export interface ResourceSample {
  time: string
  cpuPercent: number
  memTotal: number
  memAvailable: number
  memUsed: number
  memPercent: number
  commitTotal: number
  commitUsed: number
}

export interface Disk {
  path: string
  label: string
  fileSystem: string
  total: number
  free: number
  usedPercent: number
}

export interface Iface {
  name: string
  mtu: number
  up: boolean
  addrs: string[] | null
}

export interface CurrentResources {
  sample: ResourceSample | null
  disks: Disk[] | null
  interfaces: Iface[] | null
  intervalSec: number
}

export interface Overview {
  processes: number
  listening: number
  services: { Total: number; Running: number; Failed: number; Unhealthy: number }
  resources: CurrentResources
  recentProcesses: Process[] | null
  recentFailures: { time: string; kind: string; subject: string; detail: string }[] | null
  workspaces: { name: string; health: string; running: number; total: number }[] | null
  warnings: string[]
  generatedAt: string
}

export interface ToolInfo {
  id: string
  name: string
  category: string
  installed: boolean
  version: string
  path: string
  onPath: boolean
  method: string
  others: { path: string; version: string; onPath: boolean; method: string }[] | null
  note?: string
}

export interface EnvEntry {
  key: string
  value: string
  line: number
  sensitive: boolean
  empty: boolean
}
export interface EnvFile {
  path: string
  entries: EnvEntry[] | null
  duplicates: string[] | null
  invalid: { line: number; reason: string }[] | null
  secrets: { key: string; line: number; reason: string }[] | null
}
export interface EnvReport {
  dir: string
  files: EnvFile[] | null
  missing: { file: string; keys: string[] }[] | null
  hints: string[] | null
}

export interface Finding {
  id: string
  title: string
  severity: 'error' | 'warning' | 'info'
  description: string
  evidence: string[] | null
  component: string
  resolution: string
  fixAvailable: boolean
  fixId?: string
}
export interface DoctorReport {
  time: string
  findings: Finding[]
  errors: number
  warnings: number
  infos: number
}
export interface FixPlan {
  fixId: string
  description: string
  changes: string[]
}

export interface DockerStatus {
  cliInstalled: boolean
  cliPath: string
  clientVersion: string
  desktopInstalled: boolean
  desktopRunning: boolean
  daemonAvailable: boolean
  serverVersion: string
  message: string
}
export interface Container {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: string[] | null
  composeProject?: string
  composeService?: string
  cpuPercent?: string
  memUsage?: string
}
export interface WslStatus {
  installed: boolean
  path: string
  distros: { name: string; state: string; version: number; default: boolean }[] | null
  message: string
}
export interface WinService {
  name: string
  displayName: string
  state: string
  pid: number
  canStop: boolean
  protected: boolean
}

export interface Settings {
  theme: 'light' | 'dark' | 'system'
  refreshSec: number
  sampleSec: number
  logMemoryLines: number
  logRetentionDays: number
  logMaxRows: number
  redaction: boolean
  extraRedaction: string[] | null
  activityDays: number
  healthDays: number
  historyDays: number
  minimizeToTray: boolean
  notifications: boolean
  startWithWindows: boolean
  stopServicesOnExit: boolean
}

export interface DbInfo {
  path: string
  schemaVersion: number
  sizeBytes: number
  rows: Record<string, number>
  recovered: string
}

export interface RunRecord {
  service: string
  startedAt: string
  endedAt: string | null
  exitCode: number | null
  reason: string
  pid: number
}
