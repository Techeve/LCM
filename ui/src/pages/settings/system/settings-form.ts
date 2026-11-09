import { LitElement, html, nothing, type TemplateResult } from 'lit';
import type { FieldWidth } from '../../../components/field-styles';
import '../../../components/lcm-button';
import '../../../components/lcm-empty-state';
import '../../../components/lcm-input';
import '../../../components/lcm-save-bar';
import '../../../components/lcm-skeleton';
import '../../../components/lcm-switch';
import '../../../components/lcm-textarea';
import type { SaveBarField } from '../../../components/lcm-save-bar';
import { toast } from '../../../components/lcm-toaster';
import { baseStyles } from '../../../components/shared';
import { ApiError } from '../../../lib/api';
import { GLOBAL_URL, readGlobal, settingsApi, type GlobalSettings } from '../../../api/settings';
import { I18nController } from '../../../lib/controllers';
import { SAVE_EVENT } from '../../../lib/hotkeys';
import { errorText, t } from '../../../lib/i18n';
import { LiveData } from '../../../lib/live-data';
import { saveBarLabels } from '../../../lib/save-bar';
import { unsaved } from '../../../lib/unsaved';
import { draftOf, errorParams, fieldForDetail, fieldOfParams, isDirty, mergeDraft, patchBody, validateDraft, type Draft, type FieldErrors, type FieldSpec } from './form-state';
import { settingsStyles } from './settings-styles';

export interface InputOptions {
  hint?: string;
  /** Breite nach Inhalt; Vorgabe `s` für Zahlen, sonst `m`. */
  width?: FieldWidth;
  mono?: boolean;
  type?: string;
  placeholder?: string;
  disabled?: boolean;
  autocomplete?: string;
}

export interface FlagOptions {
  description?: string;
  on?: string;
  off?: string;
  disabled?: boolean;
}

/**
 * Grundlage der Einstellungsseiten, die globale Einstellungen bearbeiten: lädt sie, hält einen Entwurf,
 * meldet Ungespeichertes, speichert mit Strg/Cmd+S und der Leiste am unteren Rand und ordnet Fehler dem
 * Feld zu. Die Seite nennt ihre Felder (`fields`), zeichnet die Karten (`renderForm`) und optional die
 * Erklärspalte daneben (`explainer`). Die Karten stehen in der festen Inhaltsspalte (`columnStyles`).
 * Gesendet wird nur, was sich geändert hat (PATCH).
 */
export abstract class SettingsForm extends LitElement {
  static override properties = {
    _draft: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _errors: { state: true },
  };
  static override styles = [baseStyles, settingsStyles];

  declare _draft: Draft | undefined;
  declare _busy: boolean;
  declare _error: string;
  declare _errors: FieldErrors;

  protected abstract readonly fields: FieldSpec[];
  protected abstract renderForm(settings: GlobalSettings, draft: Draft): TemplateResult;

  protected readonly loaded = new LiveData<GlobalSettings>(this, { url: () => GLOBAL_URL, transform: readGlobal });
  #source: GlobalSettings | undefined;
  #base: Draft | undefined;
  /** Beschriftungen der gezeichneten Felder, damit die Speicherleiste das fehlerhafte Feld nennen kann. */
  readonly #labels = new Map<string, string>();

  constructor() {
    super();
    this._draft = undefined;
    this._busy = false;
    this._error = '';
    this._errors = {};
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(SAVE_EVENT, this.#onSaveKey);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener(SAVE_EVENT, this.#onSaveKey);
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    const data = this.loaded.data;
    if (!data || data === this.#source) return;
    this.#source = data;
    const fresh = draftOf(data, this.fields);
    const oldBase = this.#base;
    this._draft = this._draft && oldBase ? mergeDraft(this._draft, oldBase, fresh, this.fields) : fresh;
    this.#base = fresh;
    this.#markDirty();
  }

  /** Die gespeicherten Werte der Einstellungen. */
  protected get settings(): GlobalSettings {
    return this.#source ?? {};
  }

  protected get dirty(): boolean {
    return !!this._draft && !!this.#base && isDirty(this._draft, this.#base, this.fields);
  }

  #markDirty(): void {
    unsaved.set(this, this.dirty);
  }

  /** Strg/Cmd+S: speichert, wenn etwas zu speichern ist; der Browser bleibt in jedem Fall still. */
  #onSaveKey = (event: Event): void => {
    event.preventDefault();
    void this.save();
  };

