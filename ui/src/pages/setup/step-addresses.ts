import { html, type TemplateResult } from 'lit';
import '../settings/addresses/address-block';
import { t } from '../../lib/i18n';
import { addressHelp } from '../settings/addresses/address-help';
import { ADDRESS_FIELDS, redirectTarget, uiPortChange } from '../settings/addresses/address-model';
import type { Draft, FieldErrors } from '../settings/system/form-state';
import { addressBody, changesUiPort, setupDraft, setupErrors, wizardUrl } from './setup-model';
import { SetupStep } from './setup-step';

/**
 * Schritt „Wie ist LCM erreichbar?“: Modus, Adressen und Ports, dieselben wie in Einstellungen → Allgemein
 * (`<lcm-address-block wizard>`, ohne die Schalter für Agent und MCP). Kein Überspringen. Wechselt der Port der
 * Oberfläche, folgt der Browser der neuen Adresse und der Assistent geht dort beim nächsten Schritt weiter.
 */
export class SetupAddresses extends SetupStep {
  static override properties = { ...SetupStep.properties, _draft: { state: true }, _errors: { state: true } };

  readonly step = 'addresses' as const;
  declare _draft: Draft | undefined;
  declare _errors: FieldErrors;
  #base: Draft | undefined;

  constructor() {
    super();
    this._errors = {};
  }

  override willUpdate(): void {
    if (this._draft) return;
    this.#base = setupDraft(this.settings);
    this._draft = { ...this.#base };
  }

  #onChange(event: CustomEvent<{ values: Record<string, string | boolean> }>): void {
    if (this._draft) this._draft = { ...this._draft, ...event.detail.values };
    this._errors = {};
  }

  protected override async save(): Promise<void> {
    const draft = this._draft;
    const base = this.#base;
    if (!draft || !base) return;
    const invalid = setupErrors(draft);
    if (Object.keys(invalid).length > 0) {
      this._errors = this.errorTexts(draft, invalid, ADDRESS_FIELDS);
      this._error = t('settings.form.fixErrors');
      return;
    }
    const body = addressBody(draft, base);
    const announced = uiPortChange(draft, this.settings, window.location.href);
    const updated = await this.patch(body);
    const moved = redirectTarget(updated, window.location.href, changesUiPort(body), announced);
    if (moved) window.location.assign(wizardUrl(moved));
    else this.done(updated);
  }

  protected override help(): TemplateResult {
    return addressHelp(this._draft ?? {});
  }

  protected override body(): TemplateResult {
    return html`<lcm-address-block wizard .settings=${this.settings} .draft=${this._draft ?? {}} .errors=${this._errors} @lcm-address-change=${this.#onChange}></lcm-address-block>`;
  }
}
customElements.define('lcm-setup-addresses', SetupAddresses);
