// Einstellungen eines Servers: was LCM über ihn speichert (Name, Verbindung, Schalter für Betrieb und Docker,
// Konsole) und was LCM auf ihm einrichtet (root-Login, SSH-Port, Nameserver, Zeitzone, Zeitserver).

import { api } from '../lib/api';
import { SERVERS_URL } from './servers';
import type { ActionOutput } from './servers-packages';

type Raw = Record<string, unknown>;

const isRecord = (value: unknown): value is Raw => typeof value === 'object' && value !== null;
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const numberOr = (value: unknown, fallback: number): number => (typeof value === 'number' && Number.isFinite(value) ? value : fallback);

export const GLOBAL_SETTINGS_URL = '/api/v1/settings/global';
export const DEFAULT_GRACE_DAYS = 28;
export const MAX_DNS_SERVERS = 4;
export const MAX_NTP_SERVERS = 4;

/** Die Felder, die das Fenster gemeinsam speichert. */
export interface SettingsForm {
  name: string;
  host: string;
  port: number;
  console_disabled: boolean;
  maintenance: boolean;
  user_sync_disabled: boolean;
  unreachable_uncritical: boolean;
  unreachable_grace_days: number;
  docker_updates_disabled: boolean;
  docker_cves_ignored: boolean;
}

/** Was das Fenster sonst vom Server zeigt; diese Werte ändern sich nur über Aktionen auf dem Server. */
export interface ServerState {
  form: SettingsForm;
  hostname: string;
  /** IP-Adressen aus dem letzten Scan, durch Komma getrennt. */
  ipAddresses: string;
  osId: string;
  transport: string;
  proxmox: boolean;
  hasDocker: boolean;
  maintenanceSince: string | null;
  rootLoginDisabled: boolean;
  dnsServers: string[];
  timezone: string;
  ntpServers: string[];
  ntpService: string;
  ntpSynchronized: boolean;
  clockOffset: number;
  timeCheckedAt: string | null;
  virtualization: string;
}

const list = (value: unknown): string[] =>
  text(value)
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean);

export function normaliseServerState(raw: unknown): ServerState {
  const r = isRecord(raw) ? raw : {};
  return {
    form: {
      name: text(r.name),
      host: text(r.host),
      port: numberOr(r.ssh_port, 22),
      console_disabled: r.console_disabled === true,
      maintenance: r.maintenance === true,
      user_sync_disabled: r.user_sync_disabled === true,
      unreachable_uncritical: r.unreachable_uncritical === true,
      unreachable_grace_days: numberOr(r.unreachable_grace_days, 0) || DEFAULT_GRACE_DAYS,
      docker_updates_disabled: r.docker_updates_disabled === true,
      docker_cves_ignored: r.docker_cves_ignored === true,
    },
    hostname: text(r.hostname),
    ipAddresses: text(r.ip_addresses),
    osId: text(r.os_id),
    transport: text(r.transport),
    proxmox: text(r.proxmox_type) !== '',
    hasDocker: r.has_docker === true,
    maintenanceSince: text(r.maintenance_since) || null,
    rootLoginDisabled: r.ssh_root_login_disabled === true,
    dnsServers: list(r.dns_servers),
    timezone: text(r.timezone),
    ntpServers: list(r.ntp_servers),
    ntpService: text(r.ntp_service),
    ntpSynchronized: r.ntp_synchronized === true,
    clockOffset: numberOr(r.clock_offset_seconds, 0),
    timeCheckedAt: text(r.time_checked_at) || null,
    virtualization: text(r.virtualization),
  };
}

/**
 * Nur die geänderten Felder für `PATCH /settings`; die Konsole hat eine eigene Route mit eigenem Recht und fehlt
 * deshalb. Die Frist wird nur mitgeschickt, solange die Unerreichbarkeit als unkritisch gilt.
 */
