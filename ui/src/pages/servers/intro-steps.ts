// Die Stationen der Einführung auf der Seite des LCM-Hosts: was sie zeigen, wo und mit welchen Texten. Reine Regeln ohne
// Oberfläche; wer sie ausführt (navigieren, hervorheben), ist `lcm-srv-intro`.

import type { ServerTab } from '../../api/servers';

/** Die Seiten des Einstellungsfensters, die die Einführung durchblättert. */
export type SettingsSection = 'operation' | 'access' | 'dns' | 'time';

/** Wo die Seite stehen muss: der Reiter des Servers und, falls dessen Einstellungsfenster offen sein soll, dessen Seite. */
export interface IntroView {
  tab: ServerTab;
  section?: SettingsSection;
}

export interface IntroStepDef {
  key: string;
  /** Die Station (ab 1) in der Anzeige „Station x von n“; mehrere Schritte können zu einer Station gehören. */
  station: number;
  /** Bei einer Station aus mehreren Schritten: welcher Teil von wie vielen. */
  part?: { number: number; total: number };
  titleKey: string;
  textKey: string;
  badgeKey?: string;
  view: IntroView;
  /** Pfade der Elemente, die hervorgehoben werden (siehe `deepQuery`); leer, wenn nichts zu zeigen ist. */
  targets: string[];
}

export interface IntroContext {
  /** Was die Karte des Hosts anbietet: `setup` (Trivy, apt-cacher-ng), sonst nur die Erklärung. */
  hostCards: boolean;
  /** Das Recht, Server zu ändern: Ohne gibt es weder Knöpfe in der Leiste noch Einstellungen. */
  write: boolean;
  /** DSM und RouterOS: keine SSH-Härtung, keine Firewall, keine Zugangs-, DNS- und Zeit-Einstellungen. */
  device: boolean;
  /** Proxmox: Der Benutzerabgleich steht dort nicht in den Einstellungen. */
  proxmox: boolean;
  /** Die Reiter, die der Server hat und die der Benutzer sehen darf. */
  tabs: readonly ServerTab[];
}

/** Die Reiter der Einführung in ihrer Reihenfolge; Repositories bleibt aus. */
const TOUR_TABS = ['packages', 'apps', 'users', 'security', 'deep-scan', 'storage', 'jobs'] as const;
type TourTab = (typeof TOUR_TABS)[number];

const TAB_TEXT: Record<TourTab, string> = {
  packages: 'setup.intro.tabs.packages',
  apps: 'setup.intro.tabs.apps',
  users: 'setup.intro.tabs.users',
  security: 'setup.intro.tabs.security',
  'deep-scan': 'setup.intro.tabs.deepScan',
  storage: 'setup.intro.tabs.storage',
  jobs: 'setup.intro.tabs.jobs',
};

const OVERVIEW: IntroView = { tab: 'overview' };

type Draft = Omit<IntroStepDef, 'station' | 'part'>;

const tour = (name: string) => `[data-tour="${name}"]`;

function settingsDrafts(ctx: IntroContext): Draft[] {
  const operation = (key: string, textKey: string, targets: string[]): Draft => ({
    key,
    titleKey: 'setup.intro.settings.operation.title',
    textKey,
    view: { tab: 'overview', section: 'operation' },
    targets,
  });
  const page = (section: 'access' | 'dns' | 'time', titleKey: string, textKey: string): Draft => ({
    key: `settings-${section}`,
    titleKey,
    textKey,
    view: { tab: 'overview', section },
    targets: [tour(section)],
  });
  return [
    operation('settings-operation', 'setup.intro.settings.operation.text', [tour('flag-maintenance'), tour('flag-unreachable_uncritical')]),
    ...(ctx.proxmox || ctx.device ? [] : [operation('settings-user-sync', 'setup.intro.settings.userSync.text', [tour('flag-user_sync_disabled')])]),
    ...(ctx.device
      ? []
      : [
          page('access', 'setup.intro.settings.access.title', 'setup.intro.settings.access.text'),
          page('dns', 'setup.intro.settings.dns.title', 'setup.intro.settings.dns.text'),
          page('time', 'setup.intro.settings.time.title', 'setup.intro.settings.time.text'),
        ]),
  ];
}

/** Die Schritte jeder Station, die es für diesen Server gibt. Fehlt das Ziel einer Station, entfällt sie ganz. */
function stationDrafts(ctx: IntroContext): Draft[][] {
  const stations: Draft[][] = [];
  stations.push([{ key: 'host', titleKey: 'setup.intro.host.title', textKey: 'setup.intro.host.text', view: OVERVIEW, targets: ['[data-card="lcm-host"]'] }]);
  if (ctx.hostCards) {
    stations.push([{ key: 'trivy', titleKey: 'serverDetail.lcmHost.trivy.name', textKey: 'setup.intro.trivy', badgeKey: 'setup.intro.recommended', view: OVERVIEW, targets: ['[data-service="trivy"]'] }]);
    stations.push([{ key: 'apt-cacher', titleKey: 'serverDetail.lcmHost.aptCacher.name', textKey: 'setup.intro.aptCacher', view: OVERVIEW, targets: ['[data-service="aptCacher"]'] }]);
  }
  if (ctx.write && !ctx.device) {
    stations.push([{ key: 'hardening', titleKey: 'setup.intro.hardening.title', textKey: 'setup.intro.hardening.text', view: OVERVIEW, targets: [tour('harden'), tour('firewall')] }]);
  }
  if (ctx.write) stations.push(settingsDrafts(ctx));
  const tabs = TOUR_TABS.filter((tab) => ctx.tabs.includes(tab));
  if (tabs.length > 0) {
    stations.push(tabs.map((tab) => ({ key: `tab-${tab}`, titleKey: `servers.tabs.${tab}`, textKey: TAB_TEXT[tab], view: { tab }, targets: [`${tour('server-tabs')} >>> [role="tab"][id$="-${tab}"]`] })));
  }
  stations.push([{ key: 'groups', titleKey: 'setup.intro.groups.title', textKey: 'setup.intro.groups.text', view: OVERVIEW, targets: ['lcm-srv-groups >>> lcm-card'] }]);
  stations.push([{ key: 'done', titleKey: 'setup.intro.done.title', textKey: 'setup.intro.done.text', view: OVERVIEW, targets: [] }]);
  return stations;
}

/** Alle Schritte in der Reihenfolge der Einführung, durchnummeriert nach Stationen. */
export function introSteps(ctx: IntroContext): IntroStepDef[] {
  return stationDrafts(ctx).flatMap((drafts, index) =>
    drafts.map((draft, position) => ({
      ...draft,
      station: index + 1,
      ...(drafts.length > 1 ? { part: { number: position + 1, total: drafts.length } } : {}),
    })),
  );
}
