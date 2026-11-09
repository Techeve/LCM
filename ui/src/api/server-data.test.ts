import { describe, expect, it } from 'vitest';
import { normaliseApps, normaliseDocker, normaliseSnaps } from './server-data';
import { capabilityLevel, normaliseCapabilities, normaliseServers, pendingOf, visibleTabs, type Capability } from './servers';

describe('Serverliste normalisieren', () => {
  it('liest die Antwort mit items und total und fängt Fehlendes ab', () => {
    const page = normaliseServers({ items: [{ id: 3, name: 'web', status: 'red', insights: [{ severity: 'critical', message: 'x', key: 'k', params: { count: 2 } }], groups: [{ id: 1, name: 'A' }] }, null], total: 40 });
    expect(page.total).toBe(40);
    expect(page.items[0].status).toBe('red');
    expect(page.items[0].insights[0].params).toEqual({ count: '2' });
    expect(page.items[0].groups).toEqual([{ id: 1, name: 'A' }]);
    expect(page.items[1].id).toBe(0);
    expect(page.items[1].status).toBe('green');
  });

  it('nimmt auch ein nacktes Array', () => {
    expect(normaliseServers([{ id: 1 }]).total).toBe(1);
  });
});

describe('Reiter je Servertyp', () => {
  it('blendet Snaps und Container ohne Bestand aus', () => {
    const tabs = visibleTabs({ os_id: 'debian', has_snap: false, has_docker: false });
    expect(tabs).not.toContain('snaps');
    expect(tabs).not.toContain('containers');
    expect(tabs).toContain('packages');
    expect(visibleTabs({ os_id: 'ubuntu', has_snap: true, has_docker: true })).toEqual(expect.arrayContaining(['snaps', 'containers']));
  });

  it('zeigt den Reiter Jobs nur mit jobs:read', () => {
    const server = { os_id: 'debian', has_snap: false, has_docker: false };
    expect(visibleTabs(server, () => true)).toContain('jobs');
    expect(visibleTabs(server, (permission) => permission !== 'jobs:read')).not.toContain('jobs');
  });

  it('lässt reinen API-Geräten nur die Reiter, die zu ihnen passen', () => {
    const tabs = visibleTabs({ os_id: 'routeros', has_snap: false, has_docker: false });
    expect(tabs).not.toEqual(expect.arrayContaining(['repositories']));
    expect(tabs).not.toContain('users');
    expect(tabs).not.toContain('security');
    expect(tabs).not.toContain('deep-scan');
    expect(tabs).toEqual(expect.arrayContaining(['overview', 'jobs', 'storage']));
  });
});

describe('Betriebsfähigkeit', () => {
  const base: Capability = { category: 'packages', state: 'ok', effective: '', failing_since: null, consecutive_failures: 0, last_success_at: null, last_error_class: '', last_error_message: '', last_error_params: {}, last_job_id: '', last_job_name: '', warning_class: '', warning_message: '', stale: '', repair: '', retryable: false };

  it('ordnet die vier Kategorien und verwirft Unbekanntes', () => {
    const rows = normaliseCapabilities([{ category: 'rules' }, { category: 'bogus' }, { category: 'connection' }, { category: 'cve_scan' }, { category: 'packages' }]);
    expect(rows.map((r) => r.category)).toEqual(['connection', 'packages', 'rules', 'cve_scan']);
  });

  it('behält die Parameter der Ursache als Text', () => {
    const [row] = normaliseCapabilities([{ category: 'packages', last_error_params: { pid: 1234, process: 'unattended-upgr' } }]);
    expect(row.last_error_params).toEqual({ pid: '1234', process: 'unattended-upgr' });
    expect(normaliseCapabilities([{ category: 'rules' }])[0].last_error_params).toEqual({});
  });

  it('leitet die Ampel vom geltenden Zustand ab', () => {
    expect(capabilityLevel(base)).toBe('ok');
    expect(capabilityLevel({ ...base, state: 'failed', effective: 'critical' })).toBe('crit');
    expect(capabilityLevel({ ...base, state: 'failed', effective: 'warning' })).toBe('warn');
    expect(capabilityLevel({ ...base, stale: 'critical' })).toBe('crit');
    expect(capabilityLevel({ ...base, stale: 'warning' })).toBe('warn');
    expect(capabilityLevel({ ...base, warning_class: 'x' })).toBe('warn');
  });
});

describe('Pakete', () => {
  it('zählt nur Pakete mit Kandidat als ausstehend', () => {
    expect(pendingOf([{ name: 'a', version: '1', candidate: '', security: false, held_reason: '' }, { name: 'b', version: '1', candidate: '2', security: true, held_reason: '' }]).map((p) => p.name)).toEqual(['b']);
  });
});

describe('Reiterdaten', () => {
  it('liest Snaps, Container und Apps', () => {
    expect(normaliseSnaps([{ name: 'core', candidate_version: '2' }])[0].candidate).toBe('2');
    const docker = normaliseDocker({ containers: [{ container_id: 'x', name: 'web' }], images: [{ repository: 'nginx', tag: '1', superseded: false }, { repository: 'old', superseded: true }] });
    expect(docker.containers[0].id).toBe('x');
    expect(docker.images.map((i) => i.ref)).toEqual(['nginx:1']);
    expect(normaliseApps({ detected: [{ slug: 'wp', name: 'WordPress' }], unknown: [{}, {}] }).unknown).toBe(2);
  });
});
