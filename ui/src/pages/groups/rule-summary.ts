// Die Einstellung einer Regel in einer Zeile unter ihrem Namen („80/tcp, 443/tcp“). Leer, wenn der Typ
// nichts einstellt; was er tut, sagt dann seine Beschreibung.

import type { AptCache, CustomAction, Rule } from '../../api/groups';
import { t } from '../../lib/i18n';
import { firewallSummary } from './rule-model';

export interface SummaryContext {
  customActions: CustomAction[];
  aptCaches: AptCache[];
}

function firstLine(text: string): string {
  return text.trim().split('\n')[0] ?? '';
}

export function ruleSummary(rule: Rule, context: SummaryContext): string {
  const command = rule.command.trim();
  switch (rule.type) {
    case 'firewall': {
      const ports = firewallSummary(rule.command);
      return t('rules.summary.firewall', { ports: ports || t('rules.summary.noPorts') });
    }
    case 'custom': {
      const action = context.customActions.find((entry) => String(entry.id) === command);
      return t('rules.summary.custom', { name: action?.name ?? `#${command}` });
    }
    case 'apt-proxy': {
      const cache = context.aptCaches.find((entry) => String(entry.id) === command);
      return cache ? t('rules.summary.aptCache', { name: cache.name }) : '';
    }
    case 'packages':
      return command;
    case 'script':
      return firstLine(command);
    case 'docker-compose-update':
    case 'docker-container-update':
      return command ? t('rules.summary.dockerOnly', { name: command }) : t('rules.summary.dockerAll');
    case 'dns-test':
      return command ? t('rules.summary.dnsDomains', { domains: command.split('\n').join(', ') }) : '';
    default:
      return '';
  }
}
