// Ableitungen für die Anzeige einer Gruppe. Reine Funktionen.

import type { Group } from '../../api/groups';
import type { StatusLevel } from '../../components/shared';

export interface AclStatus {
  level: StatusLevel;
  key: string;
  params: { ok: number; total: number };
}

/** ACL-Zustand der Mitglieds-Server; ohne Server gibt es nichts zu sagen (`null`). */
export function aclStatus(group: Pick<Group, 'acl_servers' | 'acl_capable'>): AclStatus | null {
  const params = { ok: group.acl_capable, total: group.acl_servers };
  if (group.acl_servers <= 0) return null;
  if (group.acl_capable >= group.acl_servers) return { level: 'ok', key: 'groups.acl.full', params };
  if (group.acl_capable > 0) return { level: 'warn', key: 'groups.acl.partial', params };
  return { level: 'unknown', key: 'groups.acl.none', params };
}

/** Der Status eines Servers aus `/servers/statuses` als Ampelstufe. */
export function statusLevel(status: string | undefined): StatusLevel {
  switch (status) {
    case 'excellent':
      return 'great';
    case 'green':
      return 'ok';
    case 'yellow':
      return 'warn';
    case 'red':
      return 'crit';
    default:
      return 'unknown';
  }
}

export const STATUS_LABEL_KEYS: Record<StatusLevel, string> = {
  great: 'groups.status.great',
  ok: 'groups.status.ok',
  warn: 'groups.status.warn',
  crit: 'groups.status.crit',
  unknown: 'groups.status.unknown',
  running: 'groups.status.running',
};

/** Filtert die Liste nach Name und Beschreibung, ohne Groß-/Kleinschreibung; jedes Wort muss vorkommen. */
export function filterGroups(groups: Group[], query: string): Group[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return groups;
  return groups.filter((group) => {
    const haystack = `${group.name} ${group.description}`.toLowerCase();
    return words.every((word) => haystack.includes(word));
  });
}
