// „Was tun?“ am Befund: Wo ein Befund eine naheliegende Stelle hat, an der er sich
// beheben lässt, führt ein Link dorthin (Reiter des Serverdetails). Befunde ohne
// eigene Stelle führen zur Übersicht des Servers.
import { auth } from '../../lib/auth';

export interface InsightAction {
  /** Reiter des Serverdetails; leer = Übersicht. */
  tab: string;
  /** Schlüssel des Knopftextes. */
  labelKey: string;
}

const OVERVIEW: InsightAction = { tab: '', labelKey: 'dashboard.action.overview' };
const CONNECTION: InsightAction = { tab: '', labelKey: 'dashboard.action.connection' };
const PACKAGES: InsightAction = { tab: 'packages', labelKey: 'dashboard.action.packages' };
const SECURITY: InsightAction = { tab: 'security', labelKey: 'dashboard.action.security' };
const STORAGE: InsightAction = { tab: 'storage', labelKey: 'dashboard.action.storage' };
const JOBS: InsightAction = { tab: 'jobs', labelKey: 'dashboard.action.jobs' };
const CONTAINERS: InsightAction = { tab: 'containers', labelKey: 'dashboard.action.containers' };
const REPOSITORIES: InsightAction = { tab: 'repositories', labelKey: 'dashboard.action.repositories' };
const DEEP_SCAN: InsightAction = { tab: 'deep-scan', labelKey: 'dashboard.action.deepScan' };

const ACTIONS: Record<string, InsightAction> = {
  unreachable: CONNECTION,
  unreachableDetail: CONNECTION,
  'actionFailed.connection': CONNECTION,
  inventoryMissing: PACKAGES,
  'actionFailed.packages': PACKAGES,
  updateCheckFailed: PACKAGES,
  packagesStale: PACKAGES,
  packagesStaleCritical: PACKAGES,
  outdatedPackages: PACKAGES,
  rebootRequired: PACKAGES,
  'actionFailed.rules': JOBS,
  'actionFailed.cve_scan': SECURITY,
  cveScanError: SECURITY,
  cveDbStaleDays: SECURITY,
  cveDbStaleHours: SECURITY,
  cveDbStaleRecent: SECURITY,
  criticalVulns: SECURITY,
  highVulns: SECURITY,
  raisedVulns: SECURITY,
  unfixableVulns: SECURITY,
  fixableVulns: SECURITY,
  diskLow: STORAGE,
  volumeLow: STORAGE,
  volumeCritical: STORAGE,
  volumeInodes: STORAGE,
  volumeReadOnly: STORAGE,
  storageDefect: STORAGE,
  dsmPoolDegraded: STORAGE,
  dsmPoolCrashed: STORAGE,
  dsmPoolRepairing: STORAGE,
  dsmVolumeDegraded: STORAGE,
  dsmVolumeCrashed: STORAGE,
  dsmVolumeReadOnly: STORAGE,
  dsmDiskFailed: STORAGE,
  dsmDiskSmart: STORAGE,
  dsmDiskBadSectors: STORAGE,
  dsmDiskWearOut: STORAGE,
  dsmChecksUnknown: JOBS,
  outdatedImages: CONTAINERS,
  dockerPortsExposed: CONTAINERS,
  repoWarning: REPOSITORIES,
  aptWarning: REPOSITORIES,
  deepScanWarnings: DEEP_SCAN,
};

/** Die passende Stelle zum Befund; ohne Befund oder ohne `jobs:read` für den Reiter Jobs führt der Link zur Übersicht des Servers. */
export function actionFor(key: string | undefined): InsightAction {
  const action = (key && ACTIONS[key]) || OVERVIEW;
  return action === JOBS && !auth.can('jobs:read') ? OVERVIEW : action;
}

export function serverHref(serverId: number, tab = ''): string {
  return tab ? `/servers/${serverId}/${tab}` : `/servers/${serverId}`;
}
