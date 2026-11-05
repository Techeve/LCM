// Seiten der Oberfläche an einer Stelle: Hauptmenü, Tastenkürzel (`g` + Buchstabe),
// Befehlspalette und Seitentitel lesen alle diese Tabelle. Die Rechte entsprechen
// denen des alten Hauptmenüs. Die Einstellungsseiten stehen darunter, gegliedert
// wie im Seitenmenü.

import type { IconName } from '../components/icons';

export type Can = (permission: string) => boolean;

export interface PageEntry {
  id: string;
  path: string;
  /** Übersetzungsschlüssel des Namens (Menü, Palette, Seitentitel). */
  labelKey: string;
  icon: IconName;
  permission?: string;
  /** Eigene Regel statt eines einzelnen Rechts. */
  visible?: (can: Can) => boolean;
  /** Buchstabe nach `g`. */
  hotkey?: string;
  /** Steht im Hauptmenü und in der Palette. */
  menu: boolean;
}

export interface SettingsSection {
  id: string;
  labelKey: string;
  permission: string;
}

export interface SettingsGroup {
  labelKey: string;
  sections: SettingsSection[];
}

// Die Kennungen der Abschnitte entsprechen den alten Adressen, damit Lesezeichen weiter gelten.
export const SETTINGS_GROUPS: SettingsGroup[] = [
  {
    labelKey: 'settings.group.system',
    sections: [
      { id: 'general', labelKey: 'settings.nav.general', permission: 'settings:manage' },
      { id: 'subscription', labelKey: 'settings.nav.subscription', permission: 'settings:manage' },
      { id: 'backups', labelKey: 'settings.nav.backups', permission: 'backups:manage' },
      { id: 'time', labelKey: 'settings.nav.time', permission: 'settings:manage' },
      { id: 'dns', labelKey: 'settings.nav.dns', permission: 'settings:manage' },
    ],
  },
  {
    labelKey: 'settings.group.access',
    sections: [
      { id: 'users', labelKey: 'settings.nav.users', permission: 'users:read' },
      { id: 'apikeys', labelKey: 'settings.nav.apikeys', permission: 'apikeys:manage' },
      { id: 'mcp', labelKey: 'settings.nav.mcp', permission: 'settings:manage' },
      { id: 'security', labelKey: 'settings.nav.security', permission: 'settings:manage' },
    ],
  },
  {
    labelKey: 'settings.group.sources',
    sections: [
      { id: 'repositories', labelKey: 'settings.nav.repositories', permission: 'settings:manage' },
      { id: 'apt-cache', labelKey: 'settings.nav.aptCache', permission: 'settings:manage' },
      { id: 'apps', labelKey: 'settings.nav.apps', permission: 'settings:manage' },
    ],
  },
  {
    labelKey: 'settings.group.protection',
    sections: [
      { id: 'crowdsec', labelKey: 'settings.nav.crowdsec', permission: 'settings:manage' },
      { id: 'allowlists', labelKey: 'settings.nav.allowlists', permission: 'settings:manage' },
    ],
  },
  {
    labelKey: 'settings.group.automation',
    sections: [
      { id: 'schedules', labelKey: 'settings.nav.schedules', permission: 'rules:manage' },
      { id: 'custom-actions', labelKey: 'settings.nav.customActions', permission: 'rules:manage' },
    ],
  },
  {
    labelKey: 'settings.group.notices',
    sections: [
      { id: 'notifications', labelKey: 'settings.nav.notifications', permission: 'notifications:manage' },
      { id: 'alerts', labelKey: 'settings.nav.alerts', permission: 'alerts:manage' },
      { id: 'events', labelKey: 'settings.nav.events', permission: 'settings:manage' },
    ],
  },
];

/** Die Gruppen mit den Abschnitten, die der Benutzer sehen darf; leere Gruppen entfallen. */
export function visibleSettings(can: Can): SettingsGroup[] {
  return SETTINGS_GROUPS.map((group) => ({
    ...group,
    sections: group.sections.filter((section) => can(section.permission)),
  })).filter((group) => group.sections.length > 0);
}

export function findSettingsSection(id: string | undefined): SettingsSection | undefined {
  return SETTINGS_GROUPS.flatMap((group) => group.sections).find((section) => section.id === id);
}

export const PAGES: PageEntry[] = [
  { id: 'dashboard', path: '/', labelKey: 'nav.dashboard', icon: 'dashboard', permission: 'servers:read', hotkey: 'd', menu: true },
  { id: 'servers', path: '/servers', labelKey: 'nav.servers', icon: 'server', permission: 'servers:read', hotkey: 'v', menu: true },
  { id: 'groups', path: '/groups', labelKey: 'nav.groups', icon: 'users', permission: 'groups:read', hotkey: 'g', menu: true },
  { id: 'linux-users', path: '/linux-users', labelKey: 'nav.linuxUsers', icon: 'terminal', permission: 'linuxusers:read', hotkey: 'u', menu: true },
  { id: 'security', path: '/security', labelKey: 'nav.security', icon: 'shield', permission: 'packages:read', hotkey: 's', menu: true },
  { id: 'containers', path: '/containers', labelKey: 'nav.containers', icon: 'container', permission: 'packages:read', hotkey: 'c', menu: true },
  { id: 'jobs', path: '/jobs', labelKey: 'nav.jobs', icon: 'jobs', permission: 'jobs:read', hotkey: 'j', menu: true },
  {
    id: 'settings',
    path: '/settings',
    labelKey: 'nav.settings',
    icon: 'settings',
    visible: (can) => visibleSettings(can).length > 0,
    hotkey: 'e',
    menu: true,
  },
  { id: 'account', path: '/account', labelKey: 'nav.account', icon: 'user', hotkey: 'a', menu: false },
  { id: 'docs', path: '/docs', labelKey: 'nav.docs', icon: 'book', hotkey: 'h', menu: false },
  { id: 'login', path: '/login', labelKey: 'login.title', icon: 'lock', menu: false },
];

export function isAvailable(entry: PageEntry, can: Can): boolean {
  if (entry.visible) return entry.visible(can);
  return !entry.permission || can(entry.permission);
}

function covers(entry: PageEntry, path: string): boolean {
  if (entry.path === '/') return path === '/';
  return path === entry.path || path.startsWith(`${entry.path}/`);
}

/** Die Seite, zu der ein Pfad gehört (`/servers/42/packages` -> Server). */
export function pageFor(path: string): PageEntry | undefined {
  return PAGES.find((entry) => covers(entry, path));
}

/** Schlüssel des Seitentitels; in den Einstellungen der des Abschnitts. */
export function titleKeyFor(path: string): string | undefined {
  const section = /^\/settings\/([^/]+)/.exec(path)?.[1];
  return findSettingsSection(section)?.labelKey ?? pageFor(path)?.labelKey;
}
