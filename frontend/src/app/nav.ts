import {
  Activity,
  Boxes,
  Cpu,
  FileCheck2,
  FolderGit2,
  FolderTree,
  Gauge,
  GitCompare,
  LayoutDashboard,
  ListChecks,
  Network,
  PlayCircle,
  Plug,
  Radio,
  ScrollText,
  Settings as SettingsIcon,
  Stethoscope,
  Workflow,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import type { PageId } from '../stores/ui'

export interface NavItem {
  id: PageId
  label: string
  icon: LucideIcon
  keywords: string
  group: 'Tasks' | 'Machine'
}

export const NAV: NavItem[] = [
  {
    id: 'overview',
    label: 'Overview',
    icon: LayoutDashboard,
    keywords: 'dashboard home summary',
    group: 'Tasks',
  },
  {
    id: 'projects',
    label: 'Projects',
    icon: FolderGit2,
    keywords: 'repository directory',
    group: 'Tasks',
  },
  {
    id: 'workspaces',
    label: 'Workspaces',
    icon: Workflow,
    keywords: 'task environment isolation worktree',
    group: 'Tasks',
  },
  {
    id: 'executions',
    label: 'Executions',
    icon: PlayCircle,
    keywords: 'runs history timeline',
    group: 'Tasks',
  },
  {
    id: 'live',
    label: 'Live Activity',
    icon: Radio,
    keywords: 'stream events output',
    group: 'Tasks',
  },
  {
    id: 'verification',
    label: 'Verification',
    icon: ListChecks,
    keywords: 'checks tests acceptance',
    group: 'Tasks',
  },
  {
    id: 'receipts',
    label: 'Receipts',
    icon: FileCheck2,
    keywords: 'evidence export import report replay',
    group: 'Tasks',
  },
  {
    id: 'compare',
    label: 'Compare',
    icon: GitCompare,
    keywords: 'diff executions regression',
    group: 'Tasks',
  },
  {
    id: 'services',
    label: 'Services',
    icon: Activity,
    keywords: 'start stop restart health',
    group: 'Machine',
  },
  {
    id: 'stacks',
    label: 'Stacks',
    icon: FolderTree,
    keywords: 'service group up down yaml',
    group: 'Machine',
  },
  {
    id: 'environment',
    label: 'Environment',
    icon: Wrench,
    keywords: 'tools env versions node php',
    group: 'Machine',
  },
  {
    id: 'integrations',
    label: 'Integrations',
    icon: Plug,
    keywords: 'mcp claude agent server signing',
    group: 'Machine',
  },
  {
    id: 'processes',
    label: 'Processes',
    icon: Cpu,
    keywords: 'pid task kill tree',
    group: 'Machine',
  },
  {
    id: 'ports',
    label: 'Ports',
    icon: Network,
    keywords: 'tcp listening conflict',
    group: 'Machine',
  },
  {
    id: 'logs',
    label: 'Service logs',
    icon: ScrollText,
    keywords: 'stdout stderr output',
    group: 'Machine',
  },
  {
    id: 'resources',
    label: 'Resources',
    icon: Gauge,
    keywords: 'cpu memory disk network',
    group: 'Machine',
  },
  {
    id: 'containers',
    label: 'Containers',
    icon: Boxes,
    keywords: 'docker wsl windows services',
    group: 'Machine',
  },
  {
    id: 'doctor',
    label: 'Doctor',
    icon: Stethoscope,
    keywords: 'diagnostics problems fix',
    group: 'Machine',
  },
  {
    id: 'settings',
    label: 'Settings',
    icon: SettingsIcon,
    keywords: 'preferences theme data',
    group: 'Machine',
  },
]
