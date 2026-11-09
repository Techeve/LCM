// Beschriftung und Ampelstufe von Jobs: Status und Art. Die Schlüssel stehen als
// Wörterbuch im Code, damit die Prüfung der Sprachdateien sie findet.

import type { StatusLevel } from '../../components/shared';
import { i18n, t } from '../../lib/i18n';

const STATUS_KEYS: Record<string, string> = {
  success: 'jobs.status.success',
  failed: 'jobs.status.failed',
  aborted: 'jobs.status.aborted',
  blocked: 'jobs.status.blocked',
  running: 'jobs.status.running',
  pending: 'jobs.status.pending',
};

const STATUS_LEVELS: Record<string, StatusLevel> = {
  success: 'great',
  failed: 'crit',
  aborted: 'warn',
  blocked: 'warn',
  running: 'running',
  pending: 'unknown',
};

const KIND_KEYS: Record<string, string> = {
  update: 'jobs.kind.update',
  packages: 'jobs.kind.packages',
  security: 'jobs.kind.security',
  'package-scan': 'jobs.kind.package-scan',
  autoremove: 'jobs.kind.autoremove',
  script: 'jobs.kind.script',
  custom: 'jobs.kind.custom',
  sync: 'jobs.kind.sync',
  health: 'jobs.kind.health',
  firewall: 'jobs.kind.firewall',
  backup: 'jobs.kind.backup',
  cleanup: 'jobs.kind.cleanup',
  'cve-scan': 'jobs.kind.cve-scan',
  'docker-check': 'jobs.kind.docker-check',
  'app-check': 'jobs.kind.app-check',
  'docker-update': 'jobs.kind.docker-update',
  'docker-prune': 'jobs.kind.docker-prune',
  'docker-update-unused': 'jobs.kind.docker-update-unused',
  'docker-compose-update': 'jobs.kind.docker-compose-update',
  'docker-container-update': 'jobs.kind.docker-container-update',
  'apt-proxy': 'jobs.kind.apt-proxy',
  'alert-check': 'jobs.kind.alert-check',
  reboot: 'jobs.kind.reboot',
  'reboot-if-needed': 'jobs.kind.reboot-if-needed',
  'dns-test': 'jobs.kind.dns-test',
  'deep-scan': 'jobs.kind.deep-scan',
  'acl-setup': 'jobs.kind.acl-setup',
  'perm-sync': 'jobs.kind.perm-sync',
  'hardware-refresh': 'jobs.kind.hardware-refresh',
  'full-refresh': 'jobs.kind.full-refresh',
  'repair-dpkg': 'jobs.kind.repair-dpkg',
  'repair-deps': 'jobs.kind.repair-deps',
  join: 'jobs.kind.join',
  'harden-ssh': 'jobs.kind.harden-ssh',
};

export function statusLabel(status: string): string {
  const key = STATUS_KEYS[status];
  return key ? t(key) : status;
}

export function statusLevel(status: string): StatusLevel {
  return STATUS_LEVELS[status] ?? 'unknown';
}

/** Art eines Jobs in Worten; unbekannte Arten erscheinen unverändert. */
export function kindLabel(kind: string): string {
  const key = KIND_KEYS[kind];
  return (key && i18n.tryT(key)) || kind;
}
