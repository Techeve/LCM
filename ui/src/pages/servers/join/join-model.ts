// Reine Logik des Assistenten „Server hinzufügen“: Modus aus der Adresse, Prüfung der Eingaben, die Anfragen an
// das Backend und die Befehle für den Agent. Verbindlich prüft das Backend (checkHost, checkPort, Fingerprint).

import type { DsmBody, JoinAuthMethod, JoinBody, ReconnectBody, RouterOSBody } from '../../../api/provisioning';
import { LIMITS, charCount, hasControlChars, isHost } from '../../../lib/validate';

/** Beispielwerte für die Eingabefelder; in jeder Sprache gleich. */
export const EXAMPLES = {
  sshName: 'web01',
  sshHost: '10.0.0.5',
  agentName: 'notebook01',
  routerName: 'router01',
  routerHost: '192.168.88.1',
  nasName: 'nas01',
  nasHost: '192.168.1.20',
} as const;

export const JOIN_MODES = ['ssh', 'agent', 'routeros', 'dsm'] as const;
export type JoinMode = (typeof JOIN_MODES)[number];

export function modeOf(raw: string | null): JoinMode {
  return JOIN_MODES.find((mode) => mode === raw) ?? 'ssh';
}

/** Server-ID für „neu verbinden“ aus `?reconnect=42`, sonst `null`. */
export function reconnectIdOf(raw: string | null): number | null {
  return raw && /^\d+$/.test(raw) ? Number(raw) : null;
}

export interface SshForm {
  name: string;
  host: string;
  port: string;
  loginUser: string;
  password: string;
  authMethod: JoinAuthMethod;
  restricted: boolean;
}

export interface RouterOSForm {
  name: string;
  host: string;
  port: string;
  loginUser: string;
  password: string;
  authMethod: JoinAuthMethod;
}

export interface DsmForm {
  name: string;
  host: string;
  port: string;
  account: string;
  password: string;
}

export const emptySshForm = (): SshForm => ({ name: '', host: '', port: '22', loginUser: 'root', password: '', authMethod: 'password', restricted: false });
export const emptyRouterOSForm = (): RouterOSForm => ({ name: '', host: '', port: '22', loginUser: 'admin', password: '', authMethod: 'password' });
export const emptyDsmForm = (): DsmForm => ({ name: '', host: '', port: '5001', account: '', password: '' });

export type FieldProblem = 'required' | 'port' | 'host' | 'long' | 'line';
export type FormProblems<T> = Partial<Record<keyof T, FieldProblem>>;

/** Hostname oder IP ohne Schema, Pfad oder Leerzeichen (das Backend prüft die Zeichen noch einmal). */
export function hostProblem(host: string): FieldProblem | undefined {
  const value = host.trim();
  if (!value) return 'required';
  if (charCount(value) > LIMITS.host) return 'long';
  return isHost(value) ? undefined : 'host';
}

/** Ein Pflichtfeld in einer Zeile mit Höchstlänge: Name, Anmeldename, Konto. */
export function lineProblem(raw: string, max: number): FieldProblem | undefined {
  const value = raw.trim();
  if (!value) return 'required';
  if (charCount(value) > max) return 'long';
  return hasControlChars(raw) ? 'line' : undefined;
}

/** Ein Passwort darf leer sein (wo das erlaubt ist), aber nicht über die Grenze gehen. */
function passwordProblem(password: string): FieldProblem | undefined {
  return charCount(password) > LIMITS.password ? 'long' : undefined;
}

export function parsePort(raw: string): number | null {
  const value = raw.trim();
  if (!/^\d{1,5}$/.test(value)) return null;
  const port = Number(value);
  return port >= 1 && port <= 65535 ? port : null;
}

interface AddressFields {
  name: string;
  host: string;
  port: string;
}

/** Name (wenn verlangt), Adresse und Port – gemeinsam für alle Verbindungsarten. */
function addressProblems(form: AddressFields, withName: boolean): FormProblems<AddressFields> {
  const problems: FormProblems<AddressFields> = {};
  const name = withName ? lineProblem(form.name, LIMITS.name) : undefined;
  if (name) problems.name = name;
  const host = hostProblem(form.host);
  if (host) problems.host = host;
  if (parsePort(form.port) === null) problems.port = 'port';
  return problems;
}

/** Fehlende oder falsche Eingaben im SSH-Formular; ohne Namen bei „neu verbinden“. */
export function sshProblems(form: SshForm, withName = true): FormProblems<SshForm> {
  const problems: FormProblems<SshForm> = addressProblems(form, withName);
  const user = lineProblem(form.loginUser, LIMITS.loginUser);
  if (user) problems.loginUser = user;
  const password = form.authMethod === 'password' ? passwordProblem(form.password) : undefined;
  if (password) problems.password = password;
  return problems;
}

