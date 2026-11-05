import { describe, expect, it } from 'vitest';
import { readApiKeys } from '../../../api/settings';
import { emptyKeyForm, expiryDays, keyState, keysFor, validateKeyForm } from './keys-model';

const keys = readApiKeys([
  { id: 1, name: 'ci', scope: 'readwrite' },
  { id: 2, name: 'agent', scope: 'mcp' },
  { id: 3, name: 'alt', scope: 'read', revoked: true },
  { id: 4, name: 'abgelaufen', scope: 'read', expires_at: '2020-01-01T00:00:00Z' },
]);

describe('Schlüsselformular', () => {
  it('verlangt einen Namen und positive ganze Tage', () => {
    const form = { ...emptyKeyForm('read'), name: 'ci' };
    expect(validateKeyForm(form)).toEqual({});
    expect(validateKeyForm({ ...form, name: '  ' }).name).toBe('settings.apikeys.form.nameRequired');
    expect(validateKeyForm({ ...form, name: 'x'.repeat(65) }).name).toBe('settings.apikeys.form.nameTooLong');
    for (const days of ['0', '-3', '1.5', 'x', '3651']) expect(validateKeyForm({ ...form, days }).days).toBe('settings.apikeys.form.daysInvalid');
    expect(validateKeyForm({ ...form, days: '30' })).toEqual({});
    expect(validateKeyForm({ ...form, days: '3650' })).toEqual({});
  });

  it('zählt Zeichen und lehnt Zeilenumbrüche im Namen ab', () => {
    const form = { ...emptyKeyForm('read'), name: '😀'.repeat(64) };
    expect(validateKeyForm(form)).toEqual({});
    expect(validateKeyForm({ ...form, name: 'a\nb' }).name).toBe('check.oneLine');
  });

  it('leere Tage heißen ohne Ablauf', () => {
    expect(expiryDays(emptyKeyForm('read'))).toBeNull();
    expect(expiryDays({ ...emptyKeyForm('read'), days: ' 30 ' })).toBe(30);
  });
});

describe('Schlüsselstatus', () => {
  it('unterscheidet aktiv, widerrufen und abgelaufen', () => {
    expect(keys.map((key) => keyState(key))).toEqual(['active', 'active', 'revoked', 'expired']);
  });

  it('trennt REST- und MCP-Schlüssel', () => {
    expect(keysFor(keys, 'rest').map((key) => key.name)).toEqual(['ci', 'alt', 'abgelaufen']);
    expect(keysFor(keys, 'mcp').map((key) => key.name)).toEqual(['agent']);
  });
});
