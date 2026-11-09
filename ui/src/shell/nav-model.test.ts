import { beforeAll, describe, expect, it } from 'vitest';
import { i18n } from '../lib/i18n';
import { accountItems, currentNavId, initialsOf, languageItems, mainNavItems } from './nav-model';

beforeAll(() => i18n.setLanguage('de'));

const can = (...permissions: string[]) => (permission: string) => permissions.includes(permission);

describe('mainNavItems', () => {
  it('lists the translated pages the user may see, with their paths', () => {
    expect(mainNavItems(can('servers:read', 'groups:read', 'jobs:read'), false)).toEqual([
      { id: 'dashboard', label: 'Dashboard', href: '/' },
      { id: 'servers', label: 'Server', href: '/servers' },
      { id: 'groups', label: 'Gruppen', href: '/groups' },
      { id: 'jobs', label: 'Jobs', href: '/jobs' },
    ]);
  });

  it('is empty in a mandatory step, where no page is reachable', () => {
    expect(mainNavItems(can('servers:read'), true)).toEqual([]);
  });

  it('shows Container and Sicherheit with packages:read and Einstellungen with any settings permission', () => {
    expect(mainNavItems(can('packages:read'), false).map((item) => item.id)).toEqual(['security', 'containers']);
    expect(mainNavItems(can('alerts:manage'), false).map((item) => item.id)).toEqual(['settings']);
  });
});

describe('currentNavId', () => {
  it('marks the menu entry of the current path', () => {
    expect(currentNavId('/')).toBe('dashboard');
    expect(currentNavId('/servers/42/packages')).toBe('servers');
    expect(currentNavId('/settings/alerts')).toBe('settings');
    expect(currentNavId('/account')).toBe('');
    expect(currentNavId('/nowhere')).toBe('');
  });
});

describe('initialsOf', () => {
  it('takes first and last word of the display name, else the start of the username', () => {
    expect(initialsOf({ username: 'tony', display_name: 'Tony Grätscher' })).toBe('TG');
    expect(initialsOf({ username: 'admin', display_name: 'Administrator' })).toBe('AD');
    expect(initialsOf({ username: 'ops.manager' })).toBe('OP');
    expect(initialsOf(null)).toBe('');
  });
});

describe('languageItems', () => {
  it('names each language in itself', () => {
    expect(languageItems(['de', 'en']).map((item) => item.label)).toEqual(['Deutsch', 'English']);
  });
});

describe('accountItems', () => {
  it('names the signed-in user, offers the about window with the version', () => {
    const items = accountItems({ username: 'tony', display_name: 'Tony Grätscher' }, '1.42.2');
    expect(items[0]).toMatchObject({ id: 'account', label: 'Angemeldet als Tony Grätscher', hint: 'Mein Konto' });
    expect(items.find((item) => item.id === 'about')).toMatchObject({ hint: '1.42.2' });
  });

  it('falls back to the plain account entry without a user', () => {
    expect(accountItems(null)[0]).toMatchObject({ id: 'account', label: 'Mein Konto' });
  });
});
