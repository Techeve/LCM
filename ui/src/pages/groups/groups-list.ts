import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-ampel';
import '../../components/lcm-badge';
import '../../components/lcm-status';
import { baseStyles } from '../../components/shared';
import type { Group } from '../../api/groups';
import { I18nController } from '../../lib/controllers';
import { formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { aclStatus } from './group-model';
import { groupPath } from './url-state';

/**
 * <lcm-groups-list .groups=${…} active-id="3" ?compact> – die Gruppen als Liste von Links. Breit (ohne `compact`)
 * mit Spalten für Server, Verwalter, Vorrang, ACL und Zeitpläne, schmal neben dem Detail mit Kurzinfo.
 */
export class LcmGroupsList extends LitElement {
  static override properties = {
    groups: { attribute: false },
    activeId: { type: Number, attribute: 'active-id' },
    compact: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
        padding: var(--space-2);
        border-radius: var(--radius-l);
        background: var(--surface-1);
        box-shadow:
          var(--ring-card),
          var(--elev-1);
      }
      ul {
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .head,
      a.row {
        display: grid;
        grid-template-columns: minmax(12rem, 2.2fr) repeat(2, minmax(5rem, 0.7fr)) minmax(5rem, 0.7fr) minmax(10rem, 1.3fr) minmax(5rem, 0.7fr);
        gap: var(--space-4);
        align-items: center;
        padding-inline: var(--space-4);
      }
      .head {
        padding-block: var(--space-2);
        color: var(--text-2);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      .num {
        text-align: end;
        font-variant-numeric: tabular-nums;
      }
      a.row {
        min-block-size: var(--row-h);
        padding-block: var(--space-3);
        border-radius: var(--radius-m);
        color: var(--text-1);
        text-decoration: none;
        transition:
          background var(--dur-1) var(--ease),
          box-shadow var(--dur-1) var(--ease);
      }
      a.row:hover {
        background: var(--surface-2);
      }
      a.row[aria-current='page'] {
        background: var(--accent-soft);
        box-shadow: inset 3px 0 0 var(--accent);
      }
      .name {
        display: flex;
        flex-direction: column;
        gap: 0.1rem;
        min-inline-size: 0;
      }
      .title {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        font-weight: var(--fw-semibold);
      }
      .title span:first-child {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .desc,
      .meta {
        overflow: hidden;
        color: var(--text-2);
        font-size: var(--fs-s);
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .dash {
        color: var(--text-3);
      }
      @media (max-width: 48rem) {
        .head,
        a.row {
          grid-template-columns: minmax(0, 1fr) auto;
        }
        .head > :nth-child(n + 3),
        a.row > :nth-child(n + 3) {
          display: none;
        }
      }
      :host([compact]) .head {
        display: none;
      }
      :host([compact]) a.row {
        grid-template-columns: minmax(0, 1fr) auto;
        gap: var(--space-3);
      }
      :host([compact]) .cell {
        display: none;
      }
    `,
  ];

  declare groups: Group[];
  declare activeId: number;
  declare compact: boolean;

  constructor() {
    super();
    this.groups = [];
    this.activeId = 0;
    this.compact = false;
    new I18nController(this);
  }

  #count(value: number | null) {
    return value === null ? html`<span class="dash" aria-label=${t('groups.list.unknown')}>–</span>` : formatNumber(value);
  }

  #acl(group: Group) {
    const status = aclStatus(group);
    return status
      ? html`<lcm-status level=${status.level} label=${t(status.key, status.params)}></lcm-status>`
      : html`<span class="dash">–</span>`;
  }

  #aclDot(group: Group) {
    const status = aclStatus(group);
    return status ? html`<lcm-ampel level=${status.level} label=${t(status.key, status.params)}></lcm-ampel>` : nothing;
  }

  #meta(group: Group): string {
    return t('groups.list.meta', { servers: group.server_count, priority: group.priority });
  }

  #row(group: Group) {
    return html`<li>
      <a class="row" href=${groupPath(group.id)} aria-current=${group.id === this.activeId ? 'page' : nothing}>
        <span class="name">
          <span class="title"><span>${group.name}</span>${group.is_system ? html`<lcm-badge>${t('groups.system')}</lcm-badge>` : nothing}</span>
          ${this.compact ? html`<span class="meta">${this.#meta(group)}</span>` : group.description ? html`<span class="desc">${group.description}</span>` : nothing}
        </span>
        <span class="cell num">${this.#count(group.server_count)}</span>
        <span class="cell num">${this.#count(group.manager_count)}</span>
        <span class="cell num">${group.priority}</span>
        <span class="cell">${this.#acl(group)}</span>
        <span class="cell num">${this.#count(group.schedule_count)}</span>
        ${this.compact ? this.#aclDot(group) : nothing}
      </a>
    </li>`;
  }

  override render() {
    return html`
      <div class="head" aria-hidden="true">
        <span>${t('groups.list.name')}</span>
        <span class="num">${t('groups.list.servers')}</span>
        <span class="num">${t('groups.list.managers')}</span>
        <span class="num">${t('groups.list.priority')}</span>
        <span>${t('groups.list.acl')}</span>
        <span class="num">${t('groups.list.schedules')}</span>
      </div>
      <ul aria-label=${t('groups.list.label')}>
        ${this.groups.map((group) => this.#row(group))}
      </ul>
    `;
  }
}
customElements.define('lcm-groups-list', LcmGroupsList);
