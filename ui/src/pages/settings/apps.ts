import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import type { FieldWidth } from '../../components/field-styles';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import '../../components/lcm-textarea';
import type { Column } from '../../components/lcm-data-table';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import {
  APPS_URL,
  CUSTOM_ACTIONS_URL,
  normaliseApps,
  normaliseCustomActions,
  settingsMoreApi,
  type AppEntry,
  type CustomAction,
} from '../../api/settings-more';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, i18n, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { APP_LIMITS, COMPARE_KEYS, appBody, appFormOf, emptyAppForm, localised, validateAppForm, type AppForm, type AppTextField } from './packages/app-model';
import { ModalForm, explainerToggle, generalError, nonEmptyLines, sectionStyles, tableLabels } from './packages/kit';
import './packages/cells';
import './packages/row-switch';
import './packages/settings-modal';

const FIELDS: Record<string, string> = {
  'app.invalid_slug': 'slug',
  'app.slug_taken': 'slug',
  'app.name_required': 'name',
  'app.invalid_marker': 'markers',
  'app.no_marker': 'markers',
  'app.invalid_pattern': 'version_pattern',
  'app.invalid_source': 'latest_source',
  'app.action_missing': 'update_action_id',
};

const FIELD_LABELS: Partial<Record<keyof AppForm, string>> = {
  slug: 'settings.apps.field.slug',
  name: 'settings.apps.field.name',
  description: 'settings.apps.field.description',
  name_en: 'settings.apps.field.name_en',
  description_en: 'settings.apps.field.description_en',
  version_command: 'settings.apps.field.version_command',
  version_pattern: 'settings.apps.field.version_pattern',
  latest_source: 'settings.apps.field.latest_source',
  latest_pattern: 'settings.apps.field.latest_pattern',
};

type Confirm = { kind: 'save' } | { kind: 'delete'; app: AppEntry } | { kind: 'enable'; app: AppEntry };

/** Einstellungen → Apps: Steckbriefe der Software, die nicht aus der Paketverwaltung kommt. */
export class SettingsApps extends LitElement {
  static override properties = { _dialogError: { state: true }, _confirm: { state: true }, _busy: { state: true } };
  static override styles = [baseStyles, sectionStyles];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _confirm: Confirm | null;
  declare _busy: boolean;
  readonly #apps = new LiveData<AppEntry[]>(this, { url: () => APPS_URL, transform: normaliseApps });
  readonly #actions = new LiveData<CustomAction[]>(this, { url: () => CUSTOM_ACTIONS_URL, transform: normaliseCustomActions });
  readonly #modal = new ModalForm<AppForm>(this, (modal) => {
    if (modal.kind === 'new') return emptyAppForm();
    if (modal.kind !== 'edit' || !this.#apps.data) return this.#apps.loading ? undefined : null;
    const app = this.#apps.data.find((a) => a.id === modal.id);
    return app ? appFormOf(app) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._confirm = null;
    this._busy = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [{ id: 'app-new', key: 'n', label: t('settings.apps.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  get #editing(): AppEntry | undefined {
    const id = this.#modal.editId;
    return id ? this.#apps.data?.find((a) => a.id === id) : undefined;
  }

  /** Neue oder geänderte Kommandos, die bald als root laufen: erst bestätigen lassen. */
  #needsConfirm(form: AppForm): boolean {
    if (!form.enabled) return false;
    const before = this.#editing;
    return !before || !before.enabled || before.markers !== form.markers || before.version_command !== form.version_command.trim();
  }

  #requestSave(): void {
    const form = this.#modal.form;
    if (!form) return;
    const valid = Object.keys(validateAppForm(form)).length === 0;
    if (valid && this.#needsConfirm(form)) this._confirm = { kind: 'save' };
    else void this.#save();
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    this._confirm = null;
    const id = this.#modal.editId;
    const valid = Object.keys(validateAppForm(form)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveApp(id, appBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t(id ? 'settings.apps.notice.saved' : 'settings.apps.notice.created', { name: form.name.trim() }) });
    }
  }

  async #delete(app: AppEntry): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteApp(app.id);
      toast({ tone: 'ok', title: t('settings.apps.notice.deleted', { name: localised(app, i18n.language).name }) });
      this._confirm = null;
      this.#modal.close();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  /** Schalter in der Liste. Abschalten geht sofort; Einschalten lässt Kommandos als root laufen und wird bestätigt. */
  #toggle(app: AppEntry, enabled: boolean): void {
    if (enabled) this._confirm = { kind: 'enable', app };
    else void this.#setEnabled(app, false);
  }

