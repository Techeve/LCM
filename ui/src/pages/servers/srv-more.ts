import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-menu';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import type { Impact } from '../../components/lcm-dialog';
import type { MenuItem } from '../../components/lcm-menu';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { isApiDevice, refreshServer, serverPath, serversApi, type ServerItem, type StartedJob } from '../../api/servers';
import { detailApi, unrestrictCommands } from '../../api/server-detail';
import { copyWithToast } from '../../lib/clipboard';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';
import { router } from '../../lib/router';
import type { ActionPreviewData } from '../../api/servers';
import type { ConfigPreviewAction } from '../../api/servers-previews';
import { effectImpacts } from './preview';
import './preview-panel';


interface MoreAction extends MenuItem {
  run: () => Promise<unknown>;
  /** Bestätigung mit Wirkung; ohne läuft die Aktion sofort. */
  impacts?: Impact[];
  /** Erklärung unter der Wirkung, wenn ein Satz nicht reicht. */
  detail?: TemplateResult;
  /** Befehlsvorschau aus dem Backend; ersetzt Wirkung und Umkehrbarkeit von Hand. */
  preview?: ConfigPreviewAction;
  danger?: boolean;
}

/**
 * `<lcm-srv-more .server>`: die selteneren Server-Aktionen der alten Oberfläche in einem Menü – alles neu erfassen,
 * SSH härten oder zurücknehmen, Schlüssel erneuern bzw. Agent-Token neu erzeugen, Benutzer abgleichen, Neustart und
 * die Anleitung, volle Rechte wiederherzustellen. Eingriffe fragen mit ihrer Wirkung nach.
 */
