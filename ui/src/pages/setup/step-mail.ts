import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-server-action';
import { toast } from '../../components/lcm-toaster';
import { settingsApi } from '../../api/settings';
import { errorText, t } from '../../lib/i18n';
import { portForSecurity, type MailSecurity } from '../../lib/mail-security';
import { MAIL_FIELDS, mailFields } from '../settings/general-fields';
import { helpLabels } from '../settings/system/help';
import type { Draft } from '../settings/system/form-state';
import { DraftStep } from './draft-step';

/**
 * Schritt „E-Mail-Versand“: Server, Port, Verschlüsselung, Zugang, Absender und Empfänger der Admin-Mails, dieselben
 * Felder wie in Einstellungen → Allgemein, dazu „Test-Mail senden“. Der Versand wird eingeschaltet, sobald ein Server
 * eingetragen ist; ohne Server bleibt er aus. Lässt sich überspringen.
 */
export class SetupMail extends DraftStep {
  static override properties = { ...DraftStep.properties, _testing: { state: true } };

  readonly step = 'mail' as const;
  protected readonly fields = MAIL_FIELDS;
  declare _testing: boolean;

  constructor() {
    super();
    this._testing = false;
  }

  get #hasServer(): boolean {
    return this.value('mail_host').trim() !== '';
  }

  /** Der Entwurf mit eingeschaltetem Versand: Wer einen Server einträgt, will ihn nutzen. */
  #enabled(draft: Draft): Draft {
    return { ...draft, mail_enabled: true };
  }

  /** Moduswechsel: Steht der Port auf einem Standardport, folgt er dem neuen Modus. */
  #setSecurity(security: MailSecurity): void {
    this.set('mail_security', security);
    this.set('mail_port', portForSecurity(this.value('mail_port'), security));
  }

  /** Ohne Server gibt es nichts zu speichern; der Schritt geht dann weiter wie beim Überspringen. */
  protected override async save(): Promise<void> {
    const draft = this._draft;
    if (!draft) return;
    if (!this.#hasServer) {
      await this.advance();
      return;
    }
    const enabled = this.#enabled(draft);
    if (!this.check(enabled)) return;
    await this.advance(this.changes(enabled));
  }

  /** Speichert die Angaben, ohne den Schritt zu beenden, und schickt eine Test-Mail an die Empfänger. */
  async #test(): Promise<void> {
    const draft = this._draft;
    if (!draft || this._testing) return;
    const enabled = this.#enabled(draft);
    if (!this.check(enabled)) return;
    this._testing = true;
    this._error = '';
    try {
      const body = this.changes(enabled);
      if (Object.keys(body).length > 0) {
        await settingsApi.patchGlobal(body);
        // Gespeichert gilt als Ausgangsstand; ein Geheimnis liefert der Server nie zurück.
        this.base = { ...enabled, mail_password: '' };
        this._draft = { ...this.base };
      }
      await settingsApi.testMail();
      toast({ tone: 'ok', title: t('settings.general.mail.testSent') });
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._testing = false;
    }
  }

  protected override body(): TemplateResult {
    return html`<div class="form-stack">
      ${mailFields(this.input, this.value('mail_security'), (security) => this.#setSecurity(security), false)}
      <div class="row">
        <lcm-server-action
          data-action="test-mail"
          label=${t('settings.general.mail.test')}
          icon="bell"
          help=${t('settings.general.mail.testHelp')}
          meaning=${t('settings.general.mail.testMeaning')}
          .labels=${helpLabels()}
          ?disabled=${!this.#hasServer}
          ?busy=${this._testing}
          @click=${() => void this.#test()}
        ></lcm-server-action>
        ${this.#hasServer ? html`<span class="muted">${t('setup.mail.testHint')}</span>` : nothing}
      </div>
    </div>`;
  }
}
customElements.define('lcm-setup-mail', SetupMail);
