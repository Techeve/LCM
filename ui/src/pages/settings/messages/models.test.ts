import { describe, expect, it } from 'vitest';
import type { AlertRule } from '../../../api/settings-more';
import { appendLines, lineText, MAX_LINES, parseLogFilter } from './events-model';
import { channelBody, emptyChannelForm, emptyRuleForm, ruleBody, ruleFormOf, scopeText, silentRules, validateChannelForm, validateRuleForm } from './models';

const rule = (patch: Partial<AlertRule> = {}): AlertRule => ({
  id: 1,
  name: 'Platte',
  type: 'disk_capacity',
  enabled: true,
  group_ids: [],
  group_names: [],
  channel_id: null,
  severity: 'warning',
  threshold_percent: 0,
  forecast_days: 0,
  max_outdated: 0,
  min_severity: '',
  heartbeat_hours: 0,
  cooldown_minutes: 0,
  ...patch,
});

describe('Kanal', () => {
  it('prüft E-Mail-Felder', () => {
    const form = { ...emptyChannelForm(), name: 'Ops', host: 'smtp', from: 'lcm@example.com', recipients: 'a@example.com\nb@example.com' };
    expect(validateChannelForm(form, true)).toEqual({});
    expect(validateChannelForm({ ...form, recipients: 'kein-mail', port: '70000' }, true)).toMatchObject({ recipients: expect.any(String), port: expect.any(String) });
  });

  it('nimmt den Absender mit Anzeigenamen und prüft dessen Länge', () => {
    const form = { ...emptyChannelForm(), name: 'Ops', host: 'smtp', recipients: 'a@example.com' };
    expect(validateChannelForm({ ...form, from: 'LCM <lcm@example.com>' }, true).from).toBeUndefined();
    expect(validateChannelForm({ ...form, from: 'LCM' }, true).from).toBe('settings.notifications.form.fromInvalid');
    expect(validateChannelForm({ ...form, from: `${'n'.repeat(65)} <lcm@example.com>` }, true).from).toBe('check.senderName');
  });

  it('startet neue Kanäle mit STARTTLS auf Port 587 und schickt den Modus mit', () => {
    const form = emptyChannelForm();
    expect(form).toMatchObject({ security: 'starttls', port: '587' });
    const body = channelBody({ ...form, name: 'Ops', host: 'smtp', from: 'a@b.cd', recipients: 'x@y.zz', security: 'tls', port: '465' });
    expect(body.config).toMatchObject({ security: 'tls', port: 465 });
    expect(body.config).not.toHaveProperty('use_tls');
  });

  it('verlangt die Webhook-Adresse nur beim Anlegen und schickt leere Geheimnisse nicht mit', () => {
    const webhook = { ...emptyChannelForm(), name: 'Teams', type: 'webhook' };
    expect(validateChannelForm(webhook, true).secret).toBeDefined();
    expect(validateChannelForm(webhook, false)).toEqual({});
    expect(validateChannelForm({ ...webhook, secret: 'http://x' }, false).secret).toBe('settings.notifications.form.urlHttps');
    expect(channelBody(webhook)).toEqual({ name: 'Teams', type: 'webhook', enabled: true, config: { format: 'teams' } });
  });
});

describe('Alarmregel', () => {
  it('behält eine Sperrfrist von 0 (keine Sperre)', () => {
    expect(ruleFormOf(rule()).cooldown_minutes).toBe('0');
    expect(ruleBody(ruleFormOf(rule())).cooldown_minutes).toBe(0);
    expect(emptyRuleForm().cooldown_minutes).toBe('360');
  });

  it('prüft nur die Schwelle des gewählten Typs', () => {
    const form = { ...emptyRuleForm(), name: 'x', threshold_percent: '150' };
    expect(validateRuleForm(form).threshold_percent).toBeDefined();
    expect(validateRuleForm({ ...form, type: 'reboot_required' })).toEqual({});
  });

  it('lässt Gruppen bei Selbstbeobachtung weg', () => {
    const form = { ...emptyRuleForm(), name: 'x', type: 'backup_stale', group_ids: [3] };
    expect(ruleBody(form).group_ids).toEqual([]);
    expect(scopeText(rule({ type: 'backup_stale' }))).toBe('settings.alerts.scope.self');
    expect(scopeText(rule({ group_names: ['A', 'B'] }))).toBe('A, B');
  });

  it('zählt stille Regeln', () => {
    expect(silentRules([rule(), rule({ channel_id: 2 }), rule({ enabled: false })])).toBe(1);
  });
});

describe('Ereignisse', () => {
  it('liest den Filter aus der Adresse', () => {
    expect(parseLogFilter(new URLSearchParams('level=warn&q=queued&follow=1'))).toEqual({ level: 'warn', query: 'queued', follow: true });
    expect(parseLogFilter(new URLSearchParams('level=laut')).level).toBe('');
  });

  it('hält höchstens MAX_LINES Zeilen', () => {
    const line = { time: '', level: 'INFO', msg: 'x', raw: 'x' };
    const lines = appendLines(Array.from({ length: MAX_LINES }, () => line), [{ ...line, msg: 'neu' }]);
    expect(lines).toHaveLength(MAX_LINES);
    expect(lines.at(-1)?.msg).toBe('neu');
  });

  it('zeigt Meldung samt Feldern', () => {
    expect(lineText({ time: '', level: 'INFO', msg: 'request', raw: 'time=2026 level=INFO msg=request method=GET path=/x' })).toBe('request method=GET path=/x');
    expect(lineText({ time: '', level: 'INFO', msg: 'a b', raw: 'time=2026 level=INFO msg="a b" n=1' })).toBe('a b n=1');
    expect(lineText({ time: '', level: '', msg: '', raw: 'freier Text' })).toBe('freier Text');
  });
});
