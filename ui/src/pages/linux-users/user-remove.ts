import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import { baseStyles, emit } from '../../components/shared';
import { linuxUsersApi, normalizeDeprovision, type DeprovisionResult, type LinuxUser, type UserServer } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { buildImpacts } from './impacts';
import { sectionStyles } from './lu-styles';
import './lu-action';
import { isProvisioned } from './user-model';

/**
 * <lcm-lu-user-remove .user=${…} .servers=${…}> – Benutzer entfernen: erst aktiv von allen Servern (Konto samt
 * Home-Verzeichnis dort löschen), dann aus LCM löschen. Beides verlangt den getippten Benutzernamen.
 * Meldet `lcm-deleted`, wenn der Benutzer weg ist.
 */
export class LcmLuUserRemove extends LitElement {
  static override properties = {
    user: { attribute: false },
    servers: { attribute: false },
    _report: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .report {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
        font-size: var(--fs-s);
      }
      .report li {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
    `,
  ];

  declare user: LinuxUser;
  declare servers: UserServer[];
  declare _report: DeprovisionResult[] | null;

  constructor() {
    super();
    this.servers = [];
    this._report = null;
    new I18nController(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('user') && (changed.get('user') as LinuxUser | undefined)?.id !== this.user.id) this._report = null;
  }

  async #removeFromServers(): Promise<DeprovisionResult[]> {
    return normalizeDeprovision(await linuxUsersApi.removeFromServers(this.user.id));
  }

  async #delete(): Promise<void> {
    await linuxUsersApi.remove(this.user.id);
    emit(this, 'lcm-deleted');
  }

  #reportList(report: DeprovisionResult[]) {
    if (report.length === 0) return html`<p class="note">${t('linuxUsers.remove.reportNone')}</p>`;
    return html`<ul class="report" aria-label=${t('linuxUsers.remove.report')}>
      ${report.map(
        (r) => html`<li>
          <lcm-badge tone=${r.ok ? 'ok' : 'crit'}>${r.ok ? t('linuxUsers.remove.removed') : t('linuxUsers.remove.failed')}</lcm-badge>
          <strong>${r.server}</strong> <span>${r.message}</span>
        </li>`,
      )}
    </ul>`;
  }

  #removeFromServersAction() {
    return html`<lcm-lu-action
      label=${t('linuxUsers.remove.fromServers.label')}
      help=${t('linuxUsers.remove.fromServers.help')}
      meaning=${t('linuxUsers.remove.fromServers.meaning')}
      icon="power"
      variant="danger"
      danger
      confirm-heading=${t('linuxUsers.remove.fromServers.heading', { name: this.user.username })}
      confirm-message=${t('linuxUsers.remove.fromServers.message')}
      confirm-label=${t('linuxUsers.remove.fromServers.confirm')}
      expect=${this.user.username}
      expect-label=${t('linuxUsers.remove.expect', { name: this.user.username })}
      .impacts=${buildImpacts({ servers: this.servers, effect: t('linuxUsers.remove.fromServers.effect'), reversible: false })}
      .run=${() => this.#removeFromServers()}
      @lcm-done=${(e: CustomEvent<DeprovisionResult[]>) => (this._report = e.detail)}
    ></lcm-lu-action>`;
  }

  #deleteAction(blocked: boolean) {
    return html`<lcm-lu-action
      label=${t('linuxUsers.remove.delete.label')}
      help=${blocked ? t('linuxUsers.remove.delete.blocked') : t('linuxUsers.remove.delete.help')}
      meaning=${t('linuxUsers.remove.delete.meaning')}
      icon="trash"
      variant="danger-solid"
      danger
      ?disabled=${blocked}
      confirm-heading=${t('linuxUsers.remove.delete.heading', { name: this.user.username })}
      confirm-label=${t('linuxUsers.remove.delete.confirm')}
      expect=${this.user.username}
      expect-label=${t('linuxUsers.remove.expect', { name: this.user.username })}
      .impacts=${buildImpacts({ servers: [], effect: t('linuxUsers.remove.delete.effect'), reversible: false })}
      .run=${() => this.#delete()}
    ></lcm-lu-action>`;
  }

  override render() {
    const provisioned = isProvisioned(this.user);
    return html`<section>
      <h3>${t('linuxUsers.remove.title')}</h3>
      <p>${provisioned ? t('linuxUsers.remove.provisioned') : t('linuxUsers.remove.free')}</p>
      ${this._report ? this.#reportList(this._report) : nothing}
      <div class="actions">${provisioned ? this.#removeFromServersAction() : nothing}${this.#deleteAction(provisioned)}</div>
    </section>`;
  }
}
customElements.define('lcm-lu-user-remove', LcmLuUserRemove);
