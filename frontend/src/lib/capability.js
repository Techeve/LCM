// Texthelfer zur Betriebsfähigkeit eines Servers (Ampel-Befunde und Karte).
//
// Der Server liefert Dauer und Fehlerursache als Rohdaten (Zeitstempel,
// Fehlerklasse samt Parametern). Die Sprachkataloge kennen aber nur flache
// {param}-Platzhalter - Zeitspannen und Ursachen müssen also VOR dem
// Nachschlagen zu Text werden. Das passiert hier, an einer Stelle, damit der
// Befund im Popover der Ampel und die Karte auf der Serverseite dieselben
// Worte benutzen.
import { i18n } from '../stores/i18n.svelte.js';

const t = (k, p) => i18n.t(k, p);

/**
 * Zeitspanne seit `since` als Text mit Einheit, passend nach „seit …" bzw.
 * „for …": „5 Minuten", „3 Stunden", „2 Tagen".
 *
 * Bewusst über den Katalog und nicht über Intl.NumberFormat: Das deutsche
 * „seit 2 Tagen" braucht den Dativ, den die Intl-Einheitenformate („2 Tage")
 * nicht liefern.
 */
export function durationSince(since, now = Date.now()) {
  const ms = now - new Date(since).getTime();
  // Ungültiges Datum oder Uhrabweichung in die Zukunft: lieber „weniger als
  // eine Minute" als „NaN Tage" oder eine negative Zahl.
  const minutes = Number.isFinite(ms) ? Math.max(0, Math.floor(ms / 60000)) : 0;
  if (minutes < 1) return t('capabilityDuration.lessThanMinute');
  if (minutes < 60) return t('capabilityDuration.minutes', { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t('capabilityDuration.hours', { count: hours });
  return t('capabilityDuration.days', { count: Math.floor(hours / 24) });
}

// Fehlerklasse → Katalogschlüssel unter capabilityCause. Fehlt ein Parameter,
// den der Satz der Klasse braucht, wählt die Funktion die Fassung ohne ihn -
// ein Satz mit „{pid}" im Text wäre schlimmer als ein allgemeinerer.
const CAUSE_VARIANT = {
  lock_busy: (p) => (p.pid ? 'lock_busy' : 'lock_busyUnknown'),
  broken_deps: (p) => (p.packages ? 'broken_deps' : 'broken_depsUnknown'),
  disk_full: (p) => (p.path ? 'disk_full' : 'disk_fullUnknown'),
  repo_error: (p) => {
    // Eine Quelle, die „noch nicht gültig" ist, liegt fast immer an der Uhr
    // des Servers - nicht an der Quelle.
    if (p.not_valid_yet === 'true') return 'repo_errorClock';
    return p.repo ? 'repo_error' : 'repo_errorUnknown';
  },
};

const KNOWN_CLASSES = [
  'lock_busy',
  'dpkg_interrupted',
  'broken_deps',
  'disk_full',
  'repo_error',
  'network',
  'timeout',
  'scanner_unavailable',
  'unknown',
];

/** Ursache als Satz. Unbekannte oder leere Klasse ergibt die Rückfallebene „unknown". */
export function causeText(errorClass, params = {}) {
  const cls = KNOWN_CLASSES.includes(errorClass) ? errorClass : 'unknown';
  const key = (CAUSE_VARIANT[cls] ?? (() => cls))(params ?? {});
  // Ein Sperr-Halter ohne Namen ist möglich (nur die PID ist bekannt).
  const process = params?.process || t('capabilityCause.anonymousProcess');
  return t('capabilityCause.' + key, { ...params, process });
}

/**
 * Ergänzt die Parameter eines Befunds um die berechneten Texte:
 *  - since    → duration (Zeitspanne),
 *  - causeKey → cause (Ursache als Satz),
 *  - job      → job als Anhang „ - Job: …" (leer, wenn kein Job bekannt ist);
 *               nur bei Handlungsunfähigkeits-Befunden (causeKey vorhanden).
 * Alles andere bleibt unverändert; Befunde ohne diese Parameter laufen
 * unberührt durch.
 */
export function enrichInsightParams(params) {
  if (!params) return params;
  const out = { ...params };
  if (params.since) out.duration = durationSince(params.since);
  if (params.causeKey !== undefined) {
    out.cause = causeText(params.causeKey, params);
    out.job = params.job ? t('capability.jobSuffix', { job: params.job }) : '';
  }
  return out;
}
