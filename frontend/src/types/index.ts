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
  trustedKeys: string[] | null
  execRetentionDays: number
  execMaxOutputMB: number
  execMaxEvents: number
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

// ---- execution platform (0.2) ----

export type RunState =
  | 'CREATED'
  | 'CONFIGURING'
  | 'READY'
  | 'RUNNING'
  | 'VERIFYING'
  | 'COMPLETED'
  | 'FAILED'
  | 'CANCELLED'
  | 'INTERRUPTED'
  | 'REQUIRES_ATTENTION'

export interface Project {
  id: string
  name: string
  root: string
  description: string
  gitRoot?: string
  createdAt: string
  archived: boolean
  archivedAt?: string
  settings: { defaultIsolation?: string; defaultProfile?: string }
}

export interface VerificationProfile {
  id: string
  name: string
  description: string
  pipeline: string
  createdAt: string
  updatedAt: string
}

export interface TaskWorkspace {
  id: string
  projectId: string
  name: string
  task: string
  state: RunState
  isolation: 1 | 2 | 3 | 4
  dir: string
  sourceDir: string
  worktree: boolean
  configYaml: string
  approved: boolean
  createdAt: string
  updatedAt: string
  finalStatus?: string
}

export interface WorkspacePlan {
  project: string
  name: string
  isolation: string
  dir: string
  creates: string[] | null
  warnings: string[] | null
  blockers: string[] | null
}

export interface Execution {
  id: string
  workspaceId: string
  projectId: string
  task: string
  state: RunState
  isolation: number
  startedAt?: string
  endedAt?: string
  createdAt: string
  runtideVersion: string
  error?: string
  verification?: string
}

export interface ExecEvent {
  id: string
  executionId: string
  seq: number
  time: string
  type: string
  source: 'observed' | 'reported' | 'inferred'
  severity: 'info' | 'warn' | 'error'
  subject?: string
  data?: Record<string, unknown>
}

export interface ExecCommand {
  id: string
  seq: number
  kind: string
  name: string
  executable: string
  args: string[] | null
  cwd: string
  startedAt: string
  endedAt?: string
  exitCode?: number
  pid?: number
  cancelled: boolean
  timedOut: boolean
  truncated: boolean
  redacted: boolean
  stdoutBytes: number
  stderrBytes: number
  error?: string
}

export interface VerificationResult {
  seq: number
  name: string
  description?: string
  type: string
  status: 'passed' | 'failed' | 'skipped' | 'timed_out' | 'not_applicable'
  required: boolean
  durationMs: number
  exitCode?: number
  output?: string
  error?: string
  needs?: string[]
  evidence?: string[]
}

export interface FileChange {
  path: string
  change: 'added' | 'modified' | 'deleted' | 'renamed'
  oldPath?: string
  sizeBefore: number
  sizeAfter: number
}

export interface ExecutionDetail {
  execution: Execution
  workspace: TaskWorkspace | null
  project: Project | null
  environment: {
    os?: string
    arch?: string
    isolation?: string
    tools?: { id: string; name: string; version: string; onPath: boolean }[]
    git?: { head?: string; branch?: string; dirty?: boolean }
    envKeys?: string[]
  }
  commands: ExecCommand[]
  processes: { commandId: string; pid: number; exe: string; startedAt: string; endedAt?: string }[]
  services: {
    name: string
    health: string
    healthDetail: string
    restarts: number
    lastState: string
  }[]
  verification: VerificationResult[]
  verificationSummary: { overall: string; counts: Record<string, number>; reasons?: string[] }
  fileChanges: FileChange[]
  eventCount: number
  active: boolean
  durationMs: number
}

export interface OutputLine {
  id: number
  commandId: string
  time: string
  stream: 'stdout' | 'stderr'
  line: string
}

export interface ReceiptRecord {
  id: string
  executionId: string
  projectName: string
  path: string
  sha256: string
  size: number
  imported: boolean
  status: string
  signedBy?: string
  createdAt: string
}

export interface ReplayDependency {
  kind: string
  name: string
  required: string
  available: string
  status: string
}

export interface ReplayPlan {
  class: string
  reasons: string[]
  steps: string[]
  needsEnv: string[]
  toolIssues: string[]
  missing: string[]
  blockers: string[]
  dependencies: ReplayDependency[]
  destructive: string[]
  permissions: string[]
  commit?: string
  outcome?: string
}

export interface ReplayResult {
  plan: ReplayPlan
  executionId: string
  state: string
  receiptPath: string
  outcome: 'reproduced' | 'partially_reproduced' | 'could_not_reproduce' | 'metadata_only'
  outcomeReasons: string[]
}

export interface ReceiptVerification {
  path: string
  sha256: string
  valid: boolean
  problems: string[] | null
  warnings: string[] | null
  filesChecked: number
  integrity: 'verified' | 'failed'
  signatureState: 'verified' | 'unavailable' | 'invalid'
  manifest?: {
    executionId: string
    projectName: string
    task: string
    status: string
    verification: string
    isolation: string
    reproducibility: string
    runtideVersion: string
    createdAt: string
    redaction: {
      commandsWithMaskedArgs: number
      sensitiveFilesExcluded: number
      diffsIncluded: boolean
    }
  }
  signature?: { fingerprint: string; valid: boolean; trusted: boolean }
  authenticity: string
}

export interface ReceiptInspect {
  receipt: {
    manifest: NonNullable<ReceiptVerification['manifest']>
    reproduction: { class: string; reasons: string[]; missing: string[] }
    commands: ExecCommand[]
    events: ExecEvent[]
    fileChanges: FileChange[]
    verification: { summary: { overall: string }; results: VerificationResult[] }
    sha256: string
  }
  verification: ReceiptVerification
  path: string
}

export interface Comparison {
  a: { executionId: string; status: string; verification: string; task: string }
  b: { executionId: string; status: string; verification: string; task: string }
  sameProject: boolean
  durations: { aMs: number; bMs: number; deltaMs: number }
  environment: {
    changed: string[] | null
    tools: { id: string; a: string; b: string; change: string }[] | null
  }
  commands: {
    removed: string[] | null
    new: string[] | null
    changed:
      | {
          command: string
          exitA: string
          exitB: string
          durationAMs: number
          durationBMs: number
          output: string
        }[]
      | null
    unchanged: number
  }
  verification: {
    overall: [string, string]
    newFailures: string[] | null
    resolvedFailures: string[] | null
    statusChanges: { name: string; a: string; b: string }[] | null
    onlyInA: string[] | null
    onlyInB: string[] | null
  }
  files: {
    onlyInA: string[] | null
    onlyInB: string[] | null
    changedContent: string[] | null
    identical: number
  }
  services: { name: string; a: string; b: string }[] | null
  failureEvents: { new: string[] | null; resolved: string[] | null }
  missingEvidence: string[] | null
  notes: string[] | null
}
