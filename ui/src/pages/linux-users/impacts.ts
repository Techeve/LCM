// Auswirkungen für die Bestätigungsdialoge: welche Server die Aktion berührt, was dort passiert, ob sie sich umkehren lässt.

import type { Impact } from '../../components/lcm-dialog';
import type { UserServer } from '../../api/linux-users';
import { t } from '../../lib/i18n';

export const NAMED_SERVERS = 4;

/** Die ersten Servernamen, der Rest als „… und n weitere“. */
export function serverNames(servers: UserServer[], max = NAMED_SERVERS): string {
  const names = servers.slice(0, max).map((s) => s.name);
  if (servers.length > max) names.push(t('linuxUsers.impact.more', { count: servers.length - max }));
  return names.join(', ');
}

export interface ImpactOptions {
  servers: UserServer[];
  /** Was auf den Servern passiert (übersetzter Satz). */
  effect?: string;
  /** Lässt sich die Aktion rückgängig machen? Ohne Angabe entfällt die Zeile. */
  reversible?: boolean;
  /** Weitere Zeilen, etwa ein Hinweis zur Gruppe. */
  extra?: Impact[];
}

export function buildImpacts({ servers, effect, reversible, extra = [] }: ImpactOptions): Impact[] {
  const syncing = servers.filter((s) => !s.user_sync_disabled);
  const skipped = servers.filter((s) => s.user_sync_disabled);
  const impacts: Impact[] =
    servers.length === 0
      ? [{ label: t('linuxUsers.impact.servers'), value: t('linuxUsers.impact.noServers'), tone: 'ok' }]
      : [{ label: t('linuxUsers.impact.servers'), value: t('linuxUsers.impact.serversValue', { count: syncing.length, names: serverNames(syncing) }), tone: syncing.length > 0 ? 'warn' : 'ok' }];
  if (skipped.length > 0) impacts.push({ label: t('linuxUsers.impact.skipped'), value: serverNames(skipped), tone: 'ok' });
  if (effect) impacts.push({ label: t('linuxUsers.impact.effect'), value: effect });
  impacts.push(...extra);
  if (reversible !== undefined) {
    impacts.push({ label: t('linuxUsers.impact.reversible'), value: reversible ? t('linuxUsers.impact.yes') : t('linuxUsers.impact.no'), tone: reversible ? 'ok' : 'crit' });
  }
  return impacts;
}
