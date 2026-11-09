// Was die Ansicht ausmacht, steht in der Adresse: Gruppe und Reiter im Pfad, Suche und offene
// Fenster in der Query. Diese Datei übersetzt zwischen Adresse und Zustand.

import type { QueryValue } from '../../lib/router';
import type { Trigger } from './rule-model';

export const GROUP_TABS = ['overview', 'servers', 'schedules', 'rules'] as const;
export type GroupTab = (typeof GROUP_TABS)[number];

/** Unbekannte Reiter führen zur Übersicht. */
export function parseTab(raw: string | undefined): GroupTab {
  return GROUP_TABS.find((tab) => tab === raw) ?? 'overview';
}

/** Die Gruppen-ID aus dem Pfad; `null` bei fehlender oder ungültiger ID. */
export function parseGroupId(raw: string | undefined): number | null {
  if (raw === undefined || !/^[1-9]\d*$/.test(raw)) return null;
  return Number(raw);
}

export function groupPath(id: number, tab: GroupTab = 'overview'): string {
  return tab === 'overview' ? `/groups/${id}` : `/groups/${id}/${tab}`;
}

export type Modal =
  | { kind: 'none' }
  | { kind: 'new-group' }
  | { kind: 'edit-group' }
  | { kind: 'new-schedule' }
  | { kind: 'edit-schedule'; id: number }
  | { kind: 'new-rule'; trigger: Trigger | null }
  | { kind: 'edit-rule'; id: number };

const NONE: Modal = { kind: 'none' };

function positiveInt(raw: string | null): number | null {
  return raw !== null && /^[1-9]\d*$/.test(raw) ? Number(raw) : null;
}

/** Die offene Fenster. Unvollständige Angaben (`modal=edit-rule` ohne `rule`) öffnen nichts. */
export function parseModal(query: URLSearchParams, hasGroup: boolean): Modal {
  const kind = query.get('modal');
  if (kind === 'new-group') return { kind };
  if (!hasGroup) return NONE;
  if (kind === 'edit-group' || kind === 'new-schedule') return { kind };
  if (kind === 'edit-schedule') {
    const id = positiveInt(query.get('schedule'));
    return id === null ? NONE : { kind, id };
  }
  if (kind === 'edit-rule') {
    const id = positiveInt(query.get('rule'));
    return id === null ? NONE : { kind, id };
  }
  if (kind === 'new-rule') {
    const raw = query.get('trigger');
    return { kind, trigger: raw === 'enforce' ? 'enforce' : positiveInt(raw) };
  }
  return NONE;
}

/** Query-Änderung, die genau diese Fenster öffnet (und alle anderen Angaben dazu löscht). */
export function modalQuery(modal: Modal): Record<string, QueryValue> {
  const cleared: Record<string, QueryValue> = { modal: null, schedule: null, rule: null, trigger: null };
  switch (modal.kind) {
    case 'none':
      return cleared;
    case 'edit-schedule':
      return { ...cleared, modal: modal.kind, schedule: modal.id };
    case 'edit-rule':
      return { ...cleared, modal: modal.kind, rule: modal.id };
    case 'new-rule':
      return { ...cleared, modal: modal.kind, trigger: modal.trigger };
    default:
      return { ...cleared, modal: modal.kind };
  }
}
