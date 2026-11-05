import { describe, expect, it } from 'vitest';
import { normaliseApps, normaliseChannels, normaliseOverview } from '../../../api/settings-more';
import { ApiError } from '../../../lib/api';
import { appBody, emptyAppForm, markerProblem, sourceProblem, validateAppForm } from './app-model';
import { cacheNameFor, connectedCount, selectedCache, validateCacheForm } from './apt-cache-model';
import { modalQuery, fieldError, monitorState, parseModal } from './kit';
import { emptyRepoForm, validateRepoForm } from './repo-model';

describe('Fenster in der Adresse', () => {
  it('liest neu, bearbeiten und nichts', () => {
    expect(parseModal(new URLSearchParams('modal=new'))).toEqual({ kind: 'new' });
    expect(parseModal(new URLSearchParams('edit=12'))).toEqual({ kind: 'edit', id: 12 });
    expect(parseModal(new URLSearchParams('edit=0'))).toEqual({ kind: 'none' });
    expect(parseModal(new URLSearchParams('edit=abc'))).toEqual({ kind: 'none' });
  });

  it('schreibt genau eine Fenster', () => {
    expect(modalQuery({ kind: 'edit', id: 3 })).toEqual({ modal: null, edit: 3 });
    expect(modalQuery({ kind: 'none' })).toEqual({ modal: null, edit: null });
  });
});

describe('Fehler am Feld', () => {
  it('nimmt params.field, sonst den Code', () => {
    const tooLong = new ApiError(422, 'validation.too_long', { field: 'name', limit: 64 }, '');
    expect(fieldError(tooLong, { 'field:name': 'name' }).field).toBe('name');
    const slug = new ApiError(409, 'app.slug_taken', {}, '');
    expect(fieldError(slug, { 'app.slug_taken': 'slug' }).field).toBe('slug');
    expect(fieldError(new ApiError(500, 'internal', {}, ''), {}).field).toBeNull();
  });
});

describe('Überwachung', () => {
  const rule = (enabled: boolean, channel_id: number | null) => ({ type: 'apt_cacher_down', enabled, channel_id });
  it('unterscheidet aktiv, nur Protokoll, aus und keine Regel', () => {
    expect(monitorState([rule(true, 2)], 'apt_cacher_down')).toBe('active');
    expect(monitorState([rule(true, null)], 'apt_cacher_down')).toBe('logOnly');
    expect(monitorState([rule(false, 2)], 'apt_cacher_down')).toBe('disabled');
    expect(monitorState([rule(false, 2), rule(true, 1)], 'apt_cacher_down')).toBe('active');
    expect(monitorState([], 'apt_cacher_down')).toBe('none');
  });
});

describe('Paketquelle', () => {
  it('verlangt deb-Zeile bei apt und URL sonst', () => {
    const base = { ...emptyRepoForm(), name: 'Docker', key: 'docker' };
    expect(validateRepoForm({ ...base, line: 'deb https://x stable main' })).toEqual({});
    expect(validateRepoForm({ ...base, line: 'https://x' }).line).toBeDefined();
    expect(validateRepoForm({ ...base, package_manager: 'dnf', line: 'https://x/docker.repo' })).toEqual({});
    expect(validateRepoForm({ ...base, line: 'deb https://x $(id) main' }).line).toBe('settings.repositories.form.unsafe');
  });

  it('prüft Kennung und Schlüsseladresse', () => {
    const errors = validateRepoForm({ ...emptyRepoForm(), key: 'Node JS', key_url: 'http://x', line: 'deb x' });
    expect(errors.key).toBeDefined();
    expect(errors.key_url).toBe('settings.repositories.form.keyUrlHttps');
    expect(errors.name).toBeDefined();
  });
});

