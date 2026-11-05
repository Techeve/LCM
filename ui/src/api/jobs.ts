// Jobs: Liste, Ausgabe, SSH-Sitzungen, Abbrechen.

import { api } from '../lib/api';
import { store } from '../lib/store';

export const JOBS_URL = '/api/v1/jobs';
export const JOB_FILTER_OPTIONS_URL = '/api/v1/jobs/filter-options';
export const SERVERS_URL = '/api/v1/servers';

export const PAGE_SIZE = 50;

export type JobSort = '-started_at' | 'started_at' | '-duration';
export const DEFAULT_SORT: JobSort = '-started_at';

/** Jobs, die gerade Aufmerksamkeit des Servers belegen oder darauf warten. */
export const ACTIVE_STATUSES = ['running', 'pending'] as const;
export const FINISHED_STATUSES = ['success', 'failed', 'aborted', 'blocked'] as const;
export const PROBLEM_STATUSES = ['failed', 'aborted', 'blocked'] as const;

export interface JobItem {
  id: string;
  server_id: number | null;
  server_name: string;
  kind: string;
  name: string;
  status: string;
  triggered_by: string;
  created_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  duration_seconds: number | null;
  exit_code: number | null;
}

export interface JobsPage {
  items: JobItem[];
  total: number;
}

export interface JobsQuery {
  serverId?: number;
  status: readonly string[];
  kind: readonly string[];
  q: string;
  /** Auslöser, genau wie in `triggered_by` des Jobs (`admin`, `scheduler` …); leer = alle. */
  triggeredBy?: string;
  since: string;
  until: string;
  /** Health-Checks (alle 15 Minuten, je Server) ausblenden. */
  hideHealth: boolean;
  sort: JobSort;
  limit: number;
  offset: number;
}

/** Adresse der Jobliste; leere Filter bleiben weg. */
export function jobsUrl(query: JobsQuery): string {
  const params = new URLSearchParams();
  if (query.serverId) params.set('server_id', String(query.serverId));
  if (query.status.length > 0) params.set('status', query.status.join(','));
  if (query.kind.length > 0) params.set('kind', query.kind.join(','));
  if (query.q) params.set('q', query.q);
  if (query.triggeredBy) params.set('triggered_by', query.triggeredBy);
  if (query.since) params.set('since', query.since);
  if (query.until) params.set('until', query.until);
  if (query.hideHealth) params.set('hide_health', '1');
  params.set('sort', query.sort);
  params.set('limit', String(query.limit));
  params.set('offset', String(query.offset));
  return `${JOBS_URL}?${params}`;
}

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function textOrNull(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null;
}

function numberOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function secondsBetween(start: string | null, end: string | null): number | null {
  if (!start || !end) return null;
  const seconds = (Date.parse(end) - Date.parse(start)) / 1000;
  return Number.isFinite(seconds) ? Math.max(0, Math.round(seconds)) : null;
}

/** Ein Job, wie ihn Liste, Dashboard und Verlauf liefern (`type`/`kind`, `server` eingebettet oder `server_name`). */
export function normaliseJob(raw: unknown): JobItem {
  const job: Raw = isRecord(raw) ? raw : {};
  const server = isRecord(job.server) ? job.server : {};
  const startedAt = textOrNull(job.started_at);
  const finishedAt = textOrNull(job.finished_at);
  return {
    id: text(job.id),
    server_id: numberOrNull(job.server_id),
    server_name: text(job.server_name) || text(server.name),
    kind: text(job.kind) || text(job.type),
    name: text(job.name),
    status: text(job.status),
    triggered_by: text(job.triggered_by),
    created_at: textOrNull(job.created_at),
    started_at: startedAt,
    finished_at: finishedAt,
    duration_seconds: numberOrNull(job.duration_seconds) ?? secondsBetween(startedAt, finishedAt),
    exit_code: numberOrNull(job.exit_code),
  };
}

