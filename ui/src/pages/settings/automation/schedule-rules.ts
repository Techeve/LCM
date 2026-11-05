import { LitElement, css, html, nothing } from 'lit';
import '../../../components/lcm-hover-tip';
import type { ScheduleEntry } from '../../../api/settings-more';
import { baseStyles, utilStyles } from '../../../components/shared';
import { t } from '../../../lib/i18n';
import { typeLabel } from '../../groups/run-support';
import { isGroupSchedule } from './schedule-model';

/**
 * <lcm-schedule-rules .entry=${…}> – Spalte „Regeln“ der Zeitpläne: wie viele Regeln ein Lauf ausführt, in der Blase
 * jede Regel mit ihrem Typ. System-Zeitpläne haben keine Regeln, sie sind eine feste Aufgabe.
 */
export class LcmScheduleRules extends LitElement {
  static override properties = { entry: { attribute: false } };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: inline-flex;
        vertical-align: middle;
      }
      .count {
        text-decoration: underline dotted var(--text-3);
        text-underline-offset: 3px;
        white-space: nowrap;
      }
      .muted {
        white-space: nowrap;
      }
      ul {
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li + li {
        margin-block-start: var(--space-2);
      }
      li span {
        display: block;
        color: var(--text-2);
      }
      li.off {
        color: var(--text-3);
      }
    `,
  ];

  declare entry: ScheduleEntry | undefined;

  constructor() {
    super();
    this.entry = undefined;
  }

  override render() {
    const entry = this.entry;
    if (!entry) return nothing;
    if (!isGroupSchedule(entry)) return html`<span class="muted">${t('schedules.system')}</span>`;
    if (entry.rules.length === 0) return html`<span class="muted">${t('schedules.noRulesShort')}</span>`;
    const active = entry.rules.filter((rule) => rule.enabled).length;
    const off = entry.rules.length - active;
    const text = off > 0 ? t('schedules.countOff', { count: active, off }) : t('schedules.count', { count: active });
    return html`<lcm-hover-tip label=${text}>
      <span slot="trigger" class="count">${text}</span>
      <ul>
        ${entry.rules.map((rule) => {
          // Der Typ steht nur dabei, wenn die Regel anders heißt als ihr Typ.
          const type = typeLabel(rule.type);
          const detail = [rule.name === type ? '' : type, rule.enabled ? '' : t('schedules.ruleOff')].filter(Boolean).join(' · ');
          return html`<li class=${rule.enabled ? '' : 'off'}>${rule.name}${detail ? html`<span>${detail}</span>` : nothing}</li>`;
        })}
      </ul>
    </lcm-hover-tip>`;
  }
}
customElements.define('lcm-schedule-rules', LcmScheduleRules);