describe('APT-Cache', () => {
  const caches = [
    { id: 1, name: 'Berlin', url: 'http://10.0.0.1:3142' },
    { id: 2, name: 'Hamburg', url: 'http://10.0.0.2:3142' },
  ];
  const server = (url: string, active = true) => ({ id: 1, name: 'a', host: 'a', apt_proxy_active: active, apt_proxy_url: url, crowdsec_installed: false, crowdsec_active: false, crowdsec_lapi_mode: '', crowdsec_lapi_url: '' });

  it('zählt Server je Cache, auch mit Schrägstrich am Ende', () => {
    const servers = [server('http://10.0.0.1:3142/'), server('http://10.0.0.1:3142', false), server('http://10.0.0.2:3142')];
    expect(connectedCount(servers, caches[0])).toBe(1);
    expect(cacheNameFor(caches, 'http://10.0.0.2:3142')).toBe('Hamburg');
    expect(cacheNameFor(caches, 'http://other')).toBe('http://other');
  });

  it('wählt den Cache aus der Adresse, sonst den ersten', () => {
    expect(selectedCache(caches, '2')?.name).toBe('Hamburg');
    expect(selectedCache(caches, '9')?.name).toBe('Berlin');
    expect(selectedCache([], null)).toBeUndefined();
  });

  it('prüft die Adresse wie das Backend', () => {
    expect(validateCacheForm({ name: 'x', url: 'http://10.0.0.1:3142' })).toEqual({});
    expect(validateCacheForm({ name: 'x', url: 'ftp://x' }).url).toBeDefined();
    expect(validateCacheForm({ name: '', url: 'http://a b' })).toEqual({ name: 'settings.aptCache.form.nameRequired', url: 'settings.aptCache.form.urlInvalid' });
  });

  it('normalisiert die Übersicht ohne Statistik', () => {
    expect(normaliseOverview({ url: '', managed: false, server_id: 8 })).toMatchObject({ stats: null, server_id: 8, disk_usage: '' });
  });
});

describe('Anwendung', () => {
  it('prüft Merkmale wie das Backend', () => {
    expect(markerProblem('')).toBe('settings.apps.form.markersRequired');
    expect(markerProblem('# nur Kommentar')).toBe('settings.apps.form.markersRequired');
    expect(markerProblem('path /opt/app\nunit app.service')).toBeUndefined();
    expect(markerProblem('file /opt/app')).toBe('settings.apps.form.markersFormat');
    expect(markerProblem('path /opt/$(x)')).toBe('settings.apps.form.markersUnsafe');
  });

  it('prüft die Quelle der neuesten Version', () => {
    expect(sourceProblem('')).toBeUndefined();
    expect(sourceProblem('github:AdguardTeam/AdGuardHome')).toBeUndefined();
    expect(sourceProblem('url:https://example.org/releases')).toBeUndefined();
    expect(sourceProblem('github:nur-owner')).toBeDefined();
    expect(sourceProblem('url:http://x')).toBeDefined();
  });

  it('meldet ungültige Muster und baut den Körper', () => {
    const form = { ...emptyAppForm(), slug: 'Adguard', name: ' AdGuard ', markers: 'bin AdGuardHome', version_pattern: '(' };
    expect(validateAppForm(form).version_pattern).toBeDefined();
    const body = appBody({ ...form, backup_action_id: '4' });
    expect(body).toMatchObject({ slug: 'adguard', name: 'AdGuard', backup_action_id: 4, update_action_id: null });
  });

  it('normalisiert fehlende Felder', () => {
    expect(normaliseApps([{ id: 1, slug: 'x' }])[0]).toMatchObject({ enabled: true, compare: 'semver', backup_action_id: null });
  });
});

describe('Kanäle', () => {
  it('liest die Konfiguration aus dem JSON-Text', () => {
    const [channel] = normaliseChannels([{ id: 1, name: 'Ops', type: 'email', enabled: true, config: '{"host":"smtp","port":25,"recipients":["a@b"],"use_tls":false}' }]);
    expect(channel.email).toMatchObject({ host: 'smtp', port: 25, recipients: ['a@b'], security: 'none' });
    expect(normaliseChannels([{ id: 2, config: 'kaputt' }])[0].email.port).toBe(587);
  });

  it('liest die Verschlüsselung: security, sonst use_tls, sonst STARTTLS', () => {
    const security = (config: string) => normaliseChannels([{ id: 1, type: 'email', config }])[0].email.security;
    expect(security('{"security":"tls","use_tls":false}')).toBe('tls');
    expect(security('{"use_tls":true}')).toBe('starttls');
    expect(security('{"use_tls":false}')).toBe('none');
    expect(security('{"host":"smtp"}')).toBe('starttls');
  });
});
