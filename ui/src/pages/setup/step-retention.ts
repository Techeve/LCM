import { html, type TemplateResult } from 'lit';
import { RETENTION_FIELDS, retentionFields } from '../settings/general-fields';
import { DraftStep } from './draft-step';

/**
 * Schritt „Aufbewahrung“: wie viele Tage LCM Jobs, Routine-Protokolle und den Speicherverlauf aufhebt. Dieselben Felder
 * wie Einstellungen → Allgemein, mit den gespeicherten Werten (den Vorgaben) vorbelegt; was Jobs sind, sagt der Einleitungssatz.
 */
export class SetupRetention extends DraftStep {
  readonly step = 'retention' as const;
  protected readonly fields = RETENTION_FIELDS;

  protected override async save(): Promise<void> {
    const draft = this._draft;
    if (!draft || !this.check(draft)) return;
    await this.advance(this.changes(draft));
  }

  protected override body(): TemplateResult {
    return html`<div class="form-stack">${retentionFields(this.input)}</div>`;
  }
}
customElements.define('lcm-setup-retention', SetupRetention);
