// Öffentliche Aktivierung (LCM-Konto, Linux-Benutzer) und die Aufnahme neuer Server.
// Die Aktivierungen laufen ohne Anmeldung: Ein 401 heißt dort nicht „Sitzung abgelaufen“.

import { api } from '../lib/api';

const anonymous = { anonymous: true };

export interface AccountActivationResult {
  status: string;
  username: string;
}

/** Löst einen Aktivierungs- oder Zurücksetzen-Link ein und setzt das Passwort des LCM-Kontos. */
export function consumeAccountActivation(token: string, password: string): Promise<AccountActivationResult> {
  return api.post<AccountActivationResult>('/api/v1/users/activation-links/consume', { token, password }, anonymous);
}

export type LinuxKeyMode = 'none' | 'paste' | 'generate';

export interface LinuxActivationInput {
  token: string;
  password: string;
  keyMode: LinuxKeyMode;
  keyName: string;
  publicKey: string;
}

export interface LinuxActivationResult {
  status: string;
  username: string;
  /** Nur bei „Schlüsselpaar erzeugen“: genau einmal ausgeliefert, nirgends gespeichert. */
  private_key?: string;
}

export function linuxActivationBody(input: LinuxActivationInput) {
  return {
    token: input.token,
    password: input.password,
    key_name: input.keyMode === 'none' ? '' : input.keyName.trim(),
    public_key: input.keyMode === 'paste' ? input.publicKey.trim() : '',
    generate_key: input.keyMode === 'generate',
  };
}

export function consumeLinuxActivation(input: LinuxActivationInput): Promise<LinuxActivationResult> {
  return api.post<LinuxActivationResult>('/api/v1/linux-users/activation-links/consume', linuxActivationBody(input), anonymous);
}

// ---- Server aufnehmen ----

export type JoinAuthMethod = 'password' | 'key';

/** Was die Aufnahme-Endpunkte vom neuen Server zurückgeben (Ausschnitt). */
export interface JoinedServer {
  id: number;
  name: string;
  host: string;
  ssh_port?: number;
  service_user?: string;
  restricted_sudo?: boolean;
  os_name?: string;
  os_version?: string;
  transport?: string;
}

export interface HostKeyProbe {
  fingerprint: string;
  key_type: string;
}

export interface JoinBody {
  name: string;
  host: string;
  port: number;
  login_user: string;
  login_password: string;
  auth_method: JoinAuthMethod;
  confirmed_fingerprint: string;
  restricted_access: boolean;
}

export type ReconnectBody = Omit<JoinBody, 'name'>;

export interface RouterOSBody {
  name: string;
  host: string;
  port: number;
  login_user: string;
  login_password: string;
  auth_method: JoinAuthMethod;
  confirmed_fingerprint: string;
}

export interface RouterOSResult {
  server: JoinedServer;
  /** Nur bei Anmeldung per Schlüssel: der öffentliche Schlüssel zum Import auf dem Router. */
  public_key?: string;
}

export interface DsmBody {
  name: string;
  host: string;
  port: number;
  account: string;
  password: string;
  confirmed_fingerprint: string;
}

export interface AgentResult {
  server: JoinedServer;
  /** Wird genau einmal ausgeliefert; LCM speichert nur einen Hash. */
  token: string;
  /** Pin des Zertifikatsschlüssels für curl --pinnedpubkey, leer ohne TLS. */
  download_pin?: string;
}

/** Liest den Host-Schlüssel des Zielservers, ohne Zugangsdaten zu senden. */
export function probeHostKey(host: string, port: number): Promise<HostKeyProbe> {
  return api.post<HostKeyProbe>('/api/v1/servers/probe', { host, port });
}

export function joinServer(body: JoinBody): Promise<JoinedServer> {
  return api.post<JoinedServer>('/api/v1/servers/join', body);
}

export function reconnectServer(id: number, body: ReconnectBody): Promise<JoinedServer> {
  return api.post<JoinedServer>(`/api/v1/servers/${id}/reconnect`, body);
}

export function createAgentServer(name: string): Promise<AgentResult> {
  return api.post<AgentResult>('/api/v1/servers/agent', { name });
}

export function createRouterOS(body: RouterOSBody): Promise<RouterOSResult> {
  return api.post<RouterOSResult>('/api/v1/servers/routeros', body);
}

/** Liest den Zertifikats-Fingerprint des DSM, bevor Zugangsdaten über die Leitung gehen. */
export async function probeDsm(host: string, port: number): Promise<string> {
  const result = await api.post<{ cert_fingerprint: string }>('/api/v1/servers/dsm/probe', { host, port });
  return result.cert_fingerprint;
}

export function createDsm(body: DsmBody): Promise<JoinedServer> {
  return api.post<JoinedServer>('/api/v1/servers/dsm', body);
}

/** Öffentlicher Teil des System-SSH-Schlüssels; ohne `settings:manage` leer. */
export async function onboardingPublicKey(): Promise<string> {
  try {
    const settings = await api.get<{ onboarding_pub_key?: string }>('/api/v1/settings/global');
    return settings.onboarding_pub_key ?? '';
  } catch {
    return '';
  }
}

/** Wo der Agent-Listener erreichbar ist: eigener Port (0 = keiner) und, falls eingestellt, seine öffentliche Adresse. */
export interface AgentListener {
  port: number;
  url: string;
  /** Die Adresse hat keinen Port, LCM lauscht aber nicht auf 443: Ohne Reverse Proxy davor läuft der Befehl ins Leere. */
  needsProxy?: boolean;
  /** Der Agent ist in den Einstellungen ausgeschaltet; ein neuer Agent käme nicht an. Unbekannt gilt als an. */
  enabled?: boolean;
}

interface SystemInfo {
  agent_port?: number;
  agent_url?: string;
  agent_url_needs_proxy?: boolean;
  agent_enabled?: boolean;
}

/** Schalter „Agent“: aus den Systeminformationen, sonst aus den globalen Einstellungen (nur mit Recht darauf lesbar). */
async function agentEnabled(info: SystemInfo): Promise<boolean> {
  if (typeof info.agent_enabled === 'boolean') return info.agent_enabled;
  try {
    const settings = await api.get<{ agent_enabled?: boolean }>('/api/v1/settings/global');
    return settings.agent_enabled !== false;
  } catch {
    return true;
  }
}

export async function agentListener(): Promise<AgentListener> {
  try {
    const info = await api.get<SystemInfo>('/api/v1/system/info');
    return { port: info.agent_port ?? 0, url: info.agent_url ?? '', needsProxy: info.agent_url_needs_proxy ?? false, enabled: await agentEnabled(info) };
  } catch {
    return { port: 0, url: '' };
  }
}
