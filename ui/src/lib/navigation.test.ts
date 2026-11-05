import { describe, expect, it } from 'vitest';
import { findSettingsSection, isAvailable, PAGES, pageFor, SETTINGS_GROUPS, titleKeyFor, visibleSettings } from './navigation';

const canOnly = (...permissions: string[]) => (permission: string) => permissions.includes(permission);
const entry = (id: string) => PAGES.find((page) => page.id === id)!;

describe('menu rules (as in the old navigation)', () => {
  it('shows pages only with their permission', () => {
    const can = canOnly('servers:read', 'jobs:read');
    expect(PAGES.filter((page) => page.menu && isAvailable(page, can)).map((page) => page.id)).toEqual(['dashboard', 'servers', 'jobs']);
  });

  it('shows security and containers with packages:read', () => {
    const can = canOnly('packages:read');
    expect(isAvailable(entry('security'), can)).toBe(true);
    expect(isAvailable(entry('containers'), can)).toBe(true);
    expect(isAvailable(entry('groups'), can)).toBe(false);
  });

  it('shows settings as soon as one section is allowed', () => {
    expect(isAvailable(entry('settings'), canOnly())).toBe(false);
    expect(isAvailable(entry('settings'), canOnly('alerts:manage'))).toBe(true);
  });

  it('gives every hotkey only once', () => {
    const keys = PAGES.flatMap((page) => (page.hotkey ? [page.hotkey] : []));
    expect(new Set(keys).size).toBe(keys.length);
  });
});

describe('settings', () => {
  it('is grouped like the plan', () => {
    expect(SETTINGS_GROUPS.map((group) => group.sections.map((section) => section.id))).toEqual([
      ['general', 'subscription', 'backups', 'time', 'dns'],
      ['users', 'apikeys', 'mcp', 'security'],
      ['repositories', 'apt-cache', 'apps'],
      ['crowdsec', 'allowlists'],
      ['schedules', 'custom-actions'],
      ['notifications', 'alerts', 'events'],
    ]);
  });

  it('drops sections without permission and groups that become empty', () => {
    const groups = visibleSettings(canOnly('rules:manage', 'alerts:manage'));
    expect(groups.map((group) => group.sections.map((section) => section.id))).toEqual([['schedules', 'custom-actions'], ['alerts']]);
  });

  it('finds sections by id', () => {
    expect(findSettingsSection('apt-cache')?.permission).toBe('settings:manage');
    expect(findSettingsSection('nope')).toBeUndefined();
    expect(findSettingsSection(undefined)).toBeUndefined();
  });
});

describe('titles', () => {
  it('maps paths to their page', () => {
    expect(pageFor('/')?.id).toBe('dashboard');
    expect(pageFor('/servers/42/packages')?.id).toBe('servers');
    expect(pageFor('/serversX')).toBeUndefined();
    expect(pageFor('/login')?.id).toBe('login');
  });

  it('uses the section name inside the settings', () => {
    expect(titleKeyFor('/settings/alerts')).toBe('settings.nav.alerts');
    expect(titleKeyFor('/settings/unknown')).toBe('nav.settings');
    expect(titleKeyFor('/containers/images')).toBe('nav.containers');
    expect(titleKeyFor('/nowhere')).toBeUndefined();
  });
});
