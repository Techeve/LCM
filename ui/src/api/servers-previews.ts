// Befehlsvorschau der Konfigurations-Aktionen (Firewall, SSH, Sicherheitswerkzeuge, Paketquellen, Docker, Konten …):
// `GET /servers/:id/actions/:action/preview`. Das Backend baut die Befehle mit demselben Code, der sie später ausführt.
// Mehrstufige Läufe führen bedingte Schritte mit `when` (nur falls nötig, nur beim Rollback).

import { api } from '../lib/api';
import { normalisePreview, SERVERS_URL, type ActionPreviewData, type PreviewCommand } from './servers';
import type { FirewallRulePayload, HardenTarget, ManageInput, SecurityToolInstall, SshSources } from './servers-security';

export type ConfigPreviewAction =
  | 'firewall-apply'
  | 'firewall-disable'
  | 'ssh-port'
  | 'ssh-root-login-lock'
  | 'ssh-root-login-unlock'
  | 'harden-ssh'
  | 'unharden-ssh'
  | 'security-tool'
  | 'security-tool-manage'
  | 'ssh-2fa-enable'
  | 'ssh-2fa-disable'
  | 'repos-secure'
  | 'repos-revert-https'
  | 'repos-add'
  | 'apt-cache-connect'
  | 'apt-cache-disconnect'
  | 'dns'
  | 'timezone'
  | 'ntp'
  | 'pins-apply'
  | 'deep-scan-tools'
  | 'acl-install'
  | 'path-harden'
  | 'path-restore'
  | 'lcm-host-trivy'
  | 'lcm-host-sandbox'
  | 'lcm-host-apt-cacher'
  | 'lcm-host-crowdsec-lapi'
  | 'docker-compose-update'
  | 'docker-pull'
  | 'docker-pull-all'
  | 'docker-image-delete'
  | 'docker-prune'
  | 'user-lock'
  | 'user-unlock'
  | 'user-remove';

/** Die Argumente der Aktion, so wie sie auch im Body des eigentlichen Aufrufs stehen. */
export interface ConfigPreviewParams {
  port?: number;
  rules?: FirewallRulePayload[];
  sshSources?: SshSources | null;
  tool?: SecurityToolInstall | (Pick<ManageInput, 'tool' | 'action'> & Partial<ManageInput>);
  uris?: string[];
  key?: string;
  cacheId?: number;
  servers?: string[];
  timezone?: string;
  targets?: HardenTarget[];
  pathId?: number;
  bouncer?: boolean;
  project?: string;
  service?: string;
  image?: string;
  username?: string;
}

export type PreviewWhen = '' | 'if_needed' | 'on_failure';

export interface ConfigPreviewCommand extends PreviewCommand {
  when: PreviewWhen;
}

export interface ConfigPreviewData extends ActionPreviewData {
  commands: ConfigPreviewCommand[];
}

const list = (values: Array<string | number> | undefined) => (values && values.length > 0 ? values.join(',') : '');

function setIf(query: URLSearchParams, key: string, value: string | number | undefined): void {
  if (value !== undefined && value !== '') query.set(key, String(value));
}

function toolQuery(query: URLSearchParams, tool: NonNullable<ConfigPreviewParams['tool']>): void {
  query.set('tool', tool.tool);
  setIf(query, 'allowlist_ips', list(tool.allowlist_ips));
  setIf(query, 'allowlist_ids', list(tool.allowlist_ids));
  if ('action' in tool) {
    query.set('op', tool.action);
    setIf(query, 'ip', tool.unban_ip);
    return;
  }
  if (tool.bouncer !== undefined) query.set('bouncer', String(tool.bouncer));
  setIf(query, 'collections', list(tool.collections));
  setIf(query, 'lapi_mode', tool.lapi_mode);
  setIf(query, 'lapi_id', tool.lapi_id);
}

export function configPreviewUrl(serverId: number, action: ConfigPreviewAction, params: ConfigPreviewParams = {}): string {
  const query = new URLSearchParams();
  setIf(query, 'port', params.port);
  if (params.rules) query.set('rules', JSON.stringify(params.rules));
  if (params.sshSources) query.set('ssh_sources', JSON.stringify(params.sshSources));
  if (params.tool) toolQuery(query, params.tool);
  setIf(query, 'uri', list(params.uris));
  setIf(query, 'key', params.key);
  setIf(query, 'cache_id', params.cacheId);
  setIf(query, 'servers', list(params.servers));
  setIf(query, 'timezone', params.timezone);
  if (params.targets) query.set('targets', JSON.stringify(params.targets));
  setIf(query, 'path_id', params.pathId);
  if (params.bouncer !== undefined) query.set('bouncer', String(params.bouncer));
  setIf(query, 'project', params.project);
  setIf(query, 'service', params.service);
  setIf(query, 'image', params.image);
  setIf(query, 'username', params.username);
  const suffix = query.toString();
  return `${SERVERS_URL}/${serverId}/actions/${action}/preview${suffix ? `?${suffix}` : ''}`;
}

function whenOf(value: unknown): PreviewWhen {
  return value === 'if_needed' || value === 'on_failure' ? value : '';
}

/** Wie `normalisePreview`, dazu die Bedingung je Schritt. */
export function normaliseConfigPreview(raw: unknown): ConfigPreviewData {
  const data = normalisePreview(raw);
  const rawCommands = typeof raw === 'object' && raw !== null && Array.isArray((raw as { commands?: unknown }).commands) ? ((raw as { commands: unknown[] }).commands) : [];
  const whens = rawCommands.filter((c): c is Record<string, unknown> => typeof c === 'object' && c !== null).map((c) => whenOf(c.when));
  return { ...data, commands: data.commands.map((command, i) => ({ ...command, when: whens[i] ?? '' })) };
}

export async function fetchConfigPreview(serverId: number, action: ConfigPreviewAction, params: ConfigPreviewParams = {}): Promise<ConfigPreviewData> {
  return normaliseConfigPreview(await api.get(configPreviewUrl(serverId, action, params)));
}
