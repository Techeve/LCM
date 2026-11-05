// Die vier Spalten des Dashboards. Jede ist eine Funktion von Daten zu Vorlage;
// ohne Einträge erklärt ein Leerzustand, was hier erscheint. Was eine Live-Aktualisierung neu bringt
// (`fresh`, Schlüssel aus `itemKey`), blendet kurz ein.

import { html, nothing, type TemplateResult } from 'lit';
import { repeat } from 'lit/directives/repeat.js';
import '../../components/lcm-ampel';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import type { IconName } from '../../components/icons';
import { alertLevel, attentionLevel, type DashboardAlert, type DashboardAttention, type DashboardJob, type DashboardRun } from '../../api/dashboard';
import { auth } from '../../lib/auth';
import { formatDuration, formatRelative } from '../../lib/format';
import { i18n, t } from '../../lib/i18n';
import { insightText } from '../../lib/insight-text';
import { kindLabel, statusLabel, statusLevel } from '../jobs/job-labels';
import { actionFor, serverHref } from './attention-actions';

/** Schlüssel je Eintrag, an denen die Spalten neue Einträge erkennen. */
export const itemKey = {
  attention: (item: DashboardAttention): string => String(item.server_id),
  job: (job: DashboardJob): string => job.id,
  run: (run: DashboardRun): string => `${run.schedule_id}@${run.next_run}`,
  alert: (alert: DashboardAlert): string => alert.id,
};

type Fresh = ReadonlySet<string>;

function serverLink(id: number | null, name: string): TemplateResult | string {
  return id === null ? name : html`<a class="srv" href=${serverHref(id)}>${name}</a>`;
}

function empty(icon: IconName, heading: string, text: string): TemplateResult {
  return html`<lcm-empty-state level="3" icon=${icon} heading=${heading} text=${text}></lcm-empty-state>`;
}

function attentionItem(item: DashboardAttention, fresh: Fresh): TemplateResult {
  const action = actionFor(item.insight?.key);
  const level = attentionLevel(item.level);
  return html`<li ?data-fresh=${fresh.has(itemKey.attention(item))}>
    <lcm-ampel level=${level} size="m" label=${t(level === 'crit' ? 'dashboard.level.crit' : 'dashboard.level.warn')}></lcm-ampel>
    <div>
      <div class="title">${serverLink(item.server_id, item.name)}</div>
      <p class="sub">${item.insight ? insightText(item.insight) : t('dashboard.attention.noInsight')}</p>
    </div>
    <lcm-button class="act" size="small" variant="outline" href=${serverHref(item.server_id, action.tab)}>${t(action.labelKey)}</lcm-button>
  </li>`;
}

export function attentionColumn(items: DashboardAttention[], totalServers: number, fresh: Fresh): TemplateResult {
  let body: TemplateResult;
  if (items.length > 0) body = html`<ul>${repeat(items, itemKey.attention, (item) => attentionItem(item, fresh))}</ul>`;
  else if (totalServers === 0) body = empty('server', t('dashboard.attention.noServers.title'), t('dashboard.attention.noServers.text'));
  else body = empty('check', t('dashboard.attention.empty.title'), t('dashboard.attention.empty.text'));
  return html`<lcm-card level="2" heading=${t('dashboard.attention.title')} subtitle=${t('dashboard.attention.subtitle')}>${body}</lcm-card>`;
}

function jobItem(job: DashboardJob, fresh: Fresh): TemplateResult {
  const when = job.finished_at ?? job.started_at;
  const label = kindLabel(job.kind) || job.name;
  return html`<li ?data-fresh=${fresh.has(itemKey.job(job))}>
    <lcm-ampel level=${statusLevel(job.status)} size="m" label=${statusLabel(job.status)}></lcm-ampel>
    <div>
      <div class="title">${job.server_name ? html`${serverLink(job.server_id, job.server_name)} · ` : nothing}${auth.can('jobs:read') ? html`<a href=${`/jobs/${job.id}`}>${label}</a>` : label}</div>
      <p class="sub">${t('dashboard.jobs.result', { status: statusLabel(job.status), when: formatRelative(when) })}</p>
    </div>
    <span class="meta">${job.duration_seconds === null ? '' : formatDuration(job.duration_seconds)}</span>
  </li>`;
}

export function jobsColumn(items: DashboardJob[], fresh: Fresh): TemplateResult {
  const body = items.length > 0 ? html`<ul>${repeat(items, itemKey.job, (job) => jobItem(job, fresh))}</ul>` : empty('jobs', t('dashboard.jobs.empty.title'), t('dashboard.jobs.empty.text'));
  return html`<lcm-card level="2" heading=${t('dashboard.jobs.title')} subtitle=${t('dashboard.jobs.subtitle')}>
    ${body}
    ${items.length > 0 && auth.can('jobs:read') ? html`<lcm-button slot="footer" size="small" variant="ghost" icon="arrow-right" href="/jobs">${t('dashboard.jobs.all')}</lcm-button>` : nothing}
  </lcm-card>`;
}

function upcomingItem(run: DashboardRun, fresh: Fresh): TemplateResult {
  const next = new Date(run.next_run);
  const time = new Intl.DateTimeFormat(i18n.language, { hour: '2-digit', minute: '2-digit' }).format(next);
  const day = new Intl.DateTimeFormat(i18n.language, { weekday: 'short', day: '2-digit', month: '2-digit' }).format(next);
  const rules = run.rules.map((rule) => rule.name).join(', ');
  return html`<li ?data-fresh=${fresh.has(itemKey.run(run))}>
    <span class="when"><strong>${time}</strong><span>${day}</span><span>${formatRelative(next)}</span></span>
    <div>
      <div class="title"><a href=${`/groups/${run.group_id}`}>${run.group_name}</a></div>
      <p class="sub">${run.schedule_name}${rules ? html` · ${rules}` : nothing}</p>
    </div>
  </li>`;
}

export function upcomingColumn(items: DashboardRun[], fresh: Fresh): TemplateResult {
  const body = items.length > 0 ? html`<ul class="upcoming">${repeat(items, itemKey.run, (run) => upcomingItem(run, fresh))}</ul>` : empty('calendar', t('dashboard.upcoming.empty.title'), t('dashboard.upcoming.empty.text'));
  return html`<lcm-card level="2" heading=${t('dashboard.upcoming.title')} subtitle=${t('dashboard.upcoming.subtitle')}>${body}</lcm-card>`;
}

function alertItem(alert: DashboardAlert, fresh: Fresh): TemplateResult {
  const level = alertLevel(alert.severity);
  return html`<li ?data-fresh=${fresh.has(itemKey.alert(alert))}>
    <lcm-ampel level=${level} size="m" label=${t(level === 'crit' ? 'dashboard.level.crit' : 'dashboard.level.warn')}></lcm-ampel>
    <div>
      <div class="title">${alert.rule_name}</div>
      <p class="sub">${alert.server_name ? html`${serverLink(alert.server_id, alert.server_name)} · ` : nothing}${formatRelative(alert.created_at)}</p>
      <p class="sub">${alert.description}</p>
    </div>
  </li>`;
}

export function alertsColumn(items: DashboardAlert[], fresh: Fresh): TemplateResult {
  const body = items.length > 0 ? html`<ul class="two">${repeat(items, itemKey.alert, (alert) => alertItem(alert, fresh))}</ul>` : empty('bell', t('dashboard.alerts.empty.title'), t('dashboard.alerts.empty.text'));
  return html`<lcm-card level="2" heading=${t('dashboard.alerts.title')} subtitle=${t('dashboard.alerts.subtitle')}>${body}</lcm-card>`;
}
