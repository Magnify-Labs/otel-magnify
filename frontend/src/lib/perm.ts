import type { Group } from '../types'

export type Permission =
  | 'workload:push_config'
  | 'config:read_content'
  | 'workload:validate_config'
  | 'config:create'
  | 'alert:resolve'
  | 'reports:export'
  | 'workload:archive'
  | 'workload:delete'
  | 'audit:view'
  | 'users:manage'
  | 'settings:manage'

const matrix: Record<string, Set<Permission>> = {
  viewer: new Set(),
  editor: new Set<Permission>([
    'workload:push_config',
    'config:read_content',
    'workload:validate_config',
    'config:create',
    'alert:resolve',
    'reports:export',
    'workload:archive',
  ]),
  administrator: new Set<Permission>([
    'workload:push_config',
    'config:read_content',
    'workload:validate_config',
    'config:create',
    'alert:resolve',
    'reports:export',
    'workload:archive',
    'workload:delete',
    'audit:view',
    'users:manage',
    'settings:manage',
  ]),
}

export function hasPerm(groups: Group[] | undefined, p: Permission): boolean {
  if (!groups) return false
  return groups.some((g) => matrix[g.role]?.has(p) ?? matrix[g.name]?.has(p) ?? false)
}
