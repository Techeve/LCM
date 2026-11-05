// Was beim Ausführen einer Regel oder eines Zeitplans passiert: Erklärblase und Bestätigung.
// Die Angaben folgen dem, was das Backend tut (`executor.go`): ein Lauf führt die aktiven Regeln
// nacheinander auf allen Servern der Gruppe aus; ein Start von Hand ignoriert das Zeitfenster.

import type { GroupServer, Rule } from '../../api/groups';
import type { Impact } from '../../components/lcm-dialog';
import type { ActionLabels, ActionPreview } from '../../components/lcm-server-action';
import { t } from '../../lib/i18n';
import { effectsOf, typeInfo } from './rule-model';

export function actionLabels(): ActionLabels {
  return {
    meaning: t('groups.run.meaning'),
    executes: t('groups.run.executes'),
    commands: t('groups.run.steps'),
    packages: t('groups.run.servers'),
    reboot: t('groups.run.reboot'),
    reversible: t('groups.run.reversible'),
    duration: '',
    yes: t('groups.run.yes'),
    no: t('groups.run.no'),
    docs: '',
    loading: t('groups.run.loading'),
  };
}

/** Der Name des Regeltyps; unbekannte Typen (etwa aus der System-Gruppe) bleiben wörtlich. */
export function typeLabel(type: string): string {
  const info = typeInfo(type);
  return info ? t(`${info.key}.label`) : type;
}

export function activeRules(rules: Rule[]): Rule[] {
  return rules.filter((rule) => rule.enabled);
}

function serverNames(servers: GroupServer[]): string {
  const names = servers.map((server) => server.name);
  return names.length > 4 ? `${names.slice(0, 4).join(', ')} +${names.length - 4}` : names.join(', ');
}

/** Inhalt der Erklärblase am Knopf „Jetzt ausführen“. */
export function runPreview(rules: Rule[], servers: GroupServer[]): ActionPreview {
  const active = activeRules(rules);
  const effects = effectsOf(active);
  return {
    commands: active.map((rule, index) => `${index + 1}. ${rule.name} (${typeLabel(rule.type)})`),
    packages: servers.map((server) => server.name),
    reboot: effects.reboot ? true : effects.maybeReboot ? undefined : false,
    reversible: effects.reversible,
  };
}

/** Wirkung für den Bestätigungsdialog. */
export function runImpacts(rules: Rule[], servers: GroupServer[]): Impact[] {
  const active = activeRules(rules);
  const effects = effectsOf(active);
  const impacts: Impact[] = [
    { label: t('groups.run.servers'), value: `${serverNames(servers)} (${servers.length})` },
    { label: t('groups.run.executes'), value: t('groups.run.ruleCount', { count: active.length }) },
  ];
  if (effects.reboot) impacts.push({ label: t('groups.run.reboot'), value: t('groups.run.yes'), tone: 'warn' });
  else if (effects.maybeReboot) impacts.push({ label: t('groups.run.reboot'), value: t('groups.run.maybe'), tone: 'warn' });
  else impacts.push({ label: t('groups.run.reboot'), value: t('groups.run.no'), tone: 'ok' });
  impacts.push({
    label: t('groups.run.reversible'),
    value: effects.reversible ? t('groups.run.yes') : t('groups.run.no'),
    tone: effects.reversible ? 'ok' : 'warn',
  });
  return impacts;
}
