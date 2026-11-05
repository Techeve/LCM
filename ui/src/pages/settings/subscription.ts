import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-save-bar';
import '../../components/lcm-server-action';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import { toast } from '../../components/lcm-toaster';
import type { Impact } from '../../components/lcm-dialog';
import { baseStyles } from '../../components/shared';
import { APT_CHANNELS, SUBSCRIPTION_URL, enterpriseAllowed, readSubscription, settingsApi, type AptChannel, type Subscription } from '../../api/settings';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { jobLink } from '../../lib/job-link';
import { LIMITS, isHttpUrl } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { saveBarLabels } from '../../lib/save-bar';
import './system/copy-field';
import { helpLabels } from './system/help';
import { watchJob } from '../../lib/job-watch';
import { settingsStyles } from './system/settings-styles';
import { subscriptionState } from './system/subscription-model';

type Confirm = { kind: 'remove' } | { kind: 'channel'; channel: AptChannel };

const CHANNEL_TEXT: Record<AptChannel, { label: string; hint: string; job: string; confirm: string }> = {
  community: { label: 'settings.subscription.apt.community.label', hint: 'settings.subscription.apt.community.hint', job: 'settings.subscription.apt.community.job', confirm: 'settings.subscription.apt.confirm.community' },
  beta: { label: 'settings.subscription.apt.beta.label', hint: 'settings.subscription.apt.beta.hint', job: 'settings.subscription.apt.beta.job', confirm: 'settings.subscription.apt.confirm.beta' },
  enterprise: { label: 'settings.subscription.apt.enterprise.label', hint: 'settings.subscription.apt.enterprise.hint', job: 'settings.subscription.apt.enterprise.job', confirm: 'settings.subscription.apt.confirm.enterprise' },
};

/** Einstellungen → Subscription: Vertragsstand, Aktivierung und Paketkanal des LCM-Hosts. */
export class SettingsSubscription extends LitElement {
  static override properties = {
    _key: { state: true },
    _url: { state: true },
    _showUrl: { state: true },
    _picked: { state: true },
    _busy: { state: true },
    _confirm: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      .state {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
        margin-block-end: var(--space-4);
      }
      .choices {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        border: 0;
      }
      .choice {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: var(--space-3);
        align-items: start;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        cursor: pointer;
        transition: box-shadow var(--dur-1) var(--ease);
      }
      .choice:hover {
        box-shadow: inset 0 0 0 1px var(--line-strong);
      }
      .choice.selected {
        background: var(--accent-soft);
        box-shadow: inset 0 0 0 2px var(--accent);
      }
      .choice.locked {
        opacity: 0.55;
        cursor: not-allowed;
      }
      .choice:focus-within {
        outline: 2px solid var(--focus-ring);
        outline-offset: 2px;
      }
      .choice input {
        margin: 0.25rem 0 0;
        accent-color: var(--accent);
      }
      .choice .name {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
        font-weight: var(--fw-semibold);
      }
      .choice .hint {
        display: block;
        margin-block-start: var(--space-1);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare _key: string;
  declare _url: string;
  declare _showUrl: boolean;
  declare _picked: AptChannel | undefined;
  declare _busy: boolean;
  declare _confirm: Confirm | undefined;
  declare _error: string;

  readonly #data = new LiveData<Subscription>(this, { url: () => SUBSCRIPTION_URL, transform: readSubscription });
  #stopWatching: Array<() => void> = [];

  constructor() {
    super();
    this._key = '';
    this._url = '';
    this._showUrl = false;
    this._picked = undefined;
    this._busy = false;
    this._confirm = undefined;
    this._error = '';
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopWatching.forEach((stop) => stop());
    this.#stopWatching = [];
  }

  /** Die Auswahl folgt dem gemeldeten Kanal, bis der Benutzer selbst wählt. */
  get #picked(): AptChannel {
    return this._picked ?? this.#data.data?.aptChannel ?? 'community';
  }

  /** Führt eine Aktion aus; ein Fehler steht dort, wo sie ausgelöst wurde (Dialog bzw. Leiste der Aktivierung). */
  async #run(action: () => Promise<void>): Promise<boolean> {
    this._busy = true;
    this._error = '';
    try {
      await action();
      return true;
    } catch (error) {
      this._error = errorText(error);
      return false;
    } finally {
      this._busy = false;
    }
  }

