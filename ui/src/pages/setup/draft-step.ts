import { css, html, nothing } from 'lit';
import '../../components/lcm-input';
import { formLayoutStyles } from '../../components/layout-styles';
import { t } from '../../lib/i18n';
import type { InputRenderer } from '../settings/general-fields';
import { draftOf, errorParams, patchBody, validateDraft, type Draft, type FieldErrors, type FieldSpec } from '../settings/system/form-state';
import { SetupStep } from './setup-step';

/**
 * Grundlage der Schritte, die Felder der Einstellungen als Entwurf bearbeiten (E-Mail, Aufbewahrung): Der Entwurf kommt
 * aus den gespeicherten Einstellungen, `input` zeichnet ein Feld der Liste (`fields`), `check` prüft und `changes` liefert
 * nur, was sich geändert hat. Dieselben Felder, Texte und Grenzen wie Einstellungen → Allgemein.
 */
export abstract class DraftStep extends SetupStep {
  static override properties = { ...SetupStep.properties, _draft: { state: true }, _errors: { state: true } };
  static override styles = [
    ...SetupStep.styles,
    formLayoutStyles,
    css`
      p {
        margin: 0;
      }
    `,
  ];

  protected abstract readonly fields: FieldSpec[];
  declare _draft: Draft | undefined;
  declare _errors: FieldErrors;
  /** Der gespeicherte Stand, gegen den `changes` vergleicht. */
  protected base: Draft = {};

  constructor() {
    super();
    this._errors = {};
  }

  override willUpdate(): void {
    if (this._draft) return;
    this.base = draftOf(this.settings, this.fields);
    this._draft = { ...this.base };
  }

  protected set(key: string, value: string | boolean): void {
    if (!this._draft) return;
    this._draft = { ...this._draft, [key]: value };
    this._errors = {};
  }

  protected value(key: string): string {
    return String(this._draft?.[key] ?? '');
  }

  protected readonly input: InputRenderer = (key, label, options = {}) => {
    const spec = this.fields.find((field) => field.key === key);
    return html`<lcm-input
      data-field=${key}
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
  };

  /** Prüft den Entwurf; bei Fehlern stehen sie an den Feldern, und der Schritt bleibt stehen. */
  protected check(draft: Draft): boolean {
    const invalid = validateDraft(draft, this.fields);
    if (Object.keys(invalid).length === 0) return true;
    this._errors = Object.fromEntries(Object.entries(invalid).map(([key, message]) => [key, t(message, errorParams(this.fields.find((field) => field.key === key), String(draft[key] ?? '')))]));
    this._error = t('settings.form.fixErrors');
    return false;
  }

  protected changes(draft: Draft): Record<string, unknown> {
    return patchBody(draft, this.base, this.fields);
  }
}