export function settingsPatch(base: SettingsForm, form: SettingsForm): Record<string, unknown> {
  const patch: Record<string, unknown> = {};
  if (form.name.trim() !== base.name) patch.name = form.name.trim();
  if (form.host.trim() !== base.host) patch.host = form.host.trim();
  if (form.port !== base.port) patch.port = form.port;
  const flags = ['maintenance', 'user_sync_disabled', 'unreachable_uncritical', 'docker_updates_disabled', 'docker_cves_ignored'] as const;
  for (const flag of flags) if (form[flag] !== base[flag]) patch[flag] = form[flag];
  if (form.unreachable_uncritical && form.unreachable_grace_days !== base.unreachable_grace_days) patch.unreachable_grace_days = form.unreachable_grace_days;
  return patch;
}

/** Leere Felder fallen weg, doppelte auch; die Reihenfolge bleibt. */
export function cleanList(values: readonly string[]): string[] {
  return [...new Set(values.map((v) => v.trim()).filter(Boolean))];
}

/** Gleiche Einträge in gleicher Reihenfolge (ob sich eine Liste gegenüber dem Server geändert hat). */
export function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((value, i) => value === b[i]);
}

/** Feste Zahl von Eingabefeldern, vorbelegt mit der Liste. */
export function fillSlots(values: readonly string[], count: number): string[] {
  return Array.from({ length: count }, (_, i) => values[i] ?? '');
}

export interface Preset {
  label: string;
  value: string;
}

/** Vorgaben aus den globalen Einstellungen, je Zeile „Name = Wert“ oder nur der Wert. */
export function parsePresets(raw: string): Preset[] {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const i = line.indexOf('=');
      const value = (i >= 0 ? line.slice(i + 1) : line).trim();
      return { label: i >= 0 ? line.slice(0, i).trim() : value, value };
    })
    .filter((preset) => preset.value !== '');
}

export interface GlobalPresets {
  dns: Preset[];
  ntp: Preset[];
  timezone: string;
}

export function normalisePresets(raw: unknown): GlobalPresets {
  const r = isRecord(raw) ? raw : {};
  return { dns: parsePresets(text(r.dns_server_presets)), ntp: parsePresets(text(r.ntp_server_presets)), timezone: text(r.default_timezone) };
}

/** Setzt eine Vorgabe in das erste freie Feld; steht sie schon drin oder ist alles belegt, bleibt es, wie es ist. */
export function addPreset(slots: readonly string[], value: string): string[] {
  if (slots.some((slot) => slot.trim() === value)) return [...slots];
  const free = slots.findIndex((slot) => slot.trim() === '');
  if (free < 0) return [...slots];
  return slots.map((slot, i) => (i === free ? value : slot));
}

const output = (raw: unknown): ActionOutput => ({ output: isRecord(raw) ? text(raw.output) : '' });
const postOutput = async (url: string, body?: unknown) => output(await api.post<unknown>(url, body));

export const settingsApi = {
  update: (id: number, patch: Record<string, unknown>) => api.patch(`${SERVERS_URL}/${id}/settings`, patch),
  setConsoleDisabled: (id: number, disabled: boolean) => api.put(`${SERVERS_URL}/${id}/console`, { disabled }),
  setRootLogin: (id: number, disabled: boolean) => postOutput(`${SERVERS_URL}/${id}/ssh-root-login`, { disabled }),
  changeSshPort: (id: number, port: number) => postOutput(`${SERVERS_URL}/${id}/ssh-port`, { port }),
  configureDns: (id: number, servers: string[]) => postOutput(`${SERVERS_URL}/${id}/dns`, { servers }),
  timeCheck: (id: number) => api.post(`${SERVERS_URL}/${id}/time-check`),
  setTimezone: (id: number, timezone: string) => postOutput(`${SERVERS_URL}/${id}/timezone`, { timezone }),
  configureNtp: (id: number, servers: string[]) => postOutput(`${SERVERS_URL}/${id}/ntp`, { servers }),
};
