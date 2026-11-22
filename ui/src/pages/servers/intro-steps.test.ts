import { describe, expect, it } from 'vitest';
import type { ServerTab } from '../../api/servers';
import { introSteps, type IntroContext } from './intro-steps';

const ALL_TABS: ServerTab[] = ['overview', 'repositories', 'packages', 'snaps', 'containers', 'apps', 'users', 'security', 'deep-scan', 'storage', 'jobs', 'ssh'];
const FULL: IntroContext = { hostCards: true, write: true, device: false, proxmox: false, tabs: ALL_TABS };

const keys = (ctx: IntroContext) => introSteps(ctx).map((step) => step.key);
const stations = (ctx: IntroContext) => [...new Set(introSteps(ctx).map((step) => step.station))];

describe('Stationen der Einführung', () => {
  it('führt in acht Stationen durch Host, Trivy, apt-cacher-ng, Härtung, Einstellungen, Reiter, Gruppen und Abschluss', () => {
    const steps = introSteps(FULL);
    expect(stations(FULL)).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
    expect(steps.filter((step) => step.station <= 4 || step.station >= 7).map((step) => step.key)).toEqual(['host', 'trivy', 'apt-cacher', 'hardening', 'groups', 'done']);
    expect(steps[0]!.titleKey).toBe('setup.intro.host.title');
    expect(steps.at(-1)!.titleKey).toBe('setup.intro.done.title');
  });

  it('blättert die Seiten des Einstellungsfensters durch: Betrieb, Benutzerabgleich, Zugang, DNS, Zeit', () => {
    const settings = introSteps(FULL).filter((step) => step.station === 5);
    expect(settings.map((step) => step.view.section)).toEqual(['operation', 'operation', 'access', 'dns', 'time']);
    expect(settings.map((step) => step.part)).toEqual([1, 2, 3, 4, 5].map((number) => ({ number, total: 5 })));
    expect(settings.every((step) => step.view.tab === 'overview')).toBe(true);
    expect(settings[0]!.targets).toEqual(['[data-tour="flag-maintenance"]', '[data-tour="flag-unreachable_uncritical"]']);
  });

  it('geht durch die Reiter Pakete, Apps, Benutzer, Sicherheit, Deep-Scan, Speicher und Jobs und lässt Repositories aus', () => {
    const tabs = introSteps(FULL).filter((step) => step.station === 6);
    expect(tabs.map((step) => step.view.tab)).toEqual(['packages', 'apps', 'users', 'security', 'deep-scan', 'storage', 'jobs']);
    expect(tabs[0]!.targets[0]).toContain('[data-tour="server-tabs"] >>> [role="tab"][id$="-packages"]');
  });

  it('kehrt für die Gruppen auf die Hauptseite zurück', () => {
    const groups = introSteps(FULL).find((step) => step.key === 'groups')!;
    expect(groups.view).toEqual({ tab: 'overview' });
    expect(groups.targets).toEqual(['lcm-srv-groups >>> lcm-card']);
  });

  it('überspringt Reiter, die der Server nicht hat, und nummeriert die Stationen neu', () => {
    const ctx = { ...FULL, tabs: ALL_TABS.filter((tab) => tab !== 'apps' && tab !== 'deep-scan' && tab !== 'users') };
    expect(introSteps(ctx).filter((step) => step.station === 6).map((step) => step.view.tab)).toEqual(['packages', 'security', 'storage', 'jobs']);
    const none = { ...FULL, tabs: ['overview'] as ServerTab[] };
    expect(stations(none)).toEqual([1, 2, 3, 4, 5, 6, 7]);
    expect(keys(none)).not.toContain('tab-packages');
  });

  it('ohne Karten für Trivy und apt-cacher-ng (Container, ohne apt) entfallen deren Stationen', () => {
    const ctx = { ...FULL, hostCards: false };
    expect(keys(ctx)).not.toContain('trivy');
    expect(keys(ctx)).not.toContain('apt-cacher');
    expect(stations(ctx)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it('ohne Schreibrecht gibt es weder Härtung noch Einstellungen', () => {
    const ctx = { ...FULL, write: false };
    expect(keys(ctx)).not.toContain('hardening');
    expect(introSteps(ctx).some((step) => step.view.section)).toBe(false);
  });

  it('bei Proxmox fehlt der Benutzerabgleich, bei Geräten (DSM, RouterOS) auch Zugang, DNS und Zeit sowie Härtung', () => {
    expect(introSteps({ ...FULL, proxmox: true }).filter((step) => step.station === 5).map((step) => step.key)).not.toContain('settings-user-sync');
    const device = introSteps({ ...FULL, device: true });
    expect(device.filter((step) => step.key.startsWith('settings')).map((step) => step.view.section)).toEqual(['operation']);
    expect(device.map((step) => step.key)).not.toContain('hardening');
  });

  it('hebt Trivy als empfohlen hervor und zeigt nur auf Karten und Fenster, nie auf Knöpfe zum Ausführen', () => {
    const steps = introSteps(FULL);
    expect(steps.find((step) => step.key === 'trivy')!.badgeKey).toBe('setup.intro.recommended');
    expect(steps.find((step) => step.key === 'apt-cacher')!.badgeKey).toBeUndefined();
    expect(steps.some((step) => step.key.includes('lapi') || step.textKey.includes('lapi'))).toBe(false);
  });
});
