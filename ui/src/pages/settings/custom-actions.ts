import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import '../../components/lcm-input';
import '../../components/lcm-skeleton';
import '../../components/lcm-textarea';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { CUSTOM_ACTIONS_URL, normaliseCustomActions, settingsMoreApi, type CustomAction } from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { actionBody, actionFormOf, commandLines, hasSecrets, maskSecrets, validateActionForm, type ActionForm } from './automation/action-model';
import './packages/cells';
import { ModalForm, explainerToggle, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/settings-modal';

const FIELDS: Record<string, string> = {
  'field:name': 'name',
  'field:description': 'description',
  'field:commands': 'commands',
  'custom_action.name_required': 'name',
  'custom_action.name_taken': 'name',
  'conflict.duplicate': 'name',
  'custom_action.empty': 'commands',
};

type Confirm = { kind: 'save' } | { kind: 'delete'; action: CustomAction };

/** Einstellungen → Eigene Aktionen: Kommandolisten, die Regeln und Anwendungen als root ausführen. */
export class SettingsCustomActions extends LitElement {
  static override properties = { _dialogError: { state: true }, _confirm: { state: true }, _busy: { state: true }, _revealed: { state: true } };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .masked {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .masked .label {
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      pre {
        margin: 0;
        max-block-size: 18rem;
        padding: var(--space-3);
        overflow: auto;
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        white-space: pre-wrap;
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _confirm: Confirm | null;
  declare _busy: boolean;
  declare _revealed: boolean;
  readonly #actions = new LiveData<CustomAction[]>(this, { url: () => CUSTOM_ACTIONS_URL, transform: normaliseCustomActions });
  readonly #modal = new ModalForm<ActionForm>(this, (modal) => {
    this._revealed = false;
    if (modal.kind === 'new') return { name: '', description: '', commands: '' };
    if (modal.kind !== 'edit' || !this.#actions.data) return this.#actions.loading ? undefined : null;
    const action = this.#actions.data.find((a) => a.id === modal.id);
    return action ? actionFormOf(action) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._confirm = null;
    this._busy = false;
    this._revealed = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  /** Anlegen und Ändern ist Administratoren vorbehalten; Verwalter sehen die Aktionen nur. */
  get #canWrite(): boolean {
    return auth.can('settings:manage');
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (this.#canWrite) {
      palette.register(this, [{ id: 'action-new', key: 'n', label: t('settings.customActions.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
    }
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  get #editing(): CustomAction | undefined {
    const id = this.#modal.editId;
    return id ? this.#actions.data?.find((a) => a.id === id) : undefined;
  }

  /** Geänderte Kommandos einer bestehenden Aktion laufen beim nächsten Regellauf: erst bestätigen. */
  #requestSave(): void {
    const form = this.#modal.form;
    if (!form) return;
    const valid = Object.keys(validateActionForm(form)).length === 0;
    const before = this.#editing;
    if (valid && before && before.commands !== form.commands) this._confirm = { kind: 'save' };
    else void this.#save();
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    this._confirm = null;
    const id = this.#modal.editId;
    const valid = Object.keys(validateActionForm(form)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveCustomAction(id, actionBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t(id ? 'settings.customActions.notice.saved' : 'settings.customActions.notice.created', { name: form.name.trim() }) });
    }
  }

  async #delete(action: CustomAction): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteCustomAction(action.id);
      toast({ tone: 'ok', title: t('settings.customActions.notice.deleted', { name: action.name }) });
      this._confirm = null;
      this.#modal.close();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #columns(): Column<CustomAction>[] {
    return [
      {
        key: 'name',
        label: t('settings.customActions.column.name'),
        sortable: true,
        render: (r) => html`<lcm-settings-cell sub=${r.description}><strong>${r.name}</strong></lcm-settings-cell>`,
      },
      {
        key: 'count',
        label: t('settings.customActions.column.count'),
        align: 'end',
        sortable: true,
        value: (r) => commandLines(r.commands).length,
        render: (r) => html`<lcm-badge>${commandLines(r.commands).length}</lcm-badge>`,
      },
      {
        key: 'preview',
        label: t('settings.customActions.column.preview'),
        render: (r) => {
          const lines = commandLines(r.commands);
          return html`<lcm-settings-cell mono sub=${lines.length > 1 ? t('settings.customActions.moreLines', { count: lines.length - 1 }) : ''}
            >${maskSecrets(lines[0] ?? '')}</lcm-settings-cell
          >`;
        },
      },
    ];
  }

  #table() {
    const actions = this.#actions.data;
    if (!actions) return this.#actions.error ? html`<p class="callout warn">${this.#actions.error}</p>` : html`<lcm-skeleton lines="3"></lcm-skeleton>`;
    if (actions.length === 0) {
      return html`<lcm-empty-state level="3" icon="terminal" heading=${t('settings.customActions.empty.title')} text=${t('settings.customActions.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      active-id=${String(this.#modal.editId ?? '')}
      .columns=${this.#columns}
      .rows=${actions.map((a) => ({ ...a, id: String(a.id) }))}
      .labels=${tableLabels(t('settings.customActions.caption'), t('settings.customActions.empty.title'))}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
    ></lcm-data-table>`;
  }

  /** Mit Geheimnissen zeigt die Fenster erst die maskierte Fassung; Klartext nur auf Wunsch (und nur zum Bearbeiten). */
  #commandsField(form: ActionForm, errors: ReturnType<typeof validateActionForm>) {
    const count = commandLines(form.commands).length;
    const masked = this.#modal.editId !== null && !this._revealed && hasSecrets(form.commands);
    if (masked || !this.#canWrite) {
      return html`<div class="masked">
        <span class="label">${t('settings.customActions.form.commands')}</span>
        <pre>${maskSecrets(form.commands)}</pre>
        ${hasSecrets(form.commands) ? html`<p class="callout">${t('settings.customActions.form.maskedHint')}</p>` : nothing}
        ${this.#canWrite
          ? html`<lcm-button variant="secondary" size="small" icon="eye" @click=${() => (this._revealed = true)}>${t('settings.customActions.form.reveal')}</lcm-button>`
          : nothing}
      </div>`;
    }
    return html`<lcm-textarea
      label=${t('settings.customActions.form.commands')}
      mono
      rows="9"
      maxlength=${LIMITS.script}
      placeholder=${t('settings.customActions.form.commandsPlaceholder')}
      hint=${t('settings.customActions.form.commandsHint', { count })}
      error=${this.#modal.fieldText(errors, 'commands', LIMITS.script)}
      .value=${form.commands}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ commands: e.detail.value })}
    ></lcm-textarea>`;
  }

  #modalView() {
    const form = this.#modal.form;
    const errors = form ? validateActionForm(form) : {};
    const editing = this.#modal.editId !== null;
    const readOnly = !this.#canWrite;
    const heading = readOnly ? 'settings.customActions.form.viewTitle' : editing ? 'settings.customActions.form.editTitle' : 'settings.customActions.form.newTitle';
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(heading)}
      save-label=${t(editing ? 'settings.more.modal.save' : 'settings.more.modal.create')}
      ?read-only=${readOnly}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => this.#requestSave()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form
        ? html`<lcm-input
              label=${t('settings.customActions.form.name')}
              placeholder=${t('settings.customActions.form.namePlaceholder')}
              maxlength=${LIMITS.name}
              ?disabled=${readOnly}
              error=${this.#modal.fieldText(errors, 'name', LIMITS.name)}
              .value=${form.name}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ name: e.detail.value })}
            ></lcm-input>
            <lcm-input
              label=${t('settings.customActions.form.description')}
              width="full"
              maxlength=${LIMITS.description}
              ?disabled=${readOnly}
              error=${this.#modal.fieldText(errors, 'description', LIMITS.description)}
              .value=${form.description}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ description: e.detail.value })}
            ></lcm-input>
            ${this.#commandsField(form, errors)}
            <p class="callout warn">${t('settings.customActions.form.rootHint')}</p>`
        : nothing}
      ${this.#editing && this.#canWrite
        ? html`<lcm-button slot="footer-start" variant="ghost" icon="trash" @click=${() => this.#editing && (this._confirm = { kind: 'delete', action: this.#editing })}>
            ${t('settings.customActions.delete.label')}
          </lcm-button>`
        : nothing}
    </lcm-settings-modal>`;
  }

  #dialog() {
    const confirm = this._confirm;
    const form = this.#modal.form;
    if (confirm?.kind === 'delete') {
      return html`<lcm-dialog
        open
        heading=${t('settings.customActions.delete.title', { name: confirm.action.name })}
        message=${t('settings.customActions.delete.message')}
        .impacts=${[
          { label: t('settings.customActions.delete.inUse'), value: t('settings.customActions.delete.inUseValue'), tone: 'ok' },
          { label: t('settings.more.impact.reversible'), value: t('settings.more.impact.no'), tone: 'crit' },
        ]}
        expect=${confirm.action.name}
        expect-label=${t('settings.customActions.delete.expect', { name: confirm.action.name })}
        confirm-label=${t('settings.customActions.delete.confirm')}
        cancel-label=${t('settings.more.modal.cancel')}
        tone="danger"
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-confirm=${() => void this.#delete(confirm.action)}
        @lcm-cancel=${() => (this._confirm = null)}
      ></lcm-dialog>`;
    }
    return html`<lcm-dialog
      ?open=${confirm?.kind === 'save'}
      heading=${t('settings.customActions.confirm.title', { name: form?.name.trim() ?? '' })}
      message=${t('settings.customActions.confirm.message')}
      .impacts=${[
        { label: t('settings.customActions.confirm.where'), value: t('settings.customActions.confirm.whereValue') },
        { label: t('settings.customActions.confirm.as'), value: t('settings.customActions.confirm.asValue'), tone: 'warn' },
        { label: t('settings.customActions.confirm.count'), value: String(commandLines(form?.commands ?? '').length) },
      ]}
      confirm-label=${t('settings.customActions.confirm.save')}
      cancel-label=${t('settings.more.modal.cancel')}
      ?busy=${this.#modal.busy}
      @lcm-confirm=${() => void this.#save()}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #explainer() {
    const toggle = explainerToggle();
    return html`<lcm-explainer topic="settings-custom-actions" heading=${t('settings.customActions.explainer.title')} collapse-label=${toggle.collapse} expand-label=${toggle.expand}>
      <p>${t('settings.customActions.explainer.intro')}</p>
      <ul>
        <li><strong>${t('settings.customActions.explainer.useTitle')}</strong> ${t('settings.customActions.explainer.use')}</li>
        <li><strong>${t('settings.customActions.explainer.runTitle')}</strong> ${t('settings.customActions.explainer.run')}</li>
        <li><strong>${t('settings.customActions.explainer.secretsTitle')}</strong> ${t('settings.customActions.explainer.secrets')}</li>
      </ul>
    </lcm-explainer>`;
  }

  override render() {
    return html`<div class="layout explain">
        <div class="main">
          <lcm-card level="2" heading=${t('settings.customActions.title')}>
            ${this.#canWrite
              ? html`<lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>
                  ${t('settings.customActions.add')}
                </lcm-button>`
              : nothing}
            ${this.#table()}
          </lcm-card>
        </div>
        ${this.#explainer()}
      </div>
      ${this.#modalView()} ${this.#dialog()}`;
  }
}
customElements.define('lcm-settings-custom-actions', SettingsCustomActions);
