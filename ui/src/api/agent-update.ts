// Agent-Updates: der Plan eines Servers (von → nach), das Starten des Update-Jobs und die Agent-Update-Regel einer
// Gruppe. Die Zielversion bestimmt das Backend; die Oberfläche zeigt sie nur an.

import { api } from '../lib/api';
import { store } from '../lib/store';
import { GROUPS_URL, groupUrl } from './groups';
import { SERVERS_URL, type StartedJob } from './servers';

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

/** Was ein Update täte: `update` (neuere Version da), `current` (steht auf der besten Wahl), `newer` (neuer als LCM), `none` (keine passende Version), `unsupported` (keine unterstützte Paketverwaltung oder kein Paket). */
export type AgentUpdateAction = 'update' | 'current' | 'newer' | 'none' | 'unsupported';

const ACTIONS: readonly AgentUpdateAction[] = ['update', 'current', 'newer', 'none', 'unsupported'];

export interface AgentUpdatePlan {
  action: AgentUpdateAction;
  /** Kennung des Grundes bei `none` und `unsupported`: no_manager, not_packaged, no_version, unknown_lcm_version. */
  reason: string;
  installed: string;
  target: string;
  server_version: string;
  available: string[];
  warnings: string[];
}

export function normaliseAgentUpdatePlan(raw: unknown): AgentUpdatePlan {
  const r = isRecord(raw) ? raw : {};
  const strings = (value: unknown) => (Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []);
  return {
    action: ACTIONS.find((known) => known === r.action) ?? 'none',
    reason: text(r.reason),
    installed: text(r.installed),
    target: text(r.target),
    server_version: text(r.server_version),
    available: strings(r.available),
    warnings: strings(r.warnings),
  };
}

/** Der Schlüssel des Hinweises zum Plan, den ein Anwender liest. */
export function agentPlanNoteKey(plan: AgentUpdatePlan): string {
  switch (plan.action) {
    case 'update':
      return 'agentUpdate.plan.update';
    case 'current':
      return 'agentUpdate.plan.current';
    case 'newer':
      return 'agentUpdate.plan.newer';
    case 'unsupported':
      return plan.reason === 'no_manager' ? 'agentUpdate.plan.noManager' : 'agentUpdate.plan.notPackaged';
    default:
      return plan.reason === 'unknown_lcm_version' ? 'agentUpdate.plan.unknownLcm' : 'agentUpdate.plan.noVersion';
  }
}

const planUrl = (serverId: number) => `${SERVERS_URL}/${serverId}/agent-update`;

export async function fetchAgentUpdatePlan(serverId: number): Promise<AgentUpdatePlan> {
  return normaliseAgentUpdatePlan(await api.get<unknown>(planUrl(serverId)));
}

export async function startAgentUpdate(serverId: number): Promise<StartedJob> {
  const raw = await api.post<unknown>(planUrl(serverId), {});
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

// ---- Regel der Gruppe ----

export type AgentUpdateMode = 'off' | 'auto';

/** Die Agent-Update-Regel einer Gruppe samt Stand. */
export interface AgentUpdatePolicy {
  group_id: number;
  mode: AgentUpdateMode;
  staged: boolean;
  cron_expr: string;
  window_minutes: number;
  last_run_at: string | null;
  last_result: string;
  proven_version: string;
  next_run: string;
  server_version: string;
  agent_servers: number;
  outdated: number;
  running: boolean;
  blocked: boolean;
}

export const DEFAULT_AGENT_UPDATE_CRON = '0 3 * * *';
export const DEFAULT_AGENT_UPDATE_WINDOW = 120;
export const MAX_AGENT_UPDATE_WINDOW = 1440;

export const agentUpdateUrl = (groupId: number) => `${groupUrl(groupId)}/agent-updates`;

export function normaliseAgentUpdatePolicy(raw: unknown): AgentUpdatePolicy {
  const r = isRecord(raw) ? raw : {};
  return {
    group_id: count(r.group_id),
    mode: r.mode === 'auto' ? 'auto' : 'off',
    staged: r.staged !== false,
    cron_expr: text(r.cron_expr) || DEFAULT_AGENT_UPDATE_CRON,
    window_minutes: count(r.window_minutes),
    last_run_at: typeof r.last_run_at === 'string' ? r.last_run_at : null,
    last_result: text(r.last_result),
    proven_version: text(r.proven_version),
    next_run: text(r.next_run),
    server_version: text(r.server_version),
    agent_servers: count(r.agent_servers),
    outdated: count(r.outdated),
    running: r.running === true,
    blocked: r.blocked === true,
  };
}

export interface AgentUpdateForm {
  auto: boolean;
  staged: boolean;
  cron_expr: string;
  window_minutes: string;
}

export function agentUpdateFormOf(policy: AgentUpdatePolicy): AgentUpdateForm {
  return { auto: policy.mode === 'auto', staged: policy.staged, cron_expr: policy.cron_expr, window_minutes: String(policy.window_minutes) };
}

/** Fehlerschlüssel je Feld; leer = gültig. */
export function validateAgentUpdateForm(form: AgentUpdateForm): { window?: string } {
  const minutes = Number(form.window_minutes);
  if (!/^\d+$/.test(form.window_minutes.trim()) || minutes > MAX_AGENT_UPDATE_WINDOW) return { window: 'agentUpdate.policy.windowRange' };
  return {};
}

export function agentUpdateBody(form: AgentUpdateForm): { mode: AgentUpdateMode; staged: boolean; cron_expr: string; window_minutes: number } {
  return {
    mode: form.auto ? 'auto' : 'off',
    staged: form.staged,
    cron_expr: form.cron_expr.trim().split(/\s+/).join(' '),
    window_minutes: Number(form.window_minutes.trim()),
  };
}

function changed(groupId: number): void {
  store.invalidate(agentUpdateUrl(groupId));
  store.invalidate(GROUPS_URL, { withChildren: false });
}

export const agentUpdateApi = {
  save: async (groupId: number, form: AgentUpdateForm) => {
    const policy = normaliseAgentUpdatePolicy(await api.put<unknown>(agentUpdateUrl(groupId), agentUpdateBody(form)));
    changed(groupId);
    return policy;
  },
  run: async (groupId: number) => {
    await api.post(`${agentUpdateUrl(groupId)}/run`, {});
    changed(groupId);
  },
  resume: async (groupId: number) => {
    const policy = normaliseAgentUpdatePolicy(await api.post<unknown>(`${agentUpdateUrl(groupId)}/resume`, {}));
    changed(groupId);
    return policy;
  },
};
