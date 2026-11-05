import { describe, expect, it } from 'vitest';
import { isAddressOrNet } from '../../../lib/validate';
import { allowlistBody, entryPreview, invalidEntries, lapiServers, validateAllowlistForm, validateLapiForm } from './models';

describe('Allowlist-Einträge', () => {
  it('erkennt Adressen und Netze', () => {
    for (const ok of ['10.0.0.5', '192.168.1.0/24', '0.0.0.0/0', '2001:db8::/32', '::1', 'fe80::1', '::ffff:10.0.0.1', '2001:db8:0:0:0:0:0:1']) {
      expect(isAddressOrNet(ok), ok).toBe(true);
    }
    for (const bad of ['10.0.0.256', '10.0.0', '10.0.0.1/33', '2001:db8::/129', '2001::db8::1', 'abc', '10.0.0.1/24/1', '1:2:3:4:5:6:7:8:9']) {
      expect(isAddressOrNet(bad), bad).toBe(false);
    }
  });

  it('trennt an Zeilen, Kommas und Leerzeichen', () => {
    expect(invalidEntries('10.0.0.1, 10.0.0.2\nkaputt  2001:db8::1')).toEqual(['kaputt']);
    expect(allowlistBody({ name: ' Büro ', description: '', entries: '10.0.0.1, 10.0.0.2' }).entries).toBe('10.0.0.1\n10.0.0.2');
    expect(entryPreview('a\nb\nc\nd\ne')).toEqual({ text: 'a, b, c', more: 2 });
  });

  it('prüft den Namen', () => {
    expect(validateAllowlistForm({ name: 'Büro "A"', description: '', entries: '' }).name).toBe('settings.allowlists.form.nameInvalid');
    expect(validateAllowlistForm({ name: 'Büro-Netze', description: '', entries: '10.0.0.1' })).toEqual({});
  });
});

describe('CrowdSec-LAPI', () => {
  it('verlangt das Passwort nur beim Anlegen', () => {
    const form = { name: 'LAPI', url: 'http://10.0.0.1:8080', login: 'lcm-managed', password: '' };
    expect(validateLapiForm(form, true).password).toBeDefined();
    expect(validateLapiForm(form, false)).toEqual({});
    expect(validateLapiForm({ ...form, login: 'bad login', url: 'lapi' }, false)).toMatchObject({ login: expect.any(String), url: expect.any(String) });
  });

  it('findet die Server einer LAPI über die Adresse', () => {
    const server = (url: string, installed = true) => ({ id: 1, name: 'a', host: 'a', apt_proxy_active: false, apt_proxy_url: '', crowdsec_installed: installed, crowdsec_active: true, crowdsec_lapi_mode: 'remote', crowdsec_lapi_url: url });
    const lapis = [{ id: 1, name: 'A', url: 'http://10.0.0.1:8080', login: 'x' }];
    expect(lapiServers([server('http://10.0.0.1:8080/'), server('http://10.0.0.1:8080', false), server('http://other')], lapis)).toHaveLength(1);
  });
});
