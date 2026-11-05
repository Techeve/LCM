// Konflikte zwischen Grundsatz-Regeln. Ein Server darf in vielen Gruppen sein. Tragen zwei davon
// eine Grundsatz-Regel desselben Typs mit Soll-Zustand (Firewall, APT-Cache), gilt nur die Regel
// der Gruppe mit dem kleineren Vorrang (bei Gleichstand die kleinere Gruppen-ID). So rechnet auch
// das Backend (`internal/core/services/enforce_priority.go`); hier wird es nur sichtbar gemacht.
// Die System-Gruppe gilt für alle Server.

import type { Group, Rule } from '../../api/groups';
import { CONFLICT_TYPE_IDS } from './rule-model';

export interface ConflictOther {
  groupId: number;
  groupName: string;
  priority: number;
}

export interface RuleConflict {
  ruleId: number;
  /** Server, auf denen eine andere Regel dieses Typs Vorrang hat; diese Regel wird dort zurückgestellt. */
  overriddenOn: number[];
  overriddenBy: ConflictOther[];
  /** Server, auf denen sich diese Regel gegen eine andere durchsetzt. */
  winsOn: number[];
  winsOver: ConflictOther[];
  /** Gleicher Vorrang: entschieden hat nur die Gruppen-ID. */
  tie: boolean;
}

interface Applicable {
  rule: Rule;
  groupName: string;
  priority: number;
}

function groupsOfServer(serverId: number, groups: Group[]): Group[] {
  return groups.filter((group) => group.is_system || group.servers.some((server) => server.id === serverId));
}

function addOther(list: ConflictOther[], other: ConflictOther): void {
  if (!list.some((entry) => entry.groupId === other.groupId)) list.push(other);
}

function conflictFor(map: Map<number, RuleConflict>, ruleId: number): RuleConflict {
  let conflict = map.get(ruleId);
  if (!conflict) {
    conflict = { ruleId, overriddenOn: [], overriddenBy: [], winsOn: [], winsOver: [], tie: false };
    map.set(ruleId, conflict);
  }
  return conflict;
}

function byPrecedence(a: Applicable, b: Applicable): number {
  return a.priority - b.priority || a.rule.group_id - b.rule.group_id || a.rule.id - b.rule.id;
}

/**
 * Die Konflikte der Regeln einer Gruppe, nach Regel-ID. `groups` müssen ihre Server nennen,
 * `rules` sind alle Regeln (aller sichtbaren Gruppen). Regeln ohne Konflikt fehlen.
 */
export function findConflicts(groupId: number, groups: Group[], rules: Rule[]): Map<number, RuleConflict> {
  const result = new Map<number, RuleConflict>();
  const group = groups.find((entry) => entry.id === groupId);
  if (!group) return result;
  const names = new Map(groups.map((entry) => [entry.id, entry]));
  const active = rules.filter((rule) => rule.enforce && rule.enabled && CONFLICT_TYPE_IDS.includes(rule.type));

  for (const server of group.servers) {
    const candidates: Applicable[] = [];
    for (const member of groupsOfServer(server.id, groups)) {
      for (const rule of active.filter((entry) => entry.group_id === member.id)) {
        candidates.push({ rule, groupName: member.name, priority: member.priority });
      }
    }
    for (const type of CONFLICT_TYPE_IDS) {
      const ordered = candidates.filter((entry) => entry.rule.type === type).sort(byPrecedence);
      const winner = ordered[0];
      if (!winner || ordered.length < 2) continue;
      for (const entry of ordered) {
        if (entry.rule.group_id !== groupId) continue;
        const conflict = conflictFor(result, entry.rule.id);
        if (entry === winner) {
          conflict.winsOn.push(server.id);
          for (const other of ordered.slice(1)) {
            const info = names.get(other.rule.group_id);
            addOther(conflict.winsOver, { groupId: other.rule.group_id, groupName: other.groupName, priority: info?.priority ?? other.priority });
            if (other.priority === entry.priority) conflict.tie = true;
          }
        } else {
          conflict.overriddenOn.push(server.id);
          addOther(conflict.overriddenBy, { groupId: winner.rule.group_id, groupName: winner.groupName, priority: winner.priority });
          if (winner.priority === entry.priority) conflict.tie = true;
        }
      }
    }
  }
  return result;
}

/** Gibt es in dieser Gruppe eine Regel, die ein anderer ausstechen wird? Für den Hinweis auf der Übersicht. */
export function overriddenCount(conflicts: Map<number, RuleConflict>): number {
  return [...conflicts.values()].filter((conflict) => conflict.overriddenOn.length > 0).length;
}
