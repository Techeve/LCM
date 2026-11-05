import { describe, expect, it } from 'vitest';
import { changedKeys, choice, draftOf, errorParams, fieldForDetail, fieldOfParams, flag, int, isDirty, mergeDraft, patchBody, secret, text, validateDraft } from './form-state';

const fields = [text('default_ssh_user', 64), int('default_ssh_port', 1, 65535), flag('mail_enabled'), secret('mail_password', 1024)];
const settings = { default_ssh_user: 'root', default_ssh_port: 22, mail_enabled: false, mail_password: 'ignored' };

describe('Entwurf', () => {
  it('übernimmt Werte, Zahlen als Text und lässt Geheimnisse leer', () => {
    expect(draftOf(settings, fields)).toEqual({ default_ssh_user: 'root', default_ssh_port: '22', mail_enabled: false, mail_password: '' });
  });

  it('kennt fehlende Werte als leer', () => {
    expect(draftOf({}, fields)).toEqual({ default_ssh_user: '', default_ssh_port: '', mail_enabled: false, mail_password: '' });
  });

  it('erkennt Änderungen, auch am Geheimnis', () => {
    const base = draftOf(settings, fields);
    expect(isDirty({ ...base }, base, fields)).toBe(false);
    expect(changedKeys({ ...base, mail_enabled: true, mail_password: 'x' }, base, fields)).toEqual(['mail_enabled', 'mail_password']);
  });
});

describe('Prüfung', () => {
  it('verlangt ganze Zahlen im Bereich', () => {
    const base = draftOf(settings, fields);
    expect(validateDraft(base, fields)).toEqual({});
    expect(validateDraft({ ...base, default_ssh_port: 'abc' }, fields)).toEqual({ default_ssh_port: 'settings.form.errors.number' });
    expect(validateDraft({ ...base, default_ssh_port: '2.5' }, fields)).toEqual({ default_ssh_port: 'settings.form.errors.number' });
    expect(validateDraft({ ...base, default_ssh_port: '70000' }, fields)).toEqual({ default_ssh_port: 'settings.form.errors.range' });
    expect(validateDraft({ ...base, default_ssh_port: '0' }, fields)).toEqual({ default_ssh_port: 'settings.form.errors.range' });
  });
});

describe('Prüfung der Textfelder', () => {
  const check = (spec: ReturnType<typeof text>, value: string) => validateDraft({ [spec.key]: value }, [spec])[spec.key];

  it('zählt Zeichen, nicht Bytes, und lehnt Steuerzeichen ab', () => {
    expect(check(text('a', 4), 'äöüß')).toBeUndefined();
    expect(check(text('a', 4), 'äöüß!')).toBe('check.tooLong');
    expect(check(text('a', 10), 'a\nb')).toBe('check.oneLine');
    expect(check(text('a', 10, 'text'), 'a\nb')).toBeUndefined();
    expect(check(text('a', 10, 'text'), 'a\u0000b')).toBe('check.controlChars');
  });

  it('prüft Hosts, E-Mail-Adressen und Adresslisten', () => {
    expect(check(text('h', 253, 'host'), 'smtp.example.com')).toBeUndefined();
    expect(check(text('h', 253, 'host'), '[2001:db8::1]')).toBeUndefined();
    expect(check(text('h', 253, 'host'), 'smtp example')).toBe('check.host');
    expect(check(text('h', 253, 'host'), '')).toBeUndefined();
    expect(check(text('m', 254, 'email'), 'lcm@example.com')).toBeUndefined();
    expect(check(text('m', 254, 'email'), 'LCM <lcm@example.com>')).toBe('check.email');
    const emails = text('r', 4096, 'emails');
    expect(check(emails, 'a@example.com, b@example.com\nc@example.com;d@example.com')).toBeUndefined();
    expect(check(emails, 'a@example.com, kaputt')).toBe('check.emails');
    expect(errorParams(emails, 'a@example.com, kaputt, x y').list).toBe('kaputt, x y');
  });

  it('prüft Adressen, Zeitzonen und Domains', () => {
    expect(check(text('u', 4096, 'url'), 'https://lcm.example.org')).toBeUndefined();
    expect(check(text('u', 4096, 'url'), 'lcm.example.org')).toBe('check.url');
    expect(check(text('z', 64, 'timezone'), 'Europe/Berlin')).toBeUndefined();
    expect(check(text('z', 64, 'timezone'), 'Europe/Berlin; id')).toBe('check.timezone');
    expect(check(text('d', 4096, 'domains'), 'intranet.example\ngit.example.org')).toBeUndefined();
    expect(check(text('d', 4096, 'domains'), 'ok.example\nx/y')).toBe('check.domains');
  });

  it('meldet die Grenze eines Textfelds als Platzhalter', () => {
    expect(errorParams(text('a', 64), 'x').max).toBe(64);
    expect(errorParams(int('p', 1, 65535), '0')).toMatchObject({ min: 1, max: 65535 });
  });
});

