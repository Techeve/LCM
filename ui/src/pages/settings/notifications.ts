import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import type { FieldWidth } from '../../components/field-styles';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import '../../components/lcm-textarea';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { CHANNELS_URL, normaliseChannels, settingsMoreApi, type Channel } from '../../api/settings-more';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { portForSecurity } from '../../lib/mail-security';
import { LIMITS } from '../../lib/validate';
import { CHANNEL_TYPE_KEYS, FORMAT_KEYS, MAX_RECIPIENTS, badRecipientsText, channelBody, channelFormOf, channelSummary, emptyChannelForm, validateChannelForm, type ChannelForm } from './messages/models';
import { mailSecurityField } from './mail-security-field';
import './packages/cells';
import { ModalForm, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/settings-modal';

const FIELDS: Record<string, string> = {
  'field:name': 'name',
  'field:secret': 'secret',
  'field:host': 'host',
  'field:port': 'port',
  'field:from': 'from',
  'notification.name_required': 'name',
  'notification.name_taken': 'name',
};

const isSystem = (channel: Channel) => channel.type === 'system_email';

/** Einstellungen → Benachrichtigungen: Kanäle (E-Mail, Webhook), über die Alarmregeln melden. */
export class SettingsNotifications extends LitElement {
  static override properties = { _dialogError: { state: true }, _delete: { state: true }, _busy: { state: true }, _testing: { state: true } };
  static override styles = [baseStyles, sectionStyles];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: Channel | null;
  declare _busy: boolean;
  declare _testing: number | null;
  readonly #channels = new LiveData<Channel[]>(this, { url: () => CHANNELS_URL, transform: normaliseChannels });
  readonly #modal = new ModalForm<ChannelForm>(this, (modal) => {
    if (modal.kind === 'new') return emptyChannelForm();
    if (modal.kind !== 'edit' || !this.#channels.data) return this.#channels.loading ? undefined : null;
    const channel = this.#channels.data.find((c) => c.id === modal.id);
    return channel && !isSystem(channel) ? channelFormOf(channel) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._delete = null;
    this._busy = false;
    this._testing = null;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [{ id: 'channel-new', key: 'n', label: t('settings.notifications.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  get #editing(): Channel | undefined {
    const id = this.#modal.editId;
    return id ? this.#channels.data?.find((c) => c.id === id) : undefined;
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    const id = this.#modal.editId;
    const valid = Object.keys(validateChannelForm(form, id === null)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveChannel(id, channelBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t(id ? 'settings.notifications.notice.saved' : 'settings.notifications.notice.created', { name: form.name.trim() }) });
    }
  }

  async #test(channel: Channel): Promise<void> {
    this._testing = channel.id;
    try {
      await settingsMoreApi.testChannel(channel.id);
      toast({ tone: 'ok', title: t('settings.notifications.notice.testSent', { name: channel.name }) });
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.notifications.notice.testFailed', { name: channel.name }), message: errorText(error) });
    } finally {
      this._testing = null;
    }
  }

  async #confirmDelete(): Promise<void> {
    const channel = this._delete;
    if (!channel) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteChannel(channel.id);
      toast({ tone: 'ok', title: t('settings.notifications.notice.deleted', { name: channel.name }) });
      this._delete = null;
      this.#modal.close();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #columns(): Column<Channel>[] {
    return [
      {
        key: 'name',
        label: t('settings.notifications.column.name'),
        sortable: true,
        render: (r) =>
          html`<lcm-settings-cell sub=${isSystem(r) ? t('settings.notifications.systemManaged') : ''}
            ><strong>${r.name}</strong></lcm-settings-cell
          >`,
      },
      { key: 'type', label: t('settings.notifications.column.type'), render: (r) => t(CHANNEL_TYPE_KEYS[r.type] ?? 'settings.notifications.type.email') },
      { key: 'config', label: t('settings.notifications.column.config'), render: (r) => html`<lcm-settings-cell>${channelSummary(r)}</lcm-settings-cell>` },
      {
        key: 'enabled',
        label: t('settings.notifications.column.status'),
        render: (r) =>
          r.enabled
            ? html`<lcm-badge tone="ok">${t('settings.notifications.active')}</lcm-badge>`
            : html`<lcm-badge>${t('settings.notifications.inactive')}</lcm-badge>`,
      },
      {
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-settings-actions>
          <lcm-button
            variant="ghost"
            size="small"
            icon="bell"
            ?busy=${this._testing !== null && String(this._testing) === String(r.id)}
            @click=${(e: Event) => {
              e.stopPropagation();
              const found = this.#channels.data?.find((c) => String(c.id) === String(r.id));
              if (found) void this.#test(found);
            }}
            >${t('settings.notifications.test')}</lcm-button
          >
        </lcm-settings-actions>`,
      },
    ];
  }

  #openRow(id: number): void {
    const channel = this.#channels.data?.find((c) => c.id === id);
    if (channel && isSystem(channel)) void router.navigate('/settings/general');
    else this.#modal.show({ kind: 'edit', id });
  }

  #table() {
    const channels = this.#channels.data;
    if (!channels) return this.#channels.error ? html`<p class="callout warn">${this.#channels.error}</p>` : html`<lcm-skeleton lines="3"></lcm-skeleton>`;
    if (channels.length === 0) {
      return html`<lcm-empty-state level="3" icon="bell" heading=${t('settings.notifications.empty.title')} text=${t('settings.notifications.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      active-id=${String(this.#modal.editId ?? '')}
      .columns=${this.#columns}
      .rows=${channels.map((c) => ({ ...c, id: String(c.id) }))}
      .labels=${tableLabels(t('settings.notifications.caption'), t('settings.notifications.empty.title'))}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#openRow(Number(e.detail.id))}
    ></lcm-data-table>`;
  }

  #input(form: ChannelForm, errors: Record<string, string | undefined>, field: keyof ChannelForm, label: string, options: { type?: string; placeholder?: string; hint?: string; mono?: boolean; width?: FieldWidth; maxlength?: number; digits?: boolean } = {}) {
    return html`<lcm-input
      label=${label}
      width=${options.width ?? 'm'}
      type=${options.type ?? 'text'}
      ?mono=${options.mono ?? false}
      ?digits=${options.digits ?? false}
      maxlength=${options.maxlength ?? nothing}
      autocomplete="off"
      placeholder=${options.placeholder ?? ''}
      hint=${options.hint ?? ''}
      error=${this.#modal.fieldText(errors, field, options.maxlength ?? 0)}
      .value=${String(form[field])}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ [field]: e.detail.value } as Partial<ChannelForm>)}
    ></lcm-input>`;
  }

  #emailFields(form: ChannelForm, errors: Record<string, string | undefined>, isNew: boolean) {
    return html`<div class="field-row">
        ${this.#input(form, errors, 'host', t('settings.notifications.form.host'), { placeholder: 'smtp.example.com', mono: true, maxlength: LIMITS.host })}
        ${this.#input(form, errors, 'port', t('settings.notifications.form.port'), { digits: true, mono: true, width: 'xs', maxlength: 5 })}
      </div>
      ${mailSecurityField(form.security, (security) => this.#modal.set({ security, port: portForSecurity(form.port, security) }))}
      ${this.#input(form, errors, 'username', t('settings.notifications.form.username'), { maxlength: LIMITS.loginUser })}
      ${this.#input(form, errors, 'secret', t('settings.notifications.form.password'), { type: 'password', maxlength: LIMITS.password, placeholder: isNew ? '' : t('settings.notifications.form.unchanged') })}
      ${this.#input(form, errors, 'from', t('settings.notifications.form.from'), { placeholder: 'lcm@example.com', width: 'l', maxlength: LIMITS.sender, hint: t('settings.notifications.form.fromHint') })}
      <lcm-textarea
        label=${t('settings.notifications.form.recipients')}
        width="l"
        rows="3"
        maxlength=${LIMITS.list}
        placeholder=${'ops@example.com\nadmin@example.com'}
        hint=${t('settings.notifications.form.recipientsHint')}
        error=${this.#modal.fieldText(errors, 'recipients', { max: MAX_RECIPIENTS, list: badRecipientsText(form.recipients) })}
        .value=${form.recipients}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ recipients: e.detail.value })}
      ></lcm-textarea>`;
  }

  #webhookFields(form: ChannelForm, errors: Record<string, string | undefined>, isNew: boolean) {
    return html`<lcm-select
        label=${t('settings.notifications.form.format')}
        width="l"
        hint=${t('settings.notifications.form.formatHint')}
        .options=${Object.entries(FORMAT_KEYS).map(([value, key]) => ({ value, label: t(key) }))}
        .value=${form.format}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ format: e.detail.value })}
      ></lcm-select>
      ${this.#input(form, errors, 'secret', t('settings.notifications.form.url'), {
        type: 'password',
        mono: true,
        placeholder: isNew ? 'https://…' : t('settings.notifications.form.unchanged'),
        hint: t('settings.notifications.form.urlHint'),
        width: 'full',
        maxlength: LIMITS.path,
      })}`;
  }

  #modalView() {
    const form = this.#modal.form;
    const isNew = this.#modal.editId === null;
    const errors = form ? validateChannelForm(form, isNew) : {};
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(isNew ? 'settings.notifications.form.newTitle' : 'settings.notifications.form.editTitle')}
      save-label=${t(isNew ? 'settings.more.modal.create' : 'settings.more.modal.save')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form
        ? html`${this.#input(form, errors, 'name', t('settings.notifications.form.name'), { placeholder: t('settings.notifications.form.namePlaceholder'), maxlength: LIMITS.name })}
              <lcm-select
                label=${t('settings.notifications.form.type')}
                width="s"
                ?disabled=${!isNew}
                .options=${['email', 'webhook'].map((value) => ({ value, label: t(CHANNEL_TYPE_KEYS[value]) }))}
                .value=${form.type}
                @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ type: e.detail.value, secret: '' })}
              ></lcm-select>
            ${form.type === 'webhook' ? this.#webhookFields(form, errors, isNew) : this.#emailFields(form, errors, isNew)}
            <lcm-switch
              label=${t('settings.notifications.form.enabled')}
              on-label=${t('settings.more.switch.on')}
              off-label=${t('settings.more.switch.off')}
              on-text=${t('settings.notifications.form.enabledOn')}
              off-text=${t('settings.notifications.form.enabledOff')}
              .checked=${form.enabled}
              @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#modal.set({ enabled: e.detail.checked })}
            ></lcm-switch>`
        : nothing}
      ${this.#editing
        ? html`<lcm-button slot="footer-start" variant="ghost" icon="trash" @click=${() => (this._delete = this.#editing ?? null)}>
            ${t('settings.notifications.delete.label')}
          </lcm-button>`
        : nothing}
    </lcm-settings-modal>`;
  }

  #deleteDialog() {
    const channel = this._delete;
    return html`<lcm-dialog
      ?open=${channel !== null}
      heading=${t('settings.notifications.delete.title', { name: channel?.name ?? '' })}
      message=${t('settings.notifications.delete.message')}
      .impacts=${[
        { label: t('settings.notifications.delete.inUse'), value: t('settings.notifications.delete.inUseValue'), tone: 'ok' },
        { label: t('settings.notifications.delete.secret'), value: t('settings.notifications.delete.secretValue'), tone: 'warn' },
      ]}
      confirm-label=${t('settings.notifications.delete.confirm')}
      cancel-label=${t('settings.more.modal.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  override render() {
    return html`<div class="main">
        <p class="lead">${t('settings.notifications.lead')} <a class="link" href="/settings/general">${t('settings.notifications.generalLink')}</a></p>
        <lcm-card level="2" heading=${t('settings.notifications.channels')}>
          <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.notifications.add')}</lcm-button>
          ${this.#table()}
        </lcm-card>
      </div>
      ${this.#modalView()} ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-settings-notifications', SettingsNotifications);