  async #setEnabled(app: AppEntry, enabled: boolean): Promise<void> {
    this._busy = true;
    const { name } = localised(app, i18n.language);
    try {
      await settingsMoreApi.saveApp(app.id, appBody({ ...appFormOf(app), enabled }));
      toast({ tone: 'ok', title: t(enabled ? 'apps.enabled' : 'apps.disabled', { name }) });
      this._confirm = null;
    } catch (error) {
      // Einschalten wurde bestätigt: der Fehler bleibt im Dialog. Abschalten geht ohne Dialog.
      if (this._confirm) this._dialogError = errorText(error);
      else toast({ tone: 'crit', title: t('settings.apps.notice.failed'), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  #latest(app: AppEntry) {
    if (app.latest_error) return html`<lcm-badge tone="crit" title=${app.latest_error}>${t('settings.apps.latest.error')}</lcm-badge>`;
    if (app.latest_version) return html`<span class="mono">${app.latest_version}</span>`;
    return html`<span class="muted">${t(app.latest_source ? 'settings.apps.latest.pending' : 'settings.apps.latest.noSource')}</span>`;
  }

  get #columns(): Column<AppEntry>[] {
    return [
      {
        key: 'name',
        label: t('settings.apps.column.name'),
        sortable: true,
        value: (r) => localised(r, i18n.language).name,
        render: (r) => {
          const { name, description } = localised(r, i18n.language);
          // Der Schalter steht vorn in der Zeile; für eine eigene Spalte reicht neben der Erklärspalte der Platz nicht.
          return html`<lcm-settings-cell sub=${description}
            ><lcm-row-switch
              label=${t(r.enabled ? 'apps.switchOff' : 'apps.switchOn', { name })}
              ?checked=${r.enabled}
              ?disabled=${this._busy}
              @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#toggle(r, e.detail.checked)}
            ></lcm-row-switch
            ><strong>${name}</strong>${r.builtin ? html`<lcm-badge>${t('settings.apps.builtin')}</lcm-badge>` : nothing}</lcm-settings-cell
          >`;
        },
      },
      {
        key: 'markers',
        label: t('settings.apps.column.markers'),
        render: (r) => {
          const [first = '', ...rest] = nonEmptyLines(r.markers);
          return html`<lcm-settings-cell mono sub=${rest.length ? t('settings.apps.moreMarkers', { count: rest.length }) : ''}>${first}</lcm-settings-cell>`;
        },
      },
      { key: 'latest', label: t('settings.apps.column.latest'), render: (r) => this.#latest(r) },
    ];
  }

  #table() {
    const apps = this.#apps.data;
    if (!apps) return this.#apps.error ? html`<p class="callout warn">${this.#apps.error}</p>` : html`<lcm-skeleton lines="5"></lcm-skeleton>`;
    if (apps.length === 0) return html`<lcm-empty-state level="3" icon="package" heading=${t('settings.apps.empty.title')} text=${t('settings.apps.empty.text')}></lcm-empty-state>`;
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      active-id=${String(this.#modal.editId ?? '')}
      .columns=${this.#columns}
      .rows=${apps.map((a) => ({ ...a, id: String(a.id) }))}
      .labels=${tableLabels(t('settings.apps.caption'), t('settings.apps.empty.title'))}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
    ></lcm-data-table>`;
  }

  #input(form: AppForm, errors: Record<string, string | undefined>, field: keyof AppForm, options: { mono?: boolean; hint?: string; placeholder?: string; disabled?: boolean; width?: FieldWidth } = {}) {
    return html`<lcm-input
      label=${t(FIELD_LABELS[field] ?? 'settings.apps.field.name')}
      width=${options.width ?? 'm'}
      maxlength=${field in APP_LIMITS ? APP_LIMITS[field as AppTextField] : nothing}
      ?mono=${options.mono ?? false}
      ?disabled=${options.disabled ?? false}
      hint=${options.hint ?? ''}
      placeholder=${options.placeholder ?? ''}
      error=${this.#modal.fieldText(errors, field, field in APP_LIMITS ? APP_LIMITS[field as AppTextField] : 0)}
      .value=${String(form[field])}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ [field]: e.detail.value } as Partial<AppForm>)}
    ></lcm-input>`;
  }

  #actionSelect(form: AppForm, field: 'backup_action_id' | 'update_action_id', hint = '') {
    const error = this.#modal.fieldText({}, field);
    const options = [{ value: '', label: t('settings.apps.form.noAction') }, ...(this.#actions.data ?? []).map((a) => ({ value: String(a.id), label: a.name }))];
    return html`<lcm-select
      label=${t(field === 'backup_action_id' ? 'settings.apps.field.backup_action_id' : 'settings.apps.field.update_action_id')}
      width="l"
      hint=${error || hint}
      .options=${options}
      .value=${form[field]}
      @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ [field]: e.detail.value } as Partial<AppForm>)}
    ></lcm-select>`;
  }

  #form(form: AppForm) {
    const errors = validateAppForm(form);
    const builtin = this.#editing?.builtin ?? false;
    return html`${builtin ? html`<p class="callout warn">${t('settings.apps.form.builtinHint')}</p>` : nothing}
      ${this.#input(form, errors, 'slug', { mono: true, hint: t('settings.apps.form.slugHint'), disabled: this.#modal.editId !== null })}
      ${this.#input(form, errors, 'name')} ${this.#input(form, errors, 'description', { width: 'full' })}
      ${this.#input(form, errors, 'name_en')} ${this.#input(form, errors, 'description_en', { width: 'full', hint: t('settings.apps.form.englishHint') })}
      <p class="group-title">${t('settings.apps.form.detection')}</p>
      <lcm-textarea
        label=${t('settings.apps.field.markers')}
        mono
        rows="4"
        maxlength=${APP_LIMITS.markers}
        placeholder=${'path /opt/AdGuardHome/AdGuardHome'}
        hint=${t('settings.apps.form.markersHint')}
        error=${this.#modal.fieldText(errors, 'markers', APP_LIMITS.markers)}
        .value=${form.markers}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ markers: e.detail.value })}
      ></lcm-textarea>
      ${this.#input(form, errors, 'version_command', { mono: true, width: 'l', hint: t('settings.apps.form.versionCommandHint'), placeholder: '{path} --version' })}
        <lcm-select
          label=${t('settings.apps.field.compare')}
          .options=${Object.entries(COMPARE_KEYS).map(([value, key]) => ({ value, label: t(key) }))}
          .value=${form.compare}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ compare: e.detail.value })}
        ></lcm-select>
      ${this.#input(form, errors, 'version_pattern', { mono: true, width: 'l', hint: t('settings.apps.form.versionPatternHint') })}
      <p class="group-title">${t('settings.apps.form.latest')}</p>
      ${this.#input(form, errors, 'latest_source', { mono: true, width: 'l', hint: t('settings.apps.form.sourceHint'), placeholder: 'github:owner/repo' })}
      ${this.#input(form, errors, 'latest_pattern', { mono: true, width: 'l' })}
      <p class="group-title">${t('settings.apps.form.update')}</p>
      ${this.#actionSelect(form, 'backup_action_id')} ${this.#actionSelect(form, 'update_action_id', t('settings.apps.form.actionHint'))}
      <lcm-switch
        label=${t('settings.apps.field.enabled')}
        on-label=${t('settings.more.switch.on')}
        off-label=${t('settings.more.switch.off')}
        on-text=${t('settings.apps.form.enabledOn')}
        off-text=${t('settings.apps.form.enabledOff')}
        .checked=${form.enabled}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#modal.set({ enabled: e.detail.checked })}
      ></lcm-switch>`;
  }

  #dialog() {
    const confirm = this._confirm;
    const form = this.#modal.form;
    let heading = '';
    let message = '';
    let label = '';
    let impacts: Impact[] = [];
    let expect = '';
    let run = () => {};
    const rootImpacts = (command: string): Impact[] => [
      { label: t('settings.apps.confirm.where'), value: t('settings.apps.confirm.whereValue') },
      { label: t('settings.apps.confirm.as'), value: t('settings.apps.confirm.asValue'), tone: 'warn' },
      { label: t('settings.apps.confirm.command'), value: command || t('settings.apps.confirm.noCommand') },
    ];
    if (confirm?.kind === 'save' && form) {
      heading = t('settings.apps.confirm.saveTitle', { name: form.name.trim() });
      message = t('settings.apps.confirm.saveMessage');
      label = t('settings.apps.confirm.save');
      impacts = rootImpacts(form.version_command.trim());
      run = () => void this.#save();
    } else if (confirm?.kind === 'enable') {
      const app = confirm.app;
      heading = t('apps.enableTitle', { name: localised(app, i18n.language).name });
      message = t('settings.apps.confirm.saveMessage');
      label = t('apps.enable');
      impacts = rootImpacts(app.version_command);
      run = () => void this.#setEnabled(app, true);
    } else if (confirm?.kind === 'delete') {
      const name = localised(confirm.app, i18n.language).name;
      heading = t('settings.apps.delete.title', { name });
      message = t('settings.apps.delete.message');
      label = t('settings.apps.delete.confirm');
      expect = name;
      impacts = [
        { label: t('settings.apps.delete.findings'), value: t('settings.apps.delete.findingsValue'), tone: 'crit' },
        { label: t('settings.more.impact.reversible'), value: t('settings.more.impact.no'), tone: 'crit' },
      ];
      run = () => void this.#delete(confirm.app);
    }
    return html`<lcm-dialog
      ?open=${confirm !== null}
      heading=${heading}
      message=${message}
      .impacts=${impacts}
      expect=${expect}
      expect-label=${expect ? t('settings.apps.delete.expect', { name: expect }) : ''}
      confirm-label=${label}
      cancel-label=${t('settings.more.modal.cancel')}
      tone=${confirm?.kind === 'delete' ? 'danger' : 'default'}
      ?busy=${this._busy || this.#modal.busy}
      error=${this._dialogError}
      @lcm-confirm=${run}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #explainer() {
    const toggle = explainerToggle();
    return html`<lcm-explainer topic="settings-apps" heading=${t('settings.apps.explainer.title')} collapse-label=${toggle.collapse} expand-label=${toggle.expand}>
      <p>${t('settings.apps.explainer.intro')}</p>
      <ul>
        <li><strong>${t('settings.apps.explainer.markersTitle')}</strong> ${t('settings.apps.explainer.markers')}</li>
        <li><strong>${t('settings.apps.explainer.versionTitle')}</strong> ${t('settings.apps.explainer.version')}</li>
        <li><strong>${t('settings.apps.explainer.latestTitle')}</strong> ${t('settings.apps.explainer.latest')}</li>
        <li><strong>${t('settings.apps.explainer.updateTitle')}</strong> ${t('settings.apps.explainer.update')}</li>
      </ul>
      <p>${t('settings.apps.explainer.root')}</p>
    </lcm-explainer>`;
  }

  override render() {
    const form = this.#modal.form;
    const editing = this.#modal.editId !== null;
    return html`<div class="layout explain">
        <div class="main">
          <lcm-card level="2" heading=${t('settings.apps.catalog')}>
            <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.apps.add')}</lcm-button>
            ${this.#table()}
          </lcm-card>
        </div>
        ${this.#explainer()}
      </div>
      <lcm-settings-modal
        ?open=${form !== null}
        heading=${t(editing ? 'settings.apps.form.editTitle' : 'settings.apps.form.newTitle')}
        save-label=${t(editing ? 'settings.more.modal.save' : 'settings.more.modal.create')}
        ?dirty=${this.#modal.dirty}
        ?busy=${this.#modal.busy}
        error=${generalError(this.#modal.error)}
        @lcm-submit=${() => this.#requestSave()}
        @lcm-close=${() => this.#modal.close()}
      >
        ${form ? this.#form(form) : nothing}
        ${this.#editing && !this.#editing.builtin
          ? html`<lcm-button slot="footer-start" variant="ghost" icon="trash" @click=${() => this.#editing && (this._confirm = { kind: 'delete', app: this.#editing })}>
              ${t('settings.apps.delete.label')}
            </lcm-button>`
          : nothing}
      </lcm-settings-modal>
      ${this.#dialog()}`;
  }
}
customElements.define('lcm-settings-apps', SettingsApps);
