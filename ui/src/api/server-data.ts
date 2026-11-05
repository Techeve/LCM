// Daten der übrigen Reiter im Serverdetail: Snaps, Container, Apps, Benutzer, Sicherheit, Deep-Scan, Speicher, SSH-Sitzungen.
// Jede Antwort wird tolerant gelesen; was die API nicht liefert, bleibt leer und wird nicht erfunden.

import { SERVERS_URL } from './servers';

type Raw = Record<string, unknown>;

const isRecord = (value: unknown): value is Raw => typeof value === 'object' && value !== null;
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const num = (value: unknown): number => (typeof value === 'number' && Number.isFinite(value) ? value : 0);
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.filter(isRecord) : []);
const date = (value: unknown): string | null => (typeof value === 'string' && value !== '' ? value : null);

export const snapsUrl = (id: number) => `${SERVERS_URL}/${id}/snaps`;
export const dockerUrl = (id: number) => `${SERVERS_URL}/${id}/docker`;
export const appsUrl = (id: number) => `${SERVERS_URL}/${id}/apps`;
export const vulnerabilitiesUrl = (id: number) => `${SERVERS_URL}/${id}/vulnerabilities`;
export const sshSessionsUrl = (id: number, limit = 50) => `${SERVERS_URL}/${id}/ssh-sessions?limit=${limit}`;

// ---- Snaps ----

export interface SnapItem {
  id: string;
  name: string;
  version: string;
  revision: string;
  channel: string;
  publisher: string;
  candidate: string;
}

export function normaliseSnaps(raw: unknown): SnapItem[] {
  return list(raw).map((r) => ({
    id: text(r.name),
    name: text(r.name),
    version: text(r.version),
    revision: text(r.revision),
    channel: text(r.channel),
    publisher: text(r.publisher),
    candidate: text(r.candidate_version),
  }));
}

// ---- Container ----

export interface ContainerItem {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string;
  project: string;
}

export interface ImageItem {
  id: string;
  ref: string;
  size: string;
  inUse: boolean;
  updateAvailable: boolean;
  critical: number;
  high: number;
}

export interface DockerData {
  containers: ContainerItem[];
  images: ImageItem[];
}

export function normaliseDocker(raw: unknown): DockerData {
  const r = isRecord(raw) ? raw : {};
  return {
    containers: list(r.containers).map((c) => ({
      id: text(c.id) || text(c.container_id),
      name: text(c.name),
      image: text(c.image),
      state: text(c.state),
      status: text(c.status),
      ports: text(c.ports),
      project: text(c.compose_project),
    })),
    images: list(r.images)
      .filter((i) => i.superseded !== true)
      .map((i) => ({
        id: text(i.id),
        ref: text(i.tag) ? `${text(i.repository)}:${text(i.tag)}` : text(i.repository),
        size: text(i.size_text),
        inUse: i.in_use === true,
        updateAvailable: i.update_available === true,
        critical: num(i.critical_vulns),
        high: num(i.high_vulns),
      })),
  };
}

// ---- Apps ----

export interface AppItem {
  id: string;
  name: string;
  version: string;
  latest: string;
  updateAvailable: boolean;
  path: string;
}

export interface AppsData {
  detected: AppItem[];
  unknown: number;
}

export function normaliseApps(raw: unknown): AppsData {
  const r = isRecord(raw) ? raw : {};
  return {
    detected: list(r.detected).map((a) => ({
      id: String(num(a.id) || text(a.slug)),
      name: text(a.name),
      version: text(a.version),
      latest: text(a.latest_version),
      updateAvailable: a.update_available === true,
      path: text(a.path),
    })),
    unknown: list(r.unknown).length,
  };
}

// ---- Benutzer ----

// ---- Sicherheit ----

// ---- SSH-Sitzungen ----

export interface SshRow {
  id: string;
  purpose: string;
  actor: string;
  user: string;
  openedAt: string | null;
  closedAt: string | null;
  commands: number;
  hadError: boolean;
}

export function normaliseSshSessions(raw: unknown): SshRow[] {
  return list(raw).map((s) => ({
    id: text(s.id),
    purpose: text(s.purpose),
    actor: text(s.actor),
    user: text(s.user),
    openedAt: date(s.opened_at),
    closedAt: date(s.closed_at),
    commands: num(s.command_count),
    hadError: s.had_error === true,
  }));
}