describe('Körper der Anfrage', () => {
  it('sendet nur Geändertes, Zahlen als Zahl und Text getrimmt', () => {
    const base = draftOf(settings, fields);
    const draft = { ...base, default_ssh_user: ' deploy ', default_ssh_port: '2222' };
    expect(patchBody(draft, base, fields)).toEqual({ default_ssh_user: 'deploy', default_ssh_port: 2222 });
  });

  it('sendet ein neu getipptes Geheimnis unverändert', () => {
    const base = draftOf(settings, fields);
    expect(patchBody({ ...base, mail_password: ' a b ' }, base, fields)).toEqual({ mail_password: ' a b ' });
  });

  it('sendet nichts, wenn nichts geändert ist', () => {
    const base = draftOf(settings, fields);
    expect(patchBody({ ...base }, base, fields)).toEqual({});
  });
});

describe('Fehler zuordnen', () => {
  const keys = ['session_ttl_minutes', 'job_idle_timeout_minutes', 'job_idle_timeout_slow_minutes'];

  it('findet das Feld im Fehlertext, das längste Kürzel zuerst', () => {
    expect(fieldForDetail('session_ttl_minutes muss zwischen 0 und 43200 liegen (war 99999)', keys)).toBe('session_ttl_minutes');
    expect(fieldForDetail('job_idle_timeout_slow_minutes muss zwischen 0 und 1440 liegen', keys)).toBe('job_idle_timeout_slow_minutes');
  });

  it('lässt allgemeine Fehler allgemein', () => {
    expect(fieldForDetail('ungültige öffentliche basis-adresse', keys)).toBeUndefined();
  });

  it('liest das Feld aus den Parametern eines Prüffehlers', () => {
    expect(fieldOfParams({ field: 'email' }, ['email', 'username'])).toBe('email');
    expect(fieldOfParams({ field: 'other' }, ['email'])).toBeUndefined();
    expect(fieldOfParams({}, ['email'])).toBeUndefined();
  });
});

describe('Neue Serverwerte', () => {
  it('übernimmt Unberührtes und behält Eingaben', () => {
    const oldBase = draftOf(settings, fields);
    const newBase = draftOf({ ...settings, default_ssh_user: 'ops', default_ssh_port: 2200 }, fields);
    const draft = { ...oldBase, default_ssh_port: '2022' };
    expect(mergeDraft(draft, oldBase, newBase, fields)).toMatchObject({ default_ssh_user: 'ops', default_ssh_port: '2022' });
  });
});

describe('Auswahl und Absender', () => {
  const mail = [choice('mail_security', (settings) => (settings.mail_use_tls === false ? 'none' : 'starttls')), text('mail_from', 390, 'sender')];

  it('nimmt den Wert des Servers, sonst den abgeleiteten', () => {
    expect(draftOf({ mail_security: 'tls' }, mail).mail_security).toBe('tls');
    expect(draftOf({ mail_use_tls: false }, mail).mail_security).toBe('none');
    expect(draftOf({}, mail).mail_security).toBe('starttls');
  });

  it('sendet eine geänderte Auswahl als Text', () => {
    const base = draftOf({ mail_security: 'starttls' }, mail);
    expect(patchBody({ ...base, mail_security: 'tls' }, base, mail)).toEqual({ mail_security: 'tls' });
  });

  it('prüft den Absender mit und ohne Anzeigenamen', () => {
    const check = (from: string) => validateDraft({ mail_security: 'tls', mail_from: from }, mail);
    expect(check('lcm@example.com')).toEqual({});
    expect(check('LCM <lcm@example.com>')).toEqual({});
    expect(check('LCM')).toEqual({ mail_from: 'check.sender' });
    expect(check(`${'n'.repeat(65)} <lcm@example.com>`)).toEqual({ mail_from: 'check.senderName' });
  });
});