  /** Fehlerschlüssel der Dienst-Adresse; leer, solange das Feld fehlt oder passt. */
  #urlError(): string {
    const url = this._url.trim();
    return this._showUrl && url !== '' && !isHttpUrl(url) ? 'check.url' : '';
  }

  async #activate(): Promise<void> {
    await this.#run(async () => {
      await settingsApi.activateSubscription(this._key.trim(), this._showUrl ? this._url.trim() : '');
      this._key = '';
      toast({ tone: 'ok', title: t('settings.subscription.activated') });
    });
  }

  /** Prüfen ist eine Aktion ohne Eingabe: das Ergebnis, auch ein Fehler, kommt als Toast. */
  async #verify(): Promise<void> {
    this._busy = true;
    try {
      const status = await settingsApi.verifySubscription();
      if (status.lastError) toast({ tone: 'warn', title: t('settings.subscription.verifyFailed'), message: status.lastError });
      else toast({ tone: 'ok', title: t('settings.subscription.verified') });
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.subscription.verifyFailed'), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  async #remove(): Promise<void> {
    const done = await this.#run(async () => {
      await settingsApi.removeSubscription();
      toast({ tone: 'ok', title: t('settings.subscription.removed') });
    });
    if (done) this._confirm = undefined;
  }

  #closeDialog(): void {
    this._confirm = undefined;
    this._error = '';
  }

  async #switch(channel: AptChannel): Promise<void> {
    const label = t(CHANNEL_TEXT[channel].job);
    const done = await this.#run(async () => {
      const { jobId } = await settingsApi.switchAptChannel(channel);
      toast({ tone: 'info', title: t('settings.subscription.apt.started', { what: label }) });
      this.#stopWatching.push(
        watchJob(jobId, (status) => {
          if (status === 'success') toast({ tone: 'ok', title: t('settings.subscription.apt.done', { what: label }), background: true, ...jobLink(jobId) });
          else toast({ tone: 'crit', title: t('settings.subscription.apt.failed', { what: label }), message: t('settings.subscription.apt.failedHint'), ...jobLink(jobId) });
          this._picked = undefined;
        }),
      );
    });
    if (done) this._confirm = undefined;
  }

  #askChannel(channel: AptChannel): void {
    const active = this.#data.data?.aptChannel;
    // Dieselbe Einrichtung noch einmal anzuwenden ändert nichts, was zu bestätigen wäre.
    if (channel === active) void this.#switch(channel);
    else this._confirm = { kind: 'channel', channel };
  }

  #stateRow(subscription: Subscription) {
    const state = subscriptionState(subscription);
    return html`<div class="state">
      <lcm-status level=${state.level} label=${t(state.key, { days: subscription.daysLeft ?? 0 })}></lcm-status>
    </div>`;
  }

  #facts(subscription: Subscription) {
    const expires = subscription.expiresAt
      ? subscription.daysLeft !== null
        ? `${subscription.expiresAt} (${t('settings.subscription.daysLeft', { days: subscription.daysLeft })})`
        : subscription.expiresAt
      : t('settings.subscription.unlimited');
    return html`<dl class="facts">
      <dt>${t('settings.subscription.customer')}</dt>
      <dd>${subscription.customer || '–'}</dd>
      <dt>${t('settings.subscription.key')}</dt>
      <dd class="mono">${subscription.keyPrefix || '–'}</dd>
      <dt>${t('settings.subscription.expires')}</dt>
      <dd>${expires}</dd>
      ${subscription.includedServers > 0
        ? html`<dt>${t('settings.subscription.servers')}</dt>
            <dd class=${subscription.quotaExceeded ? 'warn' : ''}>${t('settings.subscription.serversValue', { included: subscription.includedServers, managed: subscription.managedServers })}</dd>`
        : nothing}
      <dt>${t('settings.subscription.lastCheck')}</dt>
      <dd>${formatDateTime(subscription.lastCheckAt)}</dd>
      ${subscription.lastError ? html`<dt>${t('settings.subscription.lastError')}</dt><dd class="crit">${subscription.lastError}</dd>` : nothing}
    </dl>`;
  }

  #activation() {
    return html`<lcm-card level="2" heading=${t('settings.subscription.activate.title')} subtitle=${t('settings.subscription.activate.subtitle')}>
      <div class="fields">
        <div class="row">
          <lcm-input
            class="grow"
            label=${t('settings.subscription.activate.key')}
            width="l"
            maxlength=${LIMITS.token}
            placeholder=${'LCM-E-XXXXX-XXXXX-XXXXX-XXXXX'}
            mono
            .value=${this._key}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._key = e.detail.value)}
          ></lcm-input>
        </div>
        ${this._showUrl
          ? html`<lcm-input
              label=${t('settings.subscription.activate.url')}
              width="l"
              hint=${t('settings.subscription.activate.urlHint')}
              maxlength=${LIMITS.path}
              error=${this.#urlError() ? t(this.#urlError()) : ''}
              placeholder=${'https://repo.techeve.de'}
              mono
              .value=${this._url}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._url = e.detail.value)}
            ></lcm-input>`
          : nothing}
        ${this._showUrl
          ? nothing
          : html`<div class="row"><lcm-button variant="ghost" size="small" @click=${() => (this._showUrl = true)}>${t('settings.subscription.activate.advanced')}</lcm-button></div>`}
        <lcm-save-bar
          always
          ?dirty=${this._key.trim() !== '' && this.#urlError() === ''}
          ?busy=${this._busy}
          error=${this._confirm ? '' : this._error}
          .labels=${{ ...saveBarLabels(t('settings.subscription.activate.action'), ''), unsaved: '' }}
          @lcm-save=${() => void this.#activate()}
        ></lcm-save-bar>
      </div>
    </lcm-card>`;
  }

  #status(subscription: Subscription) {
    return html`<lcm-card level="2" heading=${t('settings.subscription.status.title')}>
      ${this.#stateRow(subscription)}
      ${subscription.quotaExceeded
        ? html`<lcm-notice tone="warn" class="spaced-bottom">${t('settings.subscription.quota', { managed: subscription.managedServers, included: subscription.includedServers })}</lcm-notice>`
        : nothing}
      ${this.#facts(subscription)}
      <div class="row spaced-top">
        <lcm-server-action
          label=${t('settings.subscription.verify.label')}
          icon="refresh"
          help=${t('settings.subscription.verify.help')}
          meaning=${t('settings.subscription.verify.meaning')}
          .labels=${helpLabels()}
          ?busy=${this._busy}
          @click=${() => void this.#verify()}
        ></lcm-server-action>
        <lcm-button variant="danger" icon="trash" ?disabled=${this._busy} @click=${() => (this._confirm = { kind: 'remove' })}>${t('settings.subscription.remove.label')}</lcm-button>
      </div>
    </lcm-card>`;
  }

  #channels(subscription: Subscription) {
    const allowed = enterpriseAllowed(subscription);
    const picked = this.#picked;
    const same = picked === subscription.aptChannel;
    return html`<lcm-card level="2" heading=${t('settings.subscription.apt.title')} subtitle=${t('settings.subscription.apt.subtitle')}>
      ${subscription.aptAvailable
        ? html`<div class="fields">
            <fieldset class="choices" role="radiogroup" aria-label=${t('settings.subscription.apt.title')}>
              ${APT_CHANNELS.map((channel) => {
                const locked = channel === 'enterprise' && !allowed;
                return html`<label class=${`choice${picked === channel ? ' selected' : ''}${locked ? ' locked' : ''}`}>
                  <input type="radio" name="channel" .checked=${picked === channel} ?disabled=${this._busy || locked} @change=${() => (this._picked = channel)} />
                  <span>
                    <span class="name">${t(CHANNEL_TEXT[channel].label)}${channel === subscription.aptChannel ? html`<lcm-badge tone="ok">${t('settings.subscription.apt.active')}</lcm-badge>` : nothing}</span>
                    <span class="hint">${t(CHANNEL_TEXT[channel].hint)}</span>
                  </span>
                </label>`;
              })}
            </fieldset>
            ${allowed ? nothing : html`<p class="muted">${subscription.configured ? t('settings.subscription.apt.needsActive') : t('settings.subscription.apt.needsContract')}</p>`}
            <div class="row">
              <lcm-server-action
                label=${same ? t('settings.subscription.apt.reapply') : t('settings.subscription.apt.apply', { channel: t(CHANNEL_TEXT[picked].label) })}
                variant=${same ? 'secondary' : 'primary'}
                icon="package"
                help=${same ? t('settings.subscription.apt.reapplyHelp') : t('settings.subscription.apt.applyHelp')}
                meaning=${t('settings.subscription.apt.meaning')}
                .labels=${helpLabels()}
                ?busy=${this._busy}
                ?disabled=${picked === 'enterprise' && !allowed}
                @click=${() => this.#askChannel(picked)}
              ></lcm-server-action>
            </div>
          </div>`
        : html`<lcm-notice>${t('settings.subscription.apt.unavailable')}</lcm-notice>`}
    </lcm-card>`;
  }

  #instance(subscription: Subscription) {
    return html`<lcm-card level="2" heading=${t('settings.subscription.instance.title')} subtitle=${t('settings.subscription.instance.subtitle')}>
      <lcm-copy-field value=${subscription.instanceId}></lcm-copy-field>
    </lcm-card>`;
  }

  #dialog(subscription: Subscription) {
    const confirm = this._confirm;
    if (confirm?.kind === 'remove') {
      const impacts: Impact[] = [
        { label: t('settings.subscription.remove.impactAccess'), value: t('settings.subscription.remove.impactAccessValue'), tone: subscription.aptChannel === 'enterprise' ? 'warn' : undefined },
        { label: t('settings.subscription.remove.impactBinding'), value: t('settings.subscription.remove.impactBindingValue') },
        { label: t('settings.subscription.remove.impactServers'), value: t('settings.subscription.remove.impactServersValue'), tone: 'ok' },
      ];
      return html`<lcm-dialog
        open
        tone="danger"
        heading=${t('settings.subscription.remove.title')}
        message=${t('settings.subscription.remove.message')}
        .impacts=${impacts}
        confirm-label=${t('settings.subscription.remove.confirm')}
        cancel-label=${t('settings.form.cancel')}
        ?busy=${this._busy}
        error=${this._error}
        @lcm-confirm=${() => void this.#remove()}
        @lcm-cancel=${() => this.#closeDialog()}
      ></lcm-dialog>`;
    }
    if (confirm?.kind === 'channel') {
      const impacts: Impact[] = [
        { label: t('settings.subscription.apt.impactTarget'), value: t('settings.subscription.apt.impactTargetValue') },
        { label: t('settings.subscription.apt.impactDowntime'), value: t('settings.subscription.apt.impactDowntimeValue'), tone: 'ok' },
        { label: t('settings.subscription.apt.impactReversible'), value: t('settings.subscription.apt.impactReversibleValue'), tone: 'ok' },
      ];
      const channel = confirm.channel;
      return html`<lcm-dialog
        open
        heading=${t('settings.subscription.apt.confirmTitle', { channel: t(CHANNEL_TEXT[channel].label) })}
        message=${t(CHANNEL_TEXT[channel].confirm)}
        .impacts=${impacts}
        confirm-label=${t('settings.subscription.apt.confirmAction')}
        cancel-label=${t('settings.form.cancel')}
        ?busy=${this._busy}
        error=${this._error}
        @lcm-confirm=${() => void this.#switch(channel)}
        @lcm-cancel=${() => this.#closeDialog()}
      ></lcm-dialog>`;
    }
    return nothing;
  }

  override render() {
    const subscription = this.#data.data;
    if (!subscription) {
      if (this.#data.error) {
        return html`<lcm-empty-state level="2" icon="warning" heading=${t('settings.form.loadFailed')} text=${this.#data.error}>
          <lcm-button slot="action" @click=${() => this.#data.reload()}>${t('settings.form.retry')}</lcm-button>
        </lcm-empty-state>`;
      }
      return html`<lcm-skeleton shape="block" label=${t('settings.form.loading')}></lcm-skeleton>`;
    }
    return html`<div class="stack column">
      ${subscription.configured ? this.#status(subscription) : this.#activation()} ${this.#channels(subscription)} ${this.#instance(subscription)}
    </div>
    ${this.#dialog(subscription)}`;
  }
}
customElements.define('lcm-settings-subscription', SettingsSubscription);
