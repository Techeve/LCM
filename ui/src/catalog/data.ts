import type { Row } from '../components/lcm-data-table.js';
import type { StatusLevel } from '../components/shared.js';
import { levelLabels } from './text.js';

/** Testdaten: eine kleine Flotte, ein paar Jobs, Zeitpläne und Alarme. */

const rank: Record<StatusLevel, number> = { crit: 0, warn: 1, unknown: 2, running: 3, ok: 4, great: 5 };

interface Seed {
  name: string;
  host: string;
  os: string;
  osLabel: string;
  kernel: string;
  level: StatusLevel;
  updates: number;
  seen: string;
  storage: number;
  group: string;
}

const seeds: Seed[] = [
  { name: 'web01', host: 'web01.prod.techeve.de', os: 'debian', osLabel: 'Debian 12', kernel: '6.1.0-28-amd64', level: 'great', updates: 0, seen: 'vor 1 Min.', storage: 41, group: 'Produktion' },
  { name: 'web02', host: 'web02.prod.techeve.de', os: 'debian', osLabel: 'Debian 12', kernel: '6.1.0-28-amd64', level: 'ok', updates: 3, seen: 'vor 1 Min.', storage: 47, group: 'Produktion' },
  { name: 'web03', host: 'web03.prod.techeve.de', os: 'debian', osLabel: 'Debian 12', kernel: '6.1.0-25-amd64', level: 'crit', updates: 27, seen: 'vor 2 Min.', storage: 88, group: 'Produktion' },
  { name: 'db01', host: 'db01.prod.techeve.de', os: 'ubuntu', osLabel: 'Ubuntu 24.04', kernel: '6.8.0-48-generic', level: 'great', updates: 0, seen: 'vor 1 Min.', storage: 63, group: 'Datenbanken' },
  { name: 'db02', host: 'db02.prod.techeve.de', os: 'ubuntu', osLabel: 'Ubuntu 24.04', kernel: '6.8.0-48-generic', level: 'warn', updates: 11, seen: 'vor 4 Min.', storage: 79, group: 'Datenbanken' },
  { name: 'mail01', host: 'mail01.techeve.de', os: 'rocky', osLabel: 'Rocky Linux 9.4', kernel: '5.14.0-427.el9', level: 'ok', updates: 5, seen: 'vor 1 Min.', storage: 35, group: 'Infrastruktur' },
  { name: 'git01', host: 'git01.techeve.de', os: 'ubuntu', osLabel: 'Ubuntu 22.04', kernel: '5.15.0-122-generic', level: 'running', updates: 14, seen: 'gerade eben', storage: 72, group: 'Infrastruktur' },
  { name: 'cache01', host: 'cache01.prod.techeve.de', os: 'alma', osLabel: 'AlmaLinux 9.4', kernel: '5.14.0-427.el9', level: 'great', updates: 0, seen: 'vor 1 Min.', storage: 22, group: 'Produktion' },
  { name: 'monitor01', host: 'monitor01.techeve.de', os: 'debian', osLabel: 'Debian 12', kernel: '6.1.0-28-amd64', level: 'ok', updates: 2, seen: 'vor 3 Min.', storage: 54, group: 'Infrastruktur' },
  { name: 'backup01', host: 'backup01.techeve.de', os: 'opensuse', osLabel: 'openSUSE Leap 15.6', kernel: '6.4.0-150600', level: 'unknown', updates: 0, seen: 'vor 3 Std.', storage: 91, group: 'Infrastruktur' },
  { name: 'build01', host: 'build01.dev.techeve.de', os: 'fedora', osLabel: 'Fedora 41', kernel: '6.11.4-301.fc41', level: 'ok', updates: 38, seen: 'vor 1 Min.', storage: 67, group: 'Entwicklung' },
  { name: 'erp01', host: 'erp01.techeve.de', os: 'rhel', osLabel: 'RHEL 9.4', kernel: '5.14.0-427.el9', level: 'warn', updates: 9, seen: 'vor 6 Min.', storage: 58, group: 'Verwaltung' },
  { name: 'dev-box', host: 'dev-box.dev.techeve.de', os: 'ubuntu', osLabel: 'Ubuntu 24.04', kernel: '6.8.0-48-generic', level: 'ok', updates: 6, seen: 'vor 1 Min.', storage: 44, group: 'Entwicklung' },
  { name: 'proxy01', host: 'proxy01.edge.techeve.de', os: 'debian', osLabel: 'Debian 12', kernel: '6.1.0-28-amd64', level: 'great', updates: 0, seen: 'vor 1 Min.', storage: 18, group: 'Produktion' },
];

