import {
  Activity,
  Boxes,
  Cpu,
  FolderTree,
  Gauge,
  LayoutDashboard,
  Network,
  ScrollText,
  Settings as SettingsIcon,
  Stethoscope,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import type { PageId } from '../stores/ui'

export interface NavItem {
  id: PageId
  label: string
  icon: LucideIcon
  keywords: string
}

export const NAV: NavItem[] = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard, keywords: 'dashboard home summary' },
  { id: 'processes', label: 'Processes', icon: Cpu, keywords: 'pid task kill tree' },
  { id: 'ports', label: 'Ports', icon: Network, keywords: 'tcp listening conflict' },
  { id: 'services', label: 'Services', icon: Activity, keywords: 'start stop restart health' },
  { id: 'workspaces', label: 'Workspaces', icon: FolderTree, keywords: 'project up down yaml' },
  { id: 'logs', label: 'Logs', icon: ScrollText, keywords: 'stdout stderr output' },
  { id: 'resources', label: 'Resources', icon: Gauge, keywords: 'cpu memory disk network' },
  {
    id: 'environment',
    label: 'Environment',
    icon: Wrench,
    keywords: 'tools env versions node php',
  },
  { id: 'containers', label: 'Containers', icon: Boxes, keywords: 'docker wsl windows services' },
  { id: 'doctor', label: 'Doctor', icon: Stethoscope, keywords: 'diagnostics problems fix' },
  { id: 'settings', label: 'Settings', icon: SettingsIcon, keywords: 'preferences theme data' },
]
