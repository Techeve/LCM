/**
 * Gemeinsame Formatierungs-Helfer der Seiten (Dashboard, ServerDetail,
 * Security) - einmal definiert statt pro Komponente dupliziert.
 *
 * Die Label-/Zeit-Helfer lesen die aktuelle Sprache aus dem i18n-Store; da
 * i18n.locale ein $state ist, rendern aufrufende Komponenten bei Sprachwechsel
 * automatisch neu.
 */
import { i18n } from '../stores/i18n.svelte.js';

/** BCP-47-Locale für Intl/toLocale* (aus der gewählten UI-Sprache). */
export function localeTag() {
  return i18n.locale === 'en' ? 'en-US' : 'de-DE';
}

/** Zahl in der aktuellen UI-Sprache gruppiert. */
export function fmtNum(n) {
  return Number(n).toLocaleString(localeTag());
}

/** Zeitstempel menschenlesbar; Nullwerte/Go-Zero-Time werden zu "nie". */
export function lastSeen(iso) {
  if (!iso) return i18n.t('common.never');
  const d = new Date(iso);
  if (d.getFullYear() < 2000) return i18n.t('common.never');
  return d.toLocaleString(localeTag());
}

/**
 * Speichergröße menschenlesbar in BINÄREN Einheiten (MiB/GiB/TiB), automatisch
 * skaliert nach Größenordnung. Der Eingabewert ist bereits in Mebibyte (die
 * Scans nutzen `free -m` und `df -BM`, beide 1024-basiert - trotz „MB"-Name).
 * Kleine Werte bleiben MiB, ab 1024 MiB GiB, ab 1024 GiB TiB. Die Nachkomma-
 * stellen richten sich nach der Einheit; die Zahl wird lokalisiert gruppiert.
 */
export function fmtSize(mb) {
  const m = Number(mb) || 0;
  if (m < 1024) return `${m.toLocaleString(localeTag())} MiB`;
  const gib = m / 1024;
  if (gib < 1024) return `${gib.toLocaleString(localeTag(), { minimumFractionDigits: 1, maximumFractionDigits: 1 })} GiB`;
  const tib = gib / 1024;
  return `${tib.toLocaleString(localeTag(), { minimumFractionDigits: 2, maximumFractionDigits: 2 })} TiB`;
}

// CVE-Schweregrad → Bootstrap-Badge-Klasse. Das Label kommt übersetzt aus i18n.
const SEVERITY_BADGE = {
  critical: 'bg-danger', high: 'bg-warning text-dark',
  medium: 'bg-info text-dark', low: 'bg-secondary', unknown: 'bg-light text-dark border',
};
const SEVERITY_KEYS = ['critical', 'high', 'medium', 'low', 'unknown'];

export function severityBadge(s) {
  return SEVERITY_BADGE[s] || 'bg-light text-dark border';
}

export function severityLabel(s) {
  return SEVERITY_KEYS.includes(s) ? i18n.t('severity.' + s) : s || i18n.t('severity.unknown');
}