export const servers: Row[] = seeds.map((s, i) => ({
  id: `srv-${101 + i}`,
  name: s.name,
  host: s.host,
  os: { os: s.os, label: s.osLabel },
  osName: s.osLabel,
  kernel: s.kernel,
  status: { level: s.level, label: levelLabels[s.level] },
  statusRank: rank[s.level],
  updates: s.updates,
  seen: s.seen,
  storage: s.storage,
  group: s.group,
}));

export const osOptions = [...new Set(seeds.map((s) => s.osLabel))].map((label) => ({ value: label, label }));
export const statusOptions = (Object.keys(levelLabels) as StatusLevel[]).map((level) => ({ value: levelLabels[level], label: levelLabels[level] }));

export interface Attention {
  server: string;
  level: StatusLevel;
  level_label: string;
  title: string;
  text: string;
  action: string;
}

export const attention: Attention[] = [
  { server: 'web03', level: 'crit', level_label: 'kritisch', title: 'Paketquellen nicht erreichbar', text: 'apt update schlägt seit drei Läufen fehl.', action: 'Quellen prüfen' },
  { server: 'web03', level: 'crit', level_label: 'kritisch', title: 'Sicherheitsupdate offen: openssl', text: 'CVE-2026-1182 betrifft diesen Server, Fix verfügbar.', action: 'Update einspielen' },
  { server: 'db02', level: 'warn', level_label: 'Warnung', title: 'Neustart seit 12 Tagen nötig', text: 'Kernel 6.8.0-48 ist installiert, aber nicht aktiv.', action: 'Neustart planen' },
  { server: 'erp01', level: 'warn', level_label: 'Warnung', title: 'Speicher zu 91 % belegt', text: 'Das Dateisystem /var ist zu 91 % belegt.', action: 'Speicher ansehen' },
  { server: 'backup01', level: 'warn', level_label: 'Warnung', title: 'Seit 3 Stunden nicht gesehen', text: 'Der Agent meldet sich nicht mehr.', action: 'Erneut verbinden' },
];

export interface JobRow {
  server: string;
  kind: string;
  result: string;
  level: StatusLevel;
  duration: string;
  when: string;
}

export const jobs: JobRow[] = [
  { server: 'git01', kind: 'Updates einspielen', result: 'läuft gerade', level: 'running', duration: '2:14', when: 'seit 2 Min.' },
  { server: 'web02', kind: 'Updates einspielen', result: 'erfolgreich', level: 'great', duration: '3:41', when: 'vor 12 Min.' },
  { server: 'db01', kind: 'CVE-Scan', result: 'erfolgreich', level: 'great', duration: '0:48', when: 'vor 26 Min.' },
  { server: 'web03', kind: 'Paketlisten aktualisieren', result: 'fehlgeschlagen', level: 'crit', duration: '0:09', when: 'vor 41 Min.' },
  { server: 'mail01', kind: 'Updates einspielen', result: 'erfolgreich', level: 'great', duration: '5:02', when: 'vor 1 Std.' },
  { server: 'cache01', kind: 'Neustart', result: 'erfolgreich', level: 'great', duration: '1:12', when: 'vor 2 Std.' },
];

export interface Schedule {
  group: string;
  cron: string;
  rules: string;
}

export const schedules: Schedule[] = [
  { group: 'Produktion', cron: '30 3 * * 0', rules: 'Sicherheitsupdates, Kerndienste zuletzt' },
  { group: 'Datenbanken', cron: '0 4 * * 6', rules: 'Nur Sicherheitsupdates' },
  { group: 'Entwicklung', cron: '0 2 * * 1-5', rules: 'Alle Updates' },
  { group: 'Infrastruktur', cron: '15 5 1 * *', rules: 'Alle Updates, Neustart bei Bedarf' },
];

export interface AlertRow {
  level: StatusLevel;
  title: string;
  server: string;
  when: string;
}

export const alerts: AlertRow[] = [
  { level: 'crit', title: 'Verbindung verloren', server: 'backup01', when: 'vor 3 Std.' },
  { level: 'crit', title: 'Job fehlgeschlagen: Paketlisten', server: 'web03', when: 'vor 41 Min.' },
  { level: 'warn', title: 'Neustart seit 10 Tagen offen', server: 'db02', when: 'gestern' },
  { level: 'warn', title: 'Speicher über 85 %', server: 'erp01', when: 'gestern' },
];

