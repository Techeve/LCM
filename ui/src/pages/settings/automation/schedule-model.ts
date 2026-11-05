// Zeitpläne: was ein Lauf von Hand bewirkt. Gruppen-Zeitpläne führen ihre aktiven Regeln auf allen
// Servern der Gruppe aus (wie im Gruppen-Reiter); System-Zeitpläne kommen aus dem Scheduler
// (`TriggerSystem`) und wirken je nach Art auf LCM selbst oder auf alle Server.

import type { ScheduleEntry } from '../../../api/settings-more';
import type { Impact } from '../../../components/lcm-dialog';
import type { ActionPreview } from '../../../components/lcm-server-action';
import { t } from '../../../lib/i18n';
import { cronText } from '../../groups/cron-describe';
import { effectsOf } from '../../groups/rule-model';
import { typeLabel } from '../../groups/run-support';

interface SystemKind {
  label: string;
  help: string;
  where: string;
  reversible: boolean;
}

// Die Namen der System-Zeitpläne liefert das Backend fest auf Deutsch; die Oberfläche benennt sie selbst.
export const SYSTEM_KINDS: Record<string, SystemKind> = {
  backup: { label: 'settings.schedules.kind.backup', help: 'settings.schedules.kind.backupHelp', where: 'settings.schedules.where.lcm', reversible: true },
  cleanup: { label: 'settings.schedules.kind.cleanup', help: 'settings.schedules.kind.cleanupHelp', where: 'settings.schedules.where.database', reversible: false },
  'cve-scan': { label: 'settings.schedules.kind.cveScan', help: 'settings.schedules.kind.cveScanHelp', where: 'settings.schedules.where.allServers', reversible: true },
  'alert-check': { label: 'settings.schedules.kind.alertCheck', help: 'settings.schedules.kind.alertCheckHelp', where: 'settings.schedules.where.lcm', reversible: true },
  'update-check': { label: 'settings.schedules.kind.updateCheck', help: 'settings.schedules.kind.updateCheckHelp', where: 'settings.schedules.where.lcm', reversible: true },
};

export function isGroupSchedule(entry: ScheduleEntry): boolean {
  return entry.kind === 'schedule';
}

export function scheduleName(entry: ScheduleEntry): string {
  const kind = SYSTEM_KINDS[entry.kind];
  return isGroupSchedule(entry) || !kind ? entry.name : t(kind.label);
}

/** Wann der Zeitplan läuft. Eingebaute Zeitpläne nutzen „@every 30m“, das der Cron-Baukasten nicht kennt. */
export function whenText(expression: string): string {
  const every = /^@every (\d+)([mh])$/.exec(expression.trim());
  if (!every) return cronText(expression);
  const count = Number(every[1]);
  return t(every[2] === 'h' ? 'settings.schedules.everyHours' : 'settings.schedules.everyMinutes', { count });
}

export function triggerHelp(entry: ScheduleEntry): string {
  const kind = SYSTEM_KINDS[entry.kind];
  if (isGroupSchedule(entry)) return t('settings.schedules.trigger.groupHelp');
  return kind ? t(kind.help) : t('settings.schedules.trigger.unknownHelp');
}

/** Inhalt der Erklärblase am Knopf „Jetzt ausführen“. */
export function triggerPreview(entry: ScheduleEntry): ActionPreview {
  if (!isGroupSchedule(entry)) return { reversible: SYSTEM_KINDS[entry.kind]?.reversible, reboot: false };
  const active = entry.rules.filter((rule) => rule.enabled);
  const effects = effectsOf(active);
  return {
    commands: active.map((rule, index) => `${index + 1}. ${rule.name} (${typeLabel(rule.type)})`),
    reboot: effects.reboot ? true : effects.maybeReboot ? undefined : false,
    reversible: effects.reversible,
  };
}

/** Wirkung für die Bestätigung. */
export function triggerImpacts(entry: ScheduleEntry, groupName: string): Impact[] {
  const yesNo = (value: boolean) => (value ? t('settings.more.impact.yes') : t('settings.more.impact.no'));
  if (!isGroupSchedule(entry)) {
    const kind = SYSTEM_KINDS[entry.kind];
    const reversible = kind?.reversible ?? false;
    return [
      { label: t('settings.schedules.impact.where'), value: kind ? t(kind.where) : '–' },
      { label: t('settings.more.impact.reversible'), value: yesNo(reversible), tone: reversible ? 'ok' : 'warn' },
    ];
  }
  const active = entry.rules.filter((rule) => rule.enabled);
  const effects = effectsOf(active);
  const impacts: Impact[] = [
    { label: t('settings.schedules.impact.where'), value: t('settings.schedules.where.group', { group: groupName }) },
    { label: t('settings.schedules.impact.rules'), value: t('settings.schedules.impact.ruleCount', { count: active.length }) },
  ];
  if (effects.reboot || effects.maybeReboot) {
    impacts.push({ label: t('settings.more.action.reboot'), value: t(effects.reboot ? 'settings.schedules.impact.reboot' : 'settings.schedules.impact.maybeReboot'), tone: 'warn' });
  }
  impacts.push({ label: t('settings.more.impact.reversible'), value: yesNo(effects.reversible), tone: effects.reversible ? 'ok' : 'warn' });
  return impacts;
}
