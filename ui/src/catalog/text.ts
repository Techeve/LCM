import type { ActionLabels } from '../components/lcm-server-action.js';
import type { MainNavLabels, NavItem } from '../components/lcm-main-nav.js';
import type { MenuItem } from '../components/lcm-menu.js';
import type { TableLabels } from '../components/lcm-data-table.js';
import type { StatusLevel } from '../components/shared.js';

/** Texte des Katalogs (Deutsch). Die Komponenten selbst enthalten keine Texte. */

export const navItems: NavItem[] = [
  { id: 'dashboard', label: 'Dashboard', href: '#dashboard' },
  { id: 'servers', label: 'Server', href: '#servers' },
  { id: 'groups', label: 'Gruppen', href: '#groups' },
  { id: 'users', label: 'Linux-Benutzer', href: '#users' },
  { id: 'security', label: 'Sicherheit', href: '#security' },
  { id: 'containers', label: 'Container', href: '#containers' },
  { id: 'jobs', label: 'Jobs', href: '#jobs' },
  { id: 'settings', label: 'Einstellungen', href: '#settings' },
];

export const navLabels: MainNavLabels = {
  nav: 'Hauptmenü',
  search: 'Suchen',
  docs: 'Dokumentation',
  theme: 'Darstellung',
  language: 'Sprache',
  account: 'Konto',
  menu: 'Menü',
};

export const themeOptions: MenuItem[] = [
  { id: 'auto', label: 'Automatisch', icon: 'auto' },
  { id: 'light', label: 'Hell', icon: 'sun' },
  { id: 'dark', label: 'Dunkel', icon: 'moon' },
];

export const languages: MenuItem[] = [
  { id: 'de', label: 'Deutsch', hint: 'de' },
  { id: 'en', label: 'English', hint: 'en' },
  { id: 'fr', label: 'Français', hint: 'fr' },
  { id: 'pl', label: 'Polski', hint: 'pl' },
  { id: 'nl', label: 'Nederlands', hint: 'nl' },
];

export const accountItems: MenuItem[] = [
  { id: 'account', label: 'Mein Konto', icon: 'user' },
  { id: 'shortcuts', label: 'Tastenkürzel', icon: 'keyboard', hint: '?' },
  { id: 'logout', label: 'Abmelden', icon: 'logout', separatorBefore: true },
];

export const levelLabels: Record<StatusLevel, string> = {
  great: 'hervorragend',
  ok: 'in Ordnung',
  warn: 'Warnung',
  crit: 'kritisch',
  unknown: 'unbekannt',
  running: 'läuft gerade',
};

export const tableLabels: TableLabels = {
  caption: 'Server',
  filter: 'Filtern',
  all: 'Alle',
  columns: 'Spalten',
  densityComfortable: 'Komfortabel',
  densityCompact: 'Kompakt',
  selectAll: 'Alle sichtbaren Zeilen auswählen',
  selectRow: '{name} auswählen',
  selected: '{count} ausgewählt',
  clear: 'Auswahl aufheben',
  empty: 'Kein Server entspricht den Filtern.',
  sortedAsc: 'aufsteigend sortiert',
  sortedDesc: 'absteigend sortiert',
};

export const actionLabels: ActionLabels = {
  meaning: 'Für dich:',
  executes: 'Wird ausgeführt',
  commands: 'Befehle',
  packages: 'Pakete',
  reboot: 'Neustart nötig',
  reversible: 'Umkehrbar',
  duration: 'Dauer',
  yes: 'Ja',
  no: 'Nein',
  docs: 'Mehr in der Doku',
  loading: 'Vorschau wird geladen',
};

export const settingsSections = [
  { label: 'System', items: ['Allgemein', 'Subscription', 'Backups', 'Zeit', 'DNS'] },
  { label: 'Zugang', items: ['Benutzer', 'API-Schlüssel', 'MCP', 'Sicherheit'] },
  { label: 'Pakete & Quellen', items: ['Repositories', 'APT-Cache', 'Apps'] },
  { label: 'Schutz', items: ['CrowdSec', 'Allowlists'] },
  { label: 'Automatisierung', items: ['Zeitpläne', 'Eigene Aktionen'] },
  { label: 'Meldungen', items: ['Benachrichtigungen', 'Alarme', 'Ereignisse'] },
].map((section) => ({
  label: section.label,
  items: section.items.map((label) => ({ id: label.toLowerCase().replace(/\W+/g, '-'), label, href: `#settings-${label}` })),
}));
