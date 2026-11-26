import { describe, expect, it } from 'vitest';
import {
  agentPlanNoteKey,
  agentUpdateBody,
  agentUpdateFormOf,
  normaliseAgentUpdatePlan,
  normaliseAgentUpdatePolicy,
  validateAgentUpdateForm,
} from './agent-update';
import { normaliseAgentSupport } from './servers';

describe('Plan des Agent-Updates', () => {
  it('liest von und nach aus der Antwort des Backends', () => {
    const plan = normaliseAgentUpdatePlan({ action: 'update', installed: '2.2.0', target: '2.3.0', server_version: '2.3.0', available: ['2.3.0', '2.2.0', 7], warnings: ['enterprise_pin'] });
    expect(plan).toMatchObject({ action: 'update', installed: '2.2.0', target: '2.3.0', available: ['2.3.0', '2.2.0'], warnings: ['enterprise_pin'] });
  });

  it('kennt nur bekannte Aktionen und fällt sonst auf „keine Version“', () => {
    expect(normaliseAgentUpdatePlan({ action: 'neu-erfunden' }).action).toBe('none');
    expect(normaliseAgentUpdatePlan(undefined).available).toEqual([]);
  });

  it('wählt den Hinweis nach Aktion und Grund', () => {
    const note = (action: string, reason = '') => agentPlanNoteKey(normaliseAgentUpdatePlan({ action, reason }));
    expect(note('update')).toBe('agentUpdate.plan.update');
    expect(note('current')).toBe('agentUpdate.plan.current');
    expect(note('newer')).toBe('agentUpdate.plan.newer');
    expect(note('unsupported', 'no_manager')).toBe('agentUpdate.plan.noManager');
    expect(note('unsupported', 'not_packaged')).toBe('agentUpdate.plan.notPackaged');
    expect(note('none', 'unknown_lcm_version')).toBe('agentUpdate.plan.unknownLcm');
    expect(note('none', 'no_version')).toBe('agentUpdate.plan.noVersion');
  });
});

describe('Agent im Bericht des Backends', () => {
  it('liest „hinter LCM“ und das Rollback', () => {
    const support = normaliseAgentSupport({ state: 'ok', behind: true, rolled_back: '2.4.0' });
    expect(support?.behind).toBe(true);
    expect(support?.rolled_back).toBe('2.4.0');
    const plain = normaliseAgentSupport({ state: 'ok' });
    expect(plain?.behind).toBe(false);
    expect(plain?.rolled_back).toBe('');
  });
});

describe('Regel der Gruppe', () => {
  it('ist ohne Angaben aus, gestaffelt und mit dem nächtlichen Fenster belegt', () => {
    const policy = normaliseAgentUpdatePolicy({});
    expect(policy).toMatchObject({ mode: 'off', staged: true, cron_expr: '0 3 * * *', blocked: false });
  });

  it('übernimmt Stand und Sperre des Backends', () => {
    const policy = normaliseAgentUpdatePolicy({ mode: 'auto', staged: false, cron_expr: '0 2 * * 1', window_minutes: 90, blocked: true, last_run_at: '2026-11-24T03:00:00Z', last_result: '3 aktualisiert', outdated: 2, agent_servers: 5 });
    expect(policy).toMatchObject({ mode: 'auto', staged: false, window_minutes: 90, blocked: true, outdated: 2, agent_servers: 5, last_result: '3 aktualisiert' });
  });

  it('macht aus dem Formular den Body und zurück', () => {
    const form = agentUpdateFormOf(normaliseAgentUpdatePolicy({ mode: 'auto', staged: true, cron_expr: '30  2 * * *', window_minutes: 60 }));
    expect(form).toEqual({ auto: true, staged: true, cron_expr: '30  2 * * *', window_minutes: '60' });
    expect(agentUpdateBody(form)).toEqual({ mode: 'auto', staged: true, cron_expr: '30 2 * * *', window_minutes: 60 });
    expect(agentUpdateBody({ ...form, auto: false }).mode).toBe('off');
  });

  it('prüft die Länge des Fensters', () => {
    const form = { auto: true, staged: true, cron_expr: '0 3 * * *', window_minutes: '60' };
    expect(validateAgentUpdateForm(form)).toEqual({});
    expect(validateAgentUpdateForm({ ...form, window_minutes: '0' })).toEqual({});
    expect(validateAgentUpdateForm({ ...form, window_minutes: '1440' })).toEqual({});
    expect(validateAgentUpdateForm({ ...form, window_minutes: '1441' }).window).toBeDefined();
    expect(validateAgentUpdateForm({ ...form, window_minutes: '-5' }).window).toBeDefined();
    expect(validateAgentUpdateForm({ ...form, window_minutes: '' }).window).toBeDefined();
    expect(validateAgentUpdateForm({ ...form, window_minutes: 'zwei' }).window).toBeDefined();
  });
});
