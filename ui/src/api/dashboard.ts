// Antwort von `GET /api/v1/dashboard`: alles, was die Startseite zeigt, in einem Aufruf.

import type { StatusLevel } from '../components/shared';

export const DASHBOARD_URL = '/api/v1/dashboard';

export interface DashboardTiles {
  excellent: number;
  green: number;
  yellow: number;
  red: number;
  /** LCM kann den Server nicht mehr pflegen; zählt zusätzlich zur Ampelstufe. */
  incapable: number;
  total: number;
}

export interface Insight {
  severity: 'info' | 'warning' | 'critical' | string;
  /** Deutscher Klartext für alles ohne Katalog. */
  message: string;
  key?: string;
  params?: Record<string, string>;
}

export interface DashboardAttention {
  server_id: number;
  name: string;
  level: 'red' | 'yellow';
  /** Leer, wenn die Bewertung keinen Befund liefert. */
  insight: Insight | null;
}

export interface DashboardJob {
  id: string;
  server_id: number | null;
  server_name: string;
  kind: string;
  name: string;
  status: string;
  started_at: string | null;
  finished_at: string | null;
  duration_seconds: number | null;
}

export interface DashboardRule {
  id: number;
  name: string;
  kind: string;
}

export interface DashboardRun {
  schedule_id: number;
  schedule_name: string;
  group_id: number;
  group_name: string;
  next_run: string;
  rules: DashboardRule[];
}

export interface DashboardAlert {
  id: string;
  created_at: string;
  rule_name: string;
  server_id: number | null;
  server_name: string;
  group_name: string;
  type: string;
  severity: string;
  code: string;
  description: string;
}

export interface Dashboard {
  generated_at: string;
  tiles: DashboardTiles;
  attention: DashboardAttention[];
  recent_jobs: DashboardJob[];
  upcoming: DashboardRun[];
  recent_alerts: DashboardAlert[];
}

/** Ampelstufe eines Servers im Dashboard (`red`/`yellow`) als Stufe der Oberfläche. */
export function attentionLevel(level: DashboardAttention['level']): StatusLevel {
  return level === 'red' ? 'crit' : 'warn';
}

/** Ampelstufe eines Alarms nach Schwere. */
export function alertLevel(severity: string): StatusLevel {
  return severity === 'critical' ? 'crit' : 'warn';
}