// Die Jobliste kam früher als Verlauf (`/jobs/history`: `{items,total,page,page_size}`)
// oder als blanke Liste. Beide Formen gelten, bis die Listen-Endpunkte vereinheitlicht
// sind; danach bleibt nur `{items,total}`.
export function normaliseJobs(raw: unknown): JobsPage {
  const list = Array.isArray(raw) ? raw : isRecord(raw) && Array.isArray(raw.items) ? raw.items : [];
  const total = isRecord(raw) ? numberOrNull(raw.total) : null;
  return { items: list.map(normaliseJob), total: total ?? list.length };
}

export function isActive(status: string): boolean {
  return status === 'running' || status === 'pending';
}

/** Server für die Auswahl: die Liste kommt als Feld oder als `{items}`. */
export interface ServerRef {
  id: number;
  name: string;
}

export function normaliseServers(raw: unknown): ServerRef[] {
  const list = Array.isArray(raw) ? raw : isRecord(raw) && Array.isArray(raw.items) ? raw.items : [];
  return list
    .filter(isRecord)
    .map((entry) => ({ id: numberOrNull(entry.id) ?? 0, name: text(entry.name) }))
    .filter((entry) => entry.id > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
}

export interface JobFilterOptions {
  types: string[];
  triggered_by: string[];
}

export function normaliseFilterOptions(raw: unknown): JobFilterOptions {
  const record: Raw = isRecord(raw) ? raw : {};
  const strings = (value: unknown): string[] => (Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && v !== '') : []);
  return { types: strings(record.types), triggered_by: strings(record.triggered_by) };
}

// ---- Einzelner Job ----

export const jobOutputUrl = (id: string): string => `${JOBS_URL}/${encodeURIComponent(id)}/ssh-output`;
export const jobSessionsUrl = (id: string): string => `${JOBS_URL}/${encodeURIComponent(id)}/ssh-sessions`;

/** Antwort von `GET /jobs/:id/ssh-output`: der Job in Kurzform und seine Ausgabe. */
export interface JobOutput {
  job_id: string;
  name: string;
  status: string;
  exit_code: number | null;
  output: string;
}

export function normaliseOutput(raw: unknown): JobOutput {
  const record: Raw = isRecord(raw) ? raw : {};
  return {
    job_id: text(record.job_id),
    name: text(record.name),
    status: text(record.status),
    exit_code: numberOrNull(record.exit_code),
    output: text(record.output),
  };
}

export interface SshCommand {
  id: string;
  seq: number;
  started_at: string;
  duration_ms: number;
  command: string;
  output: string;
  exit_code: number;
  err?: string;
}

export interface SshSession {
  id: string;
  purpose: string;
  actor: string;
  host: string;
  user: string;
  opened_at: string;
  closed_at: string | null;
  command_count: number;
  had_error: boolean;
  commands?: SshCommand[];
}

export function normaliseSessions(raw: unknown): SshSession[] {
  return Array.isArray(raw) ? (raw as SshSession[]) : [];
}

/**
 * Was ein Job bisher ausgegeben hat. Ein laufender Job hat noch keine Gesamtausgabe
 * (sie wird erst beim Beenden gespeichert), seine Kommandos stehen aber schon im
 * SSH-Protokoll: daraus entsteht die Ausgabe, solange er läuft.
 */
export function liveOutput(output: JobOutput | undefined, sessions: SshSession[]): string {
  if (output && output.output !== '') return output.output;
  return sessions
    .flatMap((session) => session.commands ?? [])
    .sort((a, b) => a.started_at.localeCompare(b.started_at) || a.seq - b.seq)
    .map((command) => `$ ${command.command}\n${command.output}`.trimEnd())
    .join('\n\n');
}

/** Bricht den Job ab (hebt die Server-Sperre auf) und lädt Listen und Dashboard neu. */
export async function abortJob(id: string): Promise<void> {
  await api.post(`${JOBS_URL}/${encodeURIComponent(id)}/abort`);
  store.invalidate(JOBS_URL);
  store.invalidate('/api/v1/dashboard');
}
