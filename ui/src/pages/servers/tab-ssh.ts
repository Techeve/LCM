import { html } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { normaliseSshSessions, sshSessionsUrl, type SshRow } from '../../api/server-data';
import { formatDateTime, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

/** SSH-Sitzungen: die aufgezeichneten Verbindungen von LCM zu diesem Server (Zweck, Benutzer, Befehle). */
export class SrvSsh extends SrvTab {
  static override styles = [baseStyles, tabStyles];

  readonly #sessions = new LiveData<SshRow[]>(this, { url: () => sshSessionsUrl(this.server.id), topics: () => [`server:${this.server.id}`, 'jobs'], transform: normaliseSshSessions });

  get #columns(): Column<SshRow>[] {
    return [
      { key: 'purpose', label: t('servers.ssh.column.purpose'), render: (r) => html`<strong>${r.purpose}</strong>` },
      { key: 'actor', label: t('servers.ssh.column.actor') },
      { key: 'user', label: t('servers.ssh.column.user'), kind: 'mono' },
      { key: 'opened', label: t('servers.ssh.column.opened'), render: (r) => (r.openedAt ? html`<span title=${formatDateTime(r.openedAt)}>${formatRelative(r.openedAt)}</span>` : '') },
      { key: 'commands', label: t('servers.ssh.column.commands'), align: 'end', render: (r) => String(r.commands) },
      { key: 'error', label: t('servers.ssh.error'), labelHidden: true, render: (r) => (r.hadError ? html`<lcm-badge tone="crit">${t('servers.ssh.error')}</lcm-badge>` : '') },
    ];
  }

  override render() {
    return whenLoaded(this.#sessions, (sessions) =>
      sessions.length === 0
        ? html`<lcm-empty-state level="3" icon="terminal" heading=${t('servers.ssh.empty.title')} text=${t('servers.ssh.empty.text')}></lcm-empty-state>`
        : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${sessions} .labels=${plainLabels(t('servers.ssh.caption'), t('servers.ssh.empty.title'))}></lcm-data-table>`,
    );
  }
}
customElements.define('lcm-srv-ssh', SrvSsh);
