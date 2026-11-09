import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-modal';
import '../../components/lcm-skeleton';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { loginSeconds, loginsUrl, normaliseLogins, type LoginItem } from '../../api/servers-ops';
import { I18nController } from '../../lib/controllers';
import { formatDateTime, formatDuration } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { plainLabels } from './srv-tab';

/** `<lcm-ops-user-logins server-id username>`: die Anmeldungen eines Kontos (aus wtmp bzw. LCMs Protokoll) im Fenster. Offen, solange `username` gesetzt ist. */
export class OpsUserLogins extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    username: { type: String },
    fromLcm: { type: Boolean, attribute: 'from-lcm' },
  };
  static override styles = [
    baseStyles,
    css`
      p {
        margin: var(--space-3) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare serverId: number;
  declare username: string;
  declare fromLcm: boolean;

  readonly #logins = new LiveData<LoginItem[]>(this, { url: () => (this.username ? loginsUrl(this.serverId, this.username) : null), transform: normaliseLogins });

  constructor() {
    super();
    this.serverId = 0;
    this.username = '';
    this.fromLcm = false;
    new I18nController(this);
  }

  #close(): void {
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  get #columns(): Column<LoginItem>[] {
    return [
      { key: 'startedAt', label: t('serverOps.users.logins.when'), render: (r) => formatDateTime(r.startedAt) },
      { key: 'from', label: t('serverOps.users.logins.from'), kind: 'mono', render: (r) => r.from || t('serverOps.users.logins.local') },
      { key: 'tty', label: t('serverOps.users.logins.tty'), kind: 'mono' },
      { key: 'duration', label: t('serverOps.users.logins.duration'), render: (r) => this.#duration(r) },
    ];
  }

  #duration(login: LoginItem) {
    if (login.active) return html`<lcm-badge tone="ok">${t('serverOps.users.logins.active')}</lcm-badge>`;
    const seconds = loginSeconds(login);
    return seconds === null ? '–' : formatDuration(seconds);
  }

  #body() {
    const { data, error } = this.#logins;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="4" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    const caption = t('serverOps.users.logins.title', { name: this.username });
    return html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${data} .labels=${plainLabels(caption, t('serverOps.users.logins.empty'))}></lcm-data-table>
      <p>${this.fromLcm ? t('serverOps.users.logins.sourceLcm') : t('serverOps.users.logins.sourceWtmp')}</p>`;
  }

  override render() {
    return html`<lcm-modal ?open=${this.username !== ''} size="m" heading=${t('serverOps.users.logins.title', { name: this.username })} close-label=${t('servers.confirm.cancel')} @lcm-close=${(e: Event) => {
      e.stopPropagation();
      this.#close();
    }}>
      ${this.username ? this.#body() : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('serverOps.close')}</lcm-button>
    </lcm-modal>`;
  }
}
customElements.define('lcm-ops-user-logins', OpsUserLogins);