export function routerOSProblems(form: RouterOSForm): FormProblems<RouterOSForm> {
  const problems: FormProblems<RouterOSForm> = addressProblems(form, true);
  const user = lineProblem(form.loginUser, LIMITS.loginUser);
  if (user) problems.loginUser = user;
  const password = form.authMethod === 'password' ? passwordProblem(form.password) : undefined;
  if (password) problems.password = password;
  return problems; // ein leeres Passwort ist erlaubt (RouterOS-Werkszustand: admin ohne Passwort)
}

export function dsmProblems(form: DsmForm): FormProblems<DsmForm> {
  const problems: FormProblems<DsmForm> = addressProblems(form, true);
  const account = lineProblem(form.account, LIMITS.loginUser);
  if (account) problems.account = account;
  if (!form.password) problems.password = 'required';
  else if (charCount(form.password) > LIMITS.password) problems.password = 'long';
  return problems;
}

export const hasProblems = (problems: object) => Object.keys(problems).length > 0;

/** Das Passwort geht nur bei Passwort-Anmeldung mit; beim System-Schlüssel bleibt es leer. */
export function joinBody(form: SshForm, fingerprint: string): JoinBody {
  return { name: form.name.trim(), ...reconnectBody(form, fingerprint) };
}

export function reconnectBody(form: SshForm, fingerprint: string): ReconnectBody {
  return {
    host: form.host.trim(),
    port: parsePort(form.port) ?? 22,
    login_user: form.loginUser.trim(),
    login_password: form.authMethod === 'key' ? '' : form.password,
    auth_method: form.authMethod,
    confirmed_fingerprint: fingerprint,
    restricted_access: form.restricted,
  };
}

export function routerOSBody(form: RouterOSForm, fingerprint: string): RouterOSBody {
  return {
    name: form.name.trim(),
    host: form.host.trim(),
    port: parsePort(form.port) ?? 22,
    login_user: form.loginUser.trim(),
    login_password: form.authMethod === 'key' ? '' : form.password,
    auth_method: form.authMethod,
    confirmed_fingerprint: fingerprint,
  };
}

export function dsmBody(form: DsmForm, fingerprint: string): DsmBody {
  return {
    name: form.name.trim(),
    host: form.host.trim(),
    port: parsePort(form.port) ?? 5001,
    account: form.account.trim(),
    password: form.password,
    confirmed_fingerprint: fingerprint,
  };
}

/** Datei des Host-Schlüssels auf dem Zielserver, um den Fingerprint dort zu vergleichen. */
export function hostKeyFile(keyType: string): string {
  const type = keyType.toLowerCase();
  if (type.includes('ed25519')) return '/etc/ssh/ssh_host_ed25519_key.pub';
  if (type.includes('ecdsa')) return '/etc/ssh/ssh_host_ecdsa_key.pub';
  if (type.includes('rsa')) return '/etc/ssh/ssh_host_rsa_key.pub';
  return '/etc/ssh/ssh_host_*_key.pub';
}

export interface KnownServer {
  id: number;
  name: string;
  host: string;
  ssh_port: number;
}

/**
 * Schon angelegte Server mit derselben Adresse. Gleiche Adresse und gleicher Port lehnt das Backend ab
 * (dieselbe Maschine doppelt); gleiche Adresse mit anderem Port ist erlaubt (NAT, Portweiterleitung) und nur ein Hinweis.
 */
export function duplicateOf(servers: KnownServer[], host: string, port: number | null, exceptId: number | null = null) {
  const wanted = host.trim().toLowerCase();
  if (!wanted) return { taken: null, sameHost: null };
  const matches = servers.filter((server) => server.id !== exceptId && server.host.trim().toLowerCase() === wanted);
  const taken = matches.find((server) => server.ssh_port === (port ?? 22)) ?? null;
  return { taken, sameHost: taken ? null : (matches[0] ?? null) };
}

export interface AgentCommands {
  repo: string;
  install: string;
  enroll: string;
  manual: string;
}

/**
 * Befehle für den Agent. Der Agent verbindet sich auf seinem eigenen Port (gleicher Host wie im Browser); ohne
 * eigenen Port fällt die Enroll-Adresse auf die Adresse der Oberfläche zurück. Das Binary kommt über die
 * Oberfläche (`/api/v1/agent/download/amd64`).
 */
export function agentCommands(location: Pick<Location, 'origin' | 'protocol' | 'hostname'>, agentPort: number, token: string): AgentCommands {
  const agentBase = agentPort > 0 ? `${location.protocol}//${location.hostname}:${agentPort}` : location.origin;
  const enroll = `sudo lcm-agent enroll ${agentBase} ${token}`;
  return {
    repo: 'curl -fsSL https://repo.techeve.de/setup.sh | sudo sh',
    install: 'sudo apt install lcm-agent',
    enroll,
    manual: `sudo curl -fkLo /usr/local/bin/lcm-agent ${location.origin}/api/v1/agent/download/amd64 && sudo chmod +x /usr/local/bin/lcm-agent && ${enroll}`,
  };
}

/** Befehl zum Import des öffentlichen Schlüssels auf dem Router. */
export function routerOSImportCommand(loginUser: string): string {
  return `/user ssh-keys import public-key-file=lcm.pub user=${loginUser.trim() || 'admin'}`;
}
