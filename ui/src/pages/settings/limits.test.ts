import { describe, expect, it } from 'vitest';
import { emptyUserForm, validateNewPassword, validateUserForm } from './access/users-model';
import { validateActionForm } from './automation/action-model';
import { ALERT_RANGES, badRecipients, emptyChannelForm, emptyRuleForm, validateChannelForm, validateRuleForm } from './messages/models';
import { APP_LIMITS, emptyAppForm, validateAppForm } from './packages/app-model';
import { validateCacheForm } from './packages/apt-cache-model';
import { emptyRepoForm, validateRepoForm } from './packages/repo-model';
import { validateAllowlistForm, validateLapiForm } from './protection/models';

describe('Einstellungen: Benutzer', () => {
  it('prüft E-Mail, Namen und die Höchstlänge des Passworts', () => {
    const form = { ...emptyUserForm(), username: 'tony', password: 'ein-langes-Passwort-1', confirm: 'ein-langes-Passwort-1' };
    expect(validateUserForm(form, 'create')).toEqual({});
    expect(validateUserForm({ ...form, email: 'Tony <t@x.de>' }, 'create').email).toBe('settings.users.form.emailInvalid');
    expect(validateUserForm({ ...form, firstName: 'a'.repeat(65) }, 'create').firstName).toBe('check.tooLong');
    const long = 'aB1-'.repeat(51);
    expect(validateUserForm({ ...form, password: long, confirm: long }, 'create').password).toBe('check.tooLong');
    expect(validateNewPassword(long, long).password).toBe('check.tooLong');
  });
});

describe('Einstellungen: Meldungen', () => {
  const channel = { ...emptyChannelForm(), name: 'Mail', host: 'smtp.example.com', from: 'lcm@example.com', recipients: 'a@example.com\nb@example.com' };

  it('prüft Host, Port, Absender und Empfänger eines E-Mail-Kanals', () => {
    expect(validateChannelForm(channel, true)).toEqual({});
    expect(validateChannelForm({ ...channel, host: 'smtp example' }, true).host).toBe('check.host');
    expect(validateChannelForm({ ...channel, port: '70000' }, true).port).toBeDefined();
    expect(validateChannelForm({ ...channel, from: 'LCM <lcm@example.com>' }, true).from).toBeUndefined();
    expect(validateChannelForm({ ...channel, from: 'LCM <lcm@example.com' }, true).from).toBeDefined();
    expect(validateChannelForm({ ...channel, recipients: 'a@example.com, kaputt' }, true).recipients).toBe('check.emails');
    expect(badRecipients('a@example.com, kaputt, x y')).toEqual(['kaputt', 'x y']);
    const many = Array.from({ length: 51 }, (_, i) => `u${i}@example.com`).join('\n');
    expect(validateChannelForm({ ...channel, recipients: many }, true).recipients).toBe('check.tooMany');
    expect(validateChannelForm({ ...channel, name: 'n'.repeat(65) }, true).name).toBe('check.tooLong');
  });

  it('verlangt für Webhooks eine https-Adresse', () => {
    const hook = { ...emptyChannelForm(), type: 'webhook', name: 'Teams', secret: 'https://example.com/hook' };
    expect(validateChannelForm(hook, true)).toEqual({});
    expect(validateChannelForm({ ...hook, secret: 'https://exa mple.com' }, true).secret).toBe('check.url');
  });

  it('hält die Bereiche der Alarmregeln wie das Backend ein', () => {
    const rule = { ...emptyRuleForm(), name: 'Platte' };
    expect(validateRuleForm(rule)).toEqual({});
    expect(validateRuleForm({ ...rule, threshold_percent: '101' }).threshold_percent).toBeDefined();
    expect(validateRuleForm({ ...rule, cooldown_minutes: String(ALERT_RANGES.cooldown_minutes.max + 1) }).cooldown_minutes).toBeDefined();
    expect(validateRuleForm({ ...rule, cooldown_minutes: '-1' }).cooldown_minutes).toBeDefined();
    expect(validateRuleForm({ ...rule, type: 'storage_forecast', forecast_days: '3651' }).forecast_days).toBeDefined();
    expect(validateRuleForm({ ...rule, name: 'a\nb' }).name).toBe('check.oneLine');
  });
});

describe('Einstellungen: Pakete und Schutz', () => {
  it('begrenzt die Felder einer Anwendung wie das Backend', () => {
    const app = { ...emptyAppForm(), slug: 'adguard', name: 'AdGuard', markers: 'path /opt/x' };
    expect(validateAppForm(app)).toEqual({});
    expect(validateAppForm({ ...app, version_command: 'x'.repeat(APP_LIMITS.version_command + 1) }).version_command).toBe('check.tooLong');
    expect(validateAppForm({ ...app, version_command: '{path} --version\nid' }).version_command).toBe('check.oneLine');
    expect(validateAppForm({ ...app, description: 'a\nb' }).description).toBeUndefined();
    expect(validateAppForm({ ...app, name: 'n'.repeat(65) }).name).toBe('check.tooLong');
  });

  it('begrenzt Paketquellen', () => {
    const repo = { ...emptyRepoForm(), name: 'Docker', key: 'docker', line: 'deb https://example.com stable main' };
    expect(validateRepoForm(repo)).toEqual({});
    expect(validateRepoForm({ ...repo, key: 'k'.repeat(65) }).key).toBe('settings.repositories.form.keyInvalid');
    expect(validateRepoForm({ ...repo, description: 'd'.repeat(1001) }).description).toBe('check.tooLong');
    expect(validateRepoForm({ ...repo, line: `deb ${'x'.repeat(4100)}` }).line).toBe('check.tooLong');
  });

  it('prüft APT-Cache, Allowlist, LAPI und Eigene Aktion', () => {
    expect(validateCacheForm({ name: 'Berlin', url: 'http://10.0.0.5:3142' })).toEqual({});
    expect(validateCacheForm({ name: 'a"b', url: 'http://10.0.0.5:3142' }).name).toBe('settings.aptCache.form.nameInvalid');
    expect(validateCacheForm({ name: 'n'.repeat(65), url: 'http://x' }).name).toBe('check.tooLong');
    expect(validateAllowlistForm({ name: 'Büro', description: 'd'.repeat(1001), entries: '10.0.0.1' }).description).toBe('check.tooLong');
    expect(validateAllowlistForm({ name: 'Büro', description: '', entries: 'x'.repeat(32769) }).entries).toBe('check.tooLong');
    expect(validateLapiForm({ name: 'l', url: 'http://lapi:8080', login: 'lcm', password: 'p'.repeat(1025) }, true).password).toBe('check.tooLong');
    expect(validateLapiForm({ name: 'l', url: 'http://lapi:8080', login: 'l'.repeat(129), password: 'p' }, true).login).toBeDefined();
    const action = { name: 'Wartung', description: '', commands: 'echo hi' };
    expect(validateActionForm(action)).toEqual({});
    expect(validateActionForm({ ...action, commands: `echo ${'x'.repeat(32768)}` }).commands).toBe('check.tooLong');
    expect(validateActionForm({ ...action, description: 'd'.repeat(1001) }).description).toBe('check.tooLong');
  });
});
