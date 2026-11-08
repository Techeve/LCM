// LCM selbst: Version und Build, Update-Stand und Selbst-Update.
// `GET /system/info` ist öffentlich (ohne Anmeldung nur Version und Build),
// `GET /system/update-info` braucht eine Sitzung, Prüfen und Selbst-Update `settings:manage`.

import { api } from '../lib/api';

export const SYSTEM_INFO_URL = '/api/v1/system/info';
export const UPDATE_INFO_URL = '/api/v1/system/update-info';
export const UPDATE_CHECK_URL = '/api/v1/system/update-check';
export const SELF_UPDATE_URL = '/api/v1/system/self-update';
export const RELEASE_NOTES_URL = '/api/v1/system/release-notes';
export const MAINTENANCE_URL = '/api/v1/system/maintenance';

type Raw = Record<string, unknown>;

function record(value: unknown): Raw {
  return typeof value === 'object' && value !== null ? (value as Raw) : {};
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

export interface SystemInfo {
  version: string;
  build: string;
  built_at: string;
  commit: string;
  dirty: boolean;
  platform: string;
  go_version: string;
}

export function normaliseSystemInfo(raw: unknown): SystemInfo {
  const r = record(raw);
  return {
    version: text(r.version),
    build: text(r.build),
    // Ohne Zeitstempel schreibt der Build „unbekannt“ hinein.
    built_at: Number.isNaN(Date.parse(text(r.built_at))) ? '' : text(r.built_at),
    commit: text(r.commit),
    dirty: r.dirty === true,
    platform: text(r.platform),
    go_version: text(r.go_version),
  };
}

export interface UpdateInfo {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  latest_url: string;
  checked_at: string;
  error: string;
  channel: string;
}

export function normaliseUpdateInfo(raw: unknown): UpdateInfo {
  const r = record(raw);
  return {
    current_version: text(r.current_version),
    latest_version: text(r.latest_version),
    update_available: r.update_available === true,
    latest_url: text(r.latest_url),
    checked_at: text(r.checked_at),
    error: text(r.error),
    channel: text(r.channel),
  };
}

export type SelfUpdatePhase = 'idle' | 'waiting' | 'backup' | 'running' | 'failed';
const PHASES: readonly SelfUpdatePhase[] = ['idle', 'waiting', 'backup', 'running', 'failed'];

export interface SelfUpdate {
  supported: boolean;
  /** Warum es nicht geht (Klartext vom Server); leer, wenn es geht. */
  reason: string;
  phase: SelfUpdatePhase;
  target_version: string;
  waiting_for: string[];
  job_id: string;
  backup_file: string;
  error: string;
}

export function normaliseSelfUpdate(raw: unknown): SelfUpdate {
  const r = record(raw);
  const phase = PHASES.find((p) => p === r.phase) ?? 'idle';
  return {
    supported: r.supported === true,
    reason: text(r.reason),
    phase,
    target_version: text(r.target_version),
    waiting_for: Array.isArray(r.waiting_for) ? r.waiting_for.filter((j): j is string => typeof j === 'string') : [],
    job_id: text(r.job_id),
    backup_file: text(r.backup_file),
    error: text(r.error),
  };
}

/** Warten auf Jobs, Sicherung oder Installation: Das Update läuft, die Seite soll es zeigen. */
export function selfUpdateBusy(status: SelfUpdate | null | undefined): boolean {
  return status?.phase === 'waiting' || status?.phase === 'backup' || status?.phase === 'running';
}

/** Die Sperre während des Selbst-Updates: Sie trifft jeden Benutzer, also sieht sie auch jeder. */
export interface Maintenance {
  active: boolean;
  phase: SelfUpdatePhase;
  target_version: string;
}

export function normaliseMaintenance(raw: unknown): Maintenance {
  const r = record(raw);
  return {
    active: r.active === true,
    phase: PHASES.find((p) => p === r.phase) ?? 'idle',
    target_version: text(r.target_version),
  };
}

/** Hinweise zum letzten Update; leer, solange es für diesen Benutzer nichts zu zeigen gibt. */
export interface ReleaseNotes {
  version: string;
  html: string;
}

export function readReleaseNotes(raw: unknown): ReleaseNotes {
  const data = record(raw);
  return { version: text(data.version), html: text(data.html) };
}

export const systemApi = {
  async checkNow(): Promise<UpdateInfo> {
    return normaliseUpdateInfo(await api.post(UPDATE_CHECK_URL));
  },
  async markReleaseNotesSeen(): Promise<void> {
    await api.post(`${RELEASE_NOTES_URL}/seen`);
  },
  /** Vor dem Einspielen sichert LCM immer; ohne Sicherung wird nicht aktualisiert. */
  async startSelfUpdate(): Promise<SelfUpdate> {
    return normaliseSelfUpdate(await api.post(SELF_UPDATE_URL, { with_backup: true }));
  },
  /** Nur solange das Update noch auf laufende Jobs wartet. */
  async cancelSelfUpdate(): Promise<SelfUpdate> {
    return normaliseSelfUpdate(await api.delete(SELF_UPDATE_URL));
  },
};
