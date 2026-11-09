// Container: Images, Container und Compose-Projekte aller Server. Die Daten stammen heute aus dem
// Docker-Check der Server (Adressen unter `/docker/…`); der Rest der Oberfläche spricht von Containern.

export const CONTAINER_IMAGES_URL = '/api/v1/docker/overview';
export const CONTAINERS_URL = '/api/v1/docker/containers';
export const COMPOSE_URL = '/api/v1/docker/compose';

/** Die Laufzeit, aus der die Daten stammen. */
export const RUNTIME = 'Docker';

export const PAGE_SIZE = 50;

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function count(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function rows(raw: unknown): Raw[] {
  return (Array.isArray(raw) ? raw : []).filter(isRecord);
}

export interface ServerRef {
  id: number;
  name: string;
}

function servers(raw: unknown): ServerRef[] {
  return rows(raw)
    .map((entry) => ({ id: count(entry.id), name: text(entry.name) }))
    .filter((entry) => entry.id > 0);
}

// ---- Images ----

export type ImageStatus = 'update' | 'unverifiable' | 'local' | 'current';

export interface ContainerImage {
  id: string;
  name: string;
  /** Ohne Tag lässt sich das Image nicht gezielt ziehen. */
  tag: string;
  servers: ServerRef[];
  serverCount: number;
  inUse: number;
  status: ImageStatus;
  critical: number;
  high: number;
}

/** Was die Statusspalte sagt; ein verfügbares Update geht vor allem anderen. */
export function imageStatus(raw: Raw): ImageStatus {
  if (raw.update_available === true) return 'update';
  if (raw.unverifiable === true) return 'unverifiable';
  if (raw.local_only === true) return 'local';
  return 'current';
}

export function normaliseImages(raw: unknown): ContainerImage[] {
  return rows(raw).map((r) => {
    const repository = text(r.repository);
    const tag = text(r.tag);
    const refs = servers(r.servers);
    return {
      id: `${repository}:${tag}`,
      name: tag ? `${repository}:${tag}` : repository,
      tag,
      servers: refs,
      serverCount: count(r.server_count) || refs.length,
      inUse: count(r.in_use_count),
      status: imageStatus(r),
      critical: count(r.critical_vulns),
      high: count(r.high_vulns),
    };
  });
}

// ---- Container ----

export interface ContainerItem {
  id: string;
  serverId: number;
  serverName: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string;
  project: string;
  service: string;
}

export function normaliseContainers(raw: unknown): ContainerItem[] {
  return rows(raw).map((r) => {
    const serverId = count(r.server_id);
    const name = text(r.name);
    return {
      id: `${serverId}/${name}`,
      serverId,
      serverName: text(r.server_name),
      name,
      image: text(r.image),
      state: text(r.state),
      status: text(r.status),
      ports: text(r.ports),
      project: text(r.compose_project),
      service: text(r.compose_service),
    };
  });
}

/** Zustände, die Aufmerksamkeit verdienen: „restarting“ und „dead“ sind Symptome, „exited“ oft Absicht. */
export type StateLevel = 'ok' | 'warn' | 'crit' | 'unknown';

export function stateLevel(state: string): StateLevel {
  if (state === 'running') return 'ok';
  if (state === 'restarting' || state === 'dead') return 'crit';
  if (state === 'paused') return 'warn';
  return 'unknown';
}

// ---- Compose-Projekte ----

export interface ComposeProject {
  id: string;
  project: string;
  servers: ServerRef[];
  services: string[];
  containers: number;
  running: number;
  workingDir: string;
}

export function normaliseCompose(raw: unknown): ComposeProject[] {
  return rows(raw).map((r) => ({
    id: text(r.project),
    project: text(r.project),
    servers: servers(r.servers),
    services: (Array.isArray(r.services) ? r.services : []).filter((s): s is string => typeof s === 'string'),
    containers: count(r.containers),
    running: count(r.running),
    workingDir: text(r.working_dir),
  }));
}

/** Nicht jeder Container des Projekts läuft: die Zeile, die man an einem Projekt zuerst sehen will. */
export function isIncomplete(project: ComposeProject): boolean {
  return project.running < project.containers;
}