  /** Enter in einem einzeiligen Feld speichert (die Felder liegen im Shadow DOM, ein echtes Submit gibt es dort nicht). */
  #onKeyDown(event: KeyboardEvent): void {
    const path = event.composedPath();
    const target = path[0];
    if (path.some((node) => node instanceof HTMLElement && node.localName === 'lcm-dialog')) return; // Eingaben im Dialog gehören dem Dialog
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement) || ['checkbox', 'radio', 'button', 'file'].includes(target.type)) return;
    event.preventDefault();
    void this.save();
  }

  protected value(key: string): string {
    return String(this._draft?.[key] ?? '');
  }

  protected on(key: string): boolean {
    return this._draft?.[key] === true;
  }

  protected set(key: string, value: string | boolean): void {
    if (!this._draft) return;
    this._draft = { ...this._draft, [key]: value };
    if (this._errors[key]) {
      const { [key]: _removed, ...rest } = this._errors;
      this._errors = rest;
    }
    this.#markDirty();
  }

  /** Verwirft die Eingaben und zeigt wieder den gespeicherten Stand. */
  protected discard(): void {
    if (this.#base) this._draft = { ...this.#base };
    this._errors = {};
    this._error = '';
    this.#markDirty();
  }

  /** Schickt die Änderungen ab. Seiten mit eigenem Endpunkt überschreiben das. */
  protected async request(body: Record<string, unknown>, _draft: Draft, _base: Draft): Promise<GlobalSettings | void> {
    return settingsApi.patchGlobal(body);
  }

  /** Das Feld, zu dem ein Fehlercode des Servers gehört (etwa `backup.weak_passphrase`); sonst `undefined`. */
  protected fieldForCode(_code: string): string | undefined {
    return undefined;
  }

  /** Weitere Prüfungen der Seite; Werte sind Übersetzungsschlüssel. */
  protected validate(_draft: Draft): FieldErrors {
    return {};
  }

  /** Wird nach erfolgreichem Speichern aufgerufen (etwa für Nebenaufrufe). */
  protected async afterSave(_updated: GlobalSettings | void, _draft: Draft): Promise<void> {}

  async save(): Promise<void> {
    const draft = this._draft;
    const base = this.#base;
    if (!draft || !base || this._busy || !isDirty(draft, base, this.fields)) return;
    const invalid = { ...validateDraft(draft, this.fields), ...this.validate(draft) };
    if (Object.keys(invalid).length > 0) {
      this._errors = Object.fromEntries(Object.keys(invalid).map((key) => [key, this.#rangeText(key, invalid[key]!)]));
      this._error = t('settings.form.fixErrors');
      return;
    }
    this._busy = true;
    this._error = '';
    this._errors = {};
    try {
      const updated = await this.request(patchBody(draft, base, this.fields), draft, base);
      const saved = updated ? draftOf(updated, this.fields) : this.#afterSaveDraft(draft);
      this.#base = saved;
      this._draft = { ...saved };
      this.#markDirty();
      await this.afterSave(updated, draft);
      toast({ tone: 'ok', title: t('settings.form.saved') });
    } catch (error) {
      this.#showError(error);
    } finally {
      this._busy = false;
    }
  }

  /** Ohne Antwort mit den neuen Werten gilt der Entwurf als gespeichert; Geheimnisse sind dann leer. */
  #afterSaveDraft(draft: Draft): Draft {
    const saved = { ...draft };
    for (const field of this.fields) if (field.kind === 'secret') saved[field.key] = '';
    return saved;
  }

  #showError(error: unknown): void {
    const message = errorText(error);
    const keys = this.fields.map((field) => field.key);
    let field: string | undefined;
    if (error instanceof ApiError) {
      field = this.fieldForCode(error.code) ?? fieldOfParams(error.params, keys) ?? (typeof error.params.detail === 'string' ? fieldForDetail(error.params.detail, keys) : undefined);
    }
    if (field) this._errors = { [field]: message };
    this._error = field ? t('settings.form.fixErrors') : message;
  }

  #rangeText(key: string, errorKey: string): string {
    const spec = this.fields.find((field) => field.key === key);
    return t(errorKey, errorParams(spec, String(this._draft?.[key] ?? '')));
  }

  /** Erklärspalte rechts neben den Karten; ohne bleibt der Platz frei. */
  protected explainer(): TemplateResult | undefined {
    return undefined;
  }

  /** Fenster und Dialoge der Seite, außerhalb der Spalten. */
  protected overlays(): TemplateResult | typeof nothing {
    return nothing;
  }

  protected input(key: string, label: string, options: InputOptions = {}): TemplateResult {
    const spec = this.fields.find((field) => field.key === key);
    this.#labels.set(key, label);
    return html`<lcm-input
      data-key=${key}
      label=${label}
      maxlength=${spec?.limit ?? nothing}
      width=${options.width ?? (spec?.kind === 'int' ? 's' : 'm')}
      hint=${options.hint ?? ''}
      error=${this._errors[key] ?? ''}
      placeholder=${options.placeholder ?? ''}
      type=${options.type ?? (spec?.kind === 'int' ? 'number' : 'text')}
      autocomplete=${options.autocomplete ?? 'off'}
      ?mono=${options.mono ?? spec?.kind === 'int'}
      ?disabled=${options.disabled ?? false}
      .value=${this.value(key)}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.set(key, e.detail.value)}
    ></lcm-input>`;
  }

  protected area(key: string, label: string, options: InputOptions & { rows?: number } = {}): TemplateResult {
    this.#labels.set(key, label);
    return html`<lcm-textarea
      data-key=${key}
      label=${label}
      maxlength=${this.fields.find((field) => field.key === key)?.limit ?? nothing}
      width=${options.width ?? 'full'}
      hint=${options.hint ?? ''}
      error=${this._errors[key] ?? ''}
      placeholder=${options.placeholder ?? ''}
      rows=${options.rows ?? 4}
      ?mono=${options.mono ?? true}
      .value=${this.value(key)}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.set(key, e.detail.value)}
    ></lcm-textarea>`;
  }

  protected flag(key: string, label: string, options: FlagOptions = {}): TemplateResult {
    return html`<lcm-switch
      label=${label}
      description=${options.description ?? ''}
      on-label=${t('settings.form.on')}
      off-label=${t('settings.form.off')}
      on-text=${options.on ?? ''}
      off-text=${options.off ?? ''}
      ?disabled=${options.disabled ?? false}
      .checked=${this.on(key)}
      @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.set(key, e.detail.checked)}
    ></lcm-switch>`;
  }

  /** Das erste Feld mit Fehler, sofern die Seite es über `input`/`area` gezeichnet hat. */
  #errorField(): SaveBarField | null {
    const key = Object.keys(this._errors).find((name) => this.#labels.has(name));
    if (!key) return null;
    return { label: this.#labels.get(key)!, element: () => this.renderRoot.querySelector<HTMLElement>(`[data-key="${key}"]`) };
  }

  #renderBar() {
    const field = this.#errorField();
    const others = Object.keys(this._errors).length - 1;
    return html`<lcm-save-bar
      sticky
      ?dirty=${this.dirty}
      ?busy=${this._busy}
      error=${field ? (others > 0 ? t('settings.form.moreFields', { count: others }) : '') : this._error}
      .field=${field}
      .labels=${saveBarLabels()}
      @lcm-save=${() => void this.save()}
      @lcm-discard=${() => this.discard()}
    ></lcm-save-bar>`;
  }

  override render() {
    const data = this.loaded.data;
    const draft = this._draft;
    if (!data || !draft) {
      if (this.loaded.error) {
        return html`<lcm-empty-state level="2" icon="warning" heading=${t('settings.form.loadFailed')} text=${this.loaded.error}>
          <lcm-button slot="action" @click=${() => this.loaded.reload()}>${t('settings.form.retry')}</lcm-button>
        </lcm-empty-state>`;
      }
      return html`<div class="skeletons"><lcm-skeleton shape="block" label=${t('settings.form.loading')}></lcm-skeleton><lcm-skeleton shape="block"></lcm-skeleton></div>`;
    }
    const explainer = this.explainer();
    return html`<div @keydown=${this.#onKeyDown}>
        <div class=${explainer ? 'layout explain' : 'layout'}>
          <div class="stack">${this.renderForm(data, draft)} ${this.#renderBar()}</div>
          ${explainer ?? nothing}
        </div>
      </div>
      ${this.overlays()}`;
  }
}
