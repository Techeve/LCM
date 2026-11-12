import { describe, expect, it } from 'vitest';
import { visibleTabs } from '../../api/servers';
import { orderedChecks } from './tab-device-security';

describe('Sicherheit für API-Geräte', () => {
  it('zeigt DSM und RouterOS den Reiter Sicherheit, aber nicht Benutzer und Paketquellen', () => {
    for (const os_id of ['dsm', 'routeros']) {
      const tabs = visibleTabs({ os_id, has_snap: false, has_docker: false });
      expect(tabs).toContain('security');
      expect(tabs).not.toContain('users');
      expect(tabs).not.toContain('repositories');
    }
  });

  it('ordnet die Prüfungen wie die DSM-Systemsteuerung, Unbekanntes ans Ende', () => {
    const checks = ['spectre', 'neu', 'firewall', 'autoBlock'].map((key) => ({ key, state: 'ok' as const, value: '', params: {} }));
    expect(orderedChecks(checks).map((c) => c.key)).toEqual(['firewall', 'autoBlock', 'spectre', 'neu']);
  });
});
