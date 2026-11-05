// Text eines Ampel-Befunds (`StatusInsight`). Der Server liefert `key` und `params`;
// der Katalog `insights.*` kennt nur flache `{name}`-Platzhalter, deshalb werden
// Zeitspannen („seit 2 Tagen“) und Fehlerursachen hier vor dem Einsetzen zu Text.
// Ohne Schlüssel oder ohne Katalogtext gilt der deutsche Klartext des Servers.

import { i18n, t, type Params } from './i18n';

export interface InsightLike {
  message: string;
  key?: string;
  params?: Record<string, string>;
}

const MINUTE_MS = 60_000;

/**
 * Zeitspanne seit `since`, passend nach „seit …“. Über den Katalog statt über Intl:
 * das deutsche „seit 2 Tagen“ braucht den Dativ, den Einheitenformate („2 Tage“) nicht liefern.
 */
export function durationSince(since: string, now: number = Date.now()): string {
  const elapsed = now - new Date(since).getTime();
  // Ungültiges Datum oder Uhr in der Zukunft: lieber „weniger als eine Minute“ als „NaN Tage“.
  const minutes = Number.isFinite(elapsed) ? Math.max(0, Math.floor(elapsed / MINUTE_MS)) : 0;
  if (minutes < 1) return t('insights.duration.lessThanMinute');
  if (minutes < 60) return t('insights.duration.minutes', { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t('insights.duration.hours', { count: hours });
  return t('insights.duration.days', { count: Math.floor(hours / 24) });
}

const CAUSE_VARIANT: Record<string, (params: Record<string, string>) => string> = {
  lock_busy: (p) => (p.pid ? 'lock_busy' : 'lock_busyUnknown'),
  broken_deps: (p) => (p.packages ? 'broken_deps' : 'broken_depsUnknown'),
  disk_full: (p) => (p.path ? 'disk_full' : 'disk_fullUnknown'),
  // „Noch nicht gültig“ liegt fast immer an der Uhr des Servers, nicht an der Quelle.
  repo_error: (p) => (p.not_valid_yet === 'true' ? 'repo_errorClock' : p.repo ? 'repo_error' : 'repo_errorUnknown'),
};

const KNOWN_CAUSES = new Set([
  'lock_busy',
  'dpkg_interrupted',
  'broken_deps',
  'disk_full',
  'repo_error',
  'network',
  'timeout',
  'scanner_unavailable',
  'unknown',
]);

/** Ursache als Satz; eine unbekannte Klasse ergibt die Rückfallebene „unknown“. */
export function causeText(errorClass: string, params: Record<string, string> = {}): string {
  const cause = KNOWN_CAUSES.has(errorClass) ? errorClass : 'unknown';
  const variant = CAUSE_VARIANT[cause]?.(params) ?? cause;
  const process = params.process || t('insights.cause.anonymousProcess');
  return t(`insights.cause.${variant}`, { ...params, process });
}

/** Zählangaben müssen Zahlen sein, damit der Katalog die Pluralform wählt (der Server liefert Text). */
function typed(params: Record<string, string>): Params {
  const result: Params = { ...params };
  if (/^\d+$/.test(params.count ?? '')) result.count = Number(params.count);
  return result;
}

/** Ergänzt `duration` (aus `since`) sowie `cause` und `job` (aus `causeKey`, nur bei Handlungsunfähigkeit). */
function withDerived(params: Record<string, string>, now: number): Params {
  const result = typed(params);
  if (params.since) result.duration = durationSince(params.since, now);
  if (params.causeKey !== undefined) {
    result.cause = causeText(params.causeKey, params);
    result.job = params.job ? t('insights.jobSuffix', { job: params.job }) : '';
  }
  return result;
}

/** Der Text zum Befund in der Sprache der Oberfläche. */
export function insightText(insight: InsightLike, now: number = Date.now()): string {
  if (!insight.key) return insight.message;
  return i18n.tryT(`insights.${insight.key}`, withDerived(insight.params ?? {}, now)) ?? insight.message;
}
