// Was die Sicherheitsseite zeigt, steht in der Adresse (`/security/advisories?min=high&resolved=1&page=2`):
// der Reiter im Pfad, Filter, Seite und offener Befund in der Query. Diese Datei übersetzt zwischen
// Adresse und Zustand.

import { CVE_SOURCES, SEVERITIES, type AdvisoryQuery, type CveQuery, type CveSource } from '../../api/security';
import type { QueryValue } from '../../lib/router';

export const SECURITY_TABS = ['cve', 'advisories', 'caches'] as const;
export type SecurityTab = (typeof SECURITY_TABS)[number];

/** Pfadteil des Reiters; die CVE-Funde sind die Startseite. */
export function tabPath(tab: SecurityTab): string {
  return tab === 'cve' ? '/security' : `/security/${tab}`;
}

/** Unbekannte Reiter führen zu den CVE-Funden. */
export function parseTab(raw: string | undefined): SecurityTab {
  return SECURITY_TABS.find((tab) => tab === raw) ?? 'cve';
}

function pageOf(raw: string | null): number {
  const page = Number.parseInt(raw ?? '1', 10);
  return Number.isFinite(page) && page > 1 ? page : 1;
}

export function cveQueryFromUrl(query: URLSearchParams): CveQuery {
  const source = query.get('source') ?? '';
  return { source: (CVE_SOURCES as readonly string[]).includes(source) ? (source as CveSource) : '', page: pageOf(query.get('page')) };
}

export function advisoryQueryFromUrl(query: URLSearchParams): AdvisoryQuery {
  const min = query.get('min') ?? '';
  return {
    minSeverity: (SEVERITIES as readonly string[]).includes(min) ? (min as AdvisoryQuery['minSeverity']) : '',
    resolved: query.get('resolved') === '1',
    page: pageOf(query.get('page')),
  };
}

/** Der offene Befund (Fenster); leer, wenn keiner offen ist. */
export function openAdvisory(query: URLSearchParams): string {
  return query.get('advisory') ?? '';
}

/** Query-Änderung für `router.setQuery`. Jede Änderung außer der Seite springt zurück auf Seite 1. */
export function cvePatch(change: Partial<CveQuery>): Record<string, QueryValue> {
  const patch: Record<string, QueryValue> = {};
  if ('source' in change) patch.source = change.source || null;
  if ('page' in change) patch.page = change.page && change.page > 1 ? change.page : null;
  else patch.page = null;
  return patch;
}

export function advisoryPatch(change: Partial<AdvisoryQuery>): Record<string, QueryValue> {
  const patch: Record<string, QueryValue> = {};
  if ('minSeverity' in change) patch.min = change.minSeverity || null;
  if ('resolved' in change) patch.resolved = change.resolved ? 1 : null;
  if ('page' in change) patch.page = change.page && change.page > 1 ? change.page : null;
  else patch.page = null;
  return patch;
}

export function hasCveFilter(query: CveQuery): boolean {
  return query.source !== '';
}

export function hasAdvisoryFilter(query: AdvisoryQuery): boolean {
  return query.minSeverity !== '' || query.resolved;
}

/** „von–bis von gesamt“ einer Seite; `null`, wenn es nichts gibt. */
export function pageRange(page: number, pageSize: number, total: number): { from: number; to: number; more: boolean } | null {
  if (total <= 0) return null;
  const from = (page - 1) * pageSize + 1;
  const to = Math.min(total, page * pageSize);
  return { from, to, more: to < total };
}