export class SrvMore extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    server: { attribute: false },
    onRemove: { attribute: false },
    _pending: { state: true },
    _busy: { state: true },
    _preview: { state: true },
    _token: { state: true },
    _guide: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
      }
      .detail {
        margin-block-start: var(--space-3);
      }
      .detail p {
        margin: 0 0 var(--space-3);
        font-size: var(--fs-s);
        line-height: 1.5;
      }
      pre {
        margin: var(--space-3) 0;
        padding: var(--space-3);
        overflow: auto;
        border-radius: var(--radius-s);
        background: var(--surface-1);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.6;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
      p {
        margin: 0 0 var(--space-2);
      }
    `,
  ];

  declare server: ServerItem;
  /** Öffnet die Bestätigung zum Entfernen (der Dialog gehört dem Serverdetail). */
  declare onRemove: (() => void) | undefined;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _pending: MoreAction | null;
  declare _preview: ActionPreviewData | undefined;
  declare _busy: boolean;
  declare _token: string;
  declare _guide: boolean;

  constructor() {
    super();
    this._dialogError = '';
    this.onRemove = undefined;
    this._pending = null;
    this._busy = false;
    this._token = '';
    this._guide = false;
    new I18nController(this);
  }

  #impacts(...extra: Impact[]): Impact[] {
    return [{ label: t('servers.confirm.server'), value: this.server.name }, ...extra];
  }

  /** Was beim Einschränken gilt, wie das alte Fenster es sagte: erlaubt, gesperrt, Grenze des Schutzes, Rückweg. */
  #restrictDetail(): TemplateResult {
    const paragraph = (label: string, text: string) => html`<p><strong>${label}:</strong> ${text}</p>`;
    return html`<div class="detail">
      ${paragraph(t('serverDetail.more.restrictInfo.allowedLabel'), t('serverDetail.more.restrictInfo.allowed'))}
      ${paragraph(t('serverDetail.more.restrictInfo.blockedLabel'), t('serverDetail.more.restrictInfo.blocked'))}
      ${paragraph(t('serverDetail.more.restrictInfo.scopeLabel'), t('serverDetail.more.restrictInfo.scope'))}
      ${paragraph(t('serverDetail.more.restrictInfo.irreversibleLabel'), t('serverDetail.more.restrictInfo.irreversible'))}
    </div>`;
  }

  #actions(): MoreAction[] {
    const s = this.server;
    const device = isApiDevice(s);
    const agent = s.transport === 'agent';
    const reversible = (yes: boolean): Impact => ({ label: t('servers.confirm.reversible'), value: t(yes ? 'servers.confirm.yes' : 'servers.confirm.no'), tone: yes ? 'ok' : 'warn' });
    // Reihenfolge und Umfang wie das Aktionen-Menü der bisherigen Oberfläche. Einträge mit Bestätigung erklären sich
    // dort; das Menü selbst zeigt nur die Namen.
    const actions: Array<MoreAction | null> = [
      !device && !s.proxmox_type && !s.user_sync_disabled
        ? { id: 'sync-users', label: t('serverDetail.more.syncUsers.label'), tip: t('serverDetail.more.tips.syncUsers'), icon: 'users', run: () => this.#done(t('serverDetail.more.syncUsers.done', { name: s.name }), detailApi.syncUsers(s.id)) }
        : null,
      agent
        ? {
            id: 'agent-token',
            label: t('serverDetail.more.agentToken.label'),
            icon: 'key',
            impacts: this.#impacts({ label: t('serverDetail.more.effect'), value: t('serverDetail.more.agentToken.effect'), tone: 'warn' }, reversible(false)),
            run: async () => (this._token = await detailApi.regenerateAgentToken(s.id)),
          }
        : null,
      !device && !agent
        ? {
            id: 'rotate-key',
            label: t('serverDetail.more.rotateKey.label'),
            tip: t('serverDetail.more.tips.rotateKey'),
            icon: 'key',
            impacts: this.#impacts({ label: t('serverDetail.more.effect'), value: t('serverDetail.more.rotateKey.effect') }, reversible(false)),
            run: () => this.#done(t('serverDetail.more.rotateKey.done', { name: s.name }), detailApi.rotateKey(s.id)),
          }
        : null,
      !agent ? { id: 'reconnect', label: t('servers.bar.reconnect.label'), tip: t('serverDetail.more.tips.reconnect'), icon: 'link', run: async () => void router.navigate(`/servers/join?reconnect=${s.id}`) } : null,
      !device ? { id: 'dns-test', label: t('serverDetail.more.dnsTest'), tip: t('serverDetail.more.tips.dnsTest'), icon: 'search', run: () => this.#done(t('serverDetail.more.dnsTestDone', { name: s.name }), detailApi.dnsTest(s.id)) } : null,
      !device && !s.restricted_sudo
        ? { id: 'security-tools', label: t('serverDetail.more.securityTools'), icon: 'shield', run: async () => void router.navigate(`${serverPath(s.id, 'security')}?modal=security-tool`) }
        : null,
      !device && !s.restricted_sudo
        ? {
            id: 'reboot',
            label: t('servers.reboot.label'),
            icon: 'power',
            impacts: this.#impacts({ label: t('serverDetail.more.downtime'), value: t('serverDetail.more.reboot.downtime'), tone: 'warn' }, reversible(false)),
            run: () => this.#job(t('servers.reboot.label'), serversApi.reboot(s.id)),
          }
        : null,
      !device && !s.restricted_sudo
        ? {
            id: 'restrict',
            label: t('serverDetail.more.restrict'),
            icon: 'lock',
            hint: t('serverDetail.more.restrictInfo.intro'),
            impacts: this.#impacts({ label: t('serverDetail.more.effect'), value: t('servers.bar.sudo.meaning') }, reversible(false)),
            detail: this.#restrictDetail(),
            run: () => this.#done(t('servers.bar.sudo.done', { name: s.name }), serversApi.restrictSudo(s.id)),
          }
        : null,
      !device && s.restricted_sudo ? { id: 'unrestrict', label: t('serverDetail.more.unrestrict.label'), icon: 'lock', run: async () => (this._guide = true) } : null,
      this.onRemove ? { id: 'remove', label: t('servers.bar.remove.label'), icon: 'trash', danger: true, separatorBefore: true, run: async () => this.onRemove?.() } : null,
    ];
    return actions.filter((action): action is MoreAction => action !== null);
  }

  async #job(label: string, request: Promise<StartedJob>): Promise<void> {
    const job = await request;
    announceJob(label, this.server.name, job.job_id);
  }

  async #done(message: string, request: Promise<unknown>): Promise<void> {
    await request;
    toast({ tone: 'ok', title: message });
  }

  async #run(action: MoreAction): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await action.run();
      this._pending = null;
      refreshServer(this.server.id);
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #select(id: string): void {
    const action = this.#actions().find((a) => a.id === id);
    if (!action) return;
    this._preview = undefined;
    if (action.impacts) this._pending = action;
    else void this.#run(action);
  }

  async #copyToken(): Promise<void> {
    await copyWithToast(this._token, { copied: t('serverDetail.more.agentToken.copied'), failed: t('serverDetail.facts.copyFailed') });
  }

  override render() {
    const actions = this.#actions();
    const pending = this._pending;
    const items: MenuItem[] = actions.map(({ id, label, tip, icon, danger, separatorBefore }) => ({ id, label, tip, icon, danger, separatorBefore }));
    return html`<lcm-menu variant="button" label=${t('serverDetail.more.label')} text=${t('serverDetail.more.label')} .items=${items} @lcm-select=${(e: CustomEvent<{ id: string }>) => this.#select(e.detail.id)}></lcm-menu>
      <lcm-dialog
        ?open=${pending !== null}
        tone=${pending?.danger ? 'danger' : 'default'}
        heading=${pending ? t('serverDetail.more.confirmHeading', { label: pending.label, name: this.server.name }) : ''}
        message=${pending?.hint ?? ''}
        confirm-label=${pending?.label ?? ''}
        cancel-label=${t('servers.confirm.cancel')}
        .impacts=${[...(pending?.impacts ?? []), ...(pending?.preview ? effectImpacts(this._preview) : [])]}
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._pending = null)}
        @lcm-confirm=${() => pending && void this.#run(pending)}
      >
        ${pending?.detail ?? nothing}
        ${pending?.preview
          ? html`<lcm-preview-panel
              server-id=${this.server.id}
              action=${pending.preview}
              @lcm-preview=${(e: CustomEvent<ActionPreviewData>) => (this._preview = e.detail)}
            ></lcm-preview-panel>`
          : nothing}
      </lcm-dialog>
      <lcm-modal ?open=${this._token !== ''} size="m" heading=${t('serverDetail.more.agentToken.title')} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => (this._token = '')}>
        <lcm-notice tone="warn">${t('serverDetail.more.agentToken.once')}</lcm-notice>
        <pre>${this._token}</pre>
        <p>${t('serverDetail.more.agentToken.enroll')}</p>
        <pre>${t('serverDetail.more.agentToken.command', { token: this._token })}</pre>
        <lcm-button slot="footer" variant="ghost" icon="copy" @click=${() => void this.#copyToken()}>${t('serverDetail.more.agentToken.copy')}</lcm-button>
        <lcm-button slot="footer" variant="primary" @click=${() => (this._token = '')}>${t('serverDetail.more.close')}</lcm-button>
      </lcm-modal>
      <lcm-modal ?open=${this._guide} size="l" heading=${t('serverDetail.more.unrestrict.title')} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => (this._guide = false)}>
        <p>${t('serverDetail.more.unrestrict.intro')}</p>
        <pre>${unrestrictCommands(this.server.service_user)}</pre>
        <lcm-notice tone="warn">${t('serverDetail.more.unrestrict.warn')}</lcm-notice>
        <p>${t('serverDetail.more.unrestrict.after')}</p>
        <lcm-button slot="footer" variant="ghost" @click=${() => (this._guide = false)}>${t('serverDetail.more.close')}</lcm-button>
        ${this.server.transport !== 'agent'
          ? html`<lcm-button slot="footer" variant="primary" icon="refresh" href=${`/servers/join?reconnect=${this.server.id}`}>${t('servers.bar.reconnect.label')}</lcm-button>`
          : nothing}
      </lcm-modal>`;
  }
}
customElements.define('lcm-srv-more', SrvMore);
