import { LitElement, css, html, nothing } from 'lit';
import '../components/lcm-button';
import '../components/lcm-modal';
import '../components/lcm-notice';
import '../pages/settings/session-mode-field';
import './release-notes-dialog';
import { GLOBAL_URL, readGlobal, settingsApi, type GlobalSettings } from '../api/settings';
import { baseStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { errorText, t } from '../lib/i18n';
import { LiveData } from '../lib/live-data';
import { router } from '../lib/router';

/**
 * <lcm-session-mode-dialog> – die einmalige Frage „Wie soll die Anmeldung im Browser funktionieren?“. Sie erscheint direkt
 * nach den Pflichtschritten (Passwort, 2FA) für Benutzer, die die globalen Einstellungen ändern dürfen, solange der Betreiber
 * die Wahl nicht bestätigt hat. „Übernehmen“ speichert Weg und Bestätigung; bleibt der Weg gleich, bleibt die Sitzung.
 * Schließen ohne Wahl lässt den Standard stehen, die Frage kommt bei der nächsten Anmeldung wieder.
 * Erst wenn sie beantwortet ist, kommen die Hinweise zum letzten Update: zwei Fenster übereinander wären zu viel.
 */
export class LcmSessionModeDialog extends LitElement {
  static override properties = { _choice: { state: true }, _dismissed: { state: true }, _busy: { state: true }, _error: { state: true } };
  static override styles = [
    baseStyles,
    css`
      p {
        margin: 0 0 var(--space-4);
        color: var(--text-2);
      }
    `,
  ];

  declare _choice: string | undefined;
  declare _dismissed: boolean;
  declare _busy: boolean;
  declare _error: string;

  readonly #settings = new LiveData<GlobalSettings>(this, { url: () => (this.#mayAsk ? GLOBAL_URL : null), transform: readGlobal });

  constructor() {
    super();
    this._choice = undefined;
    this._dismissed = false;
    this._busy = false;
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  /** Wer die Einstellungen ändern darf und keinen Pflichtschritt mehr offen hat. */
  get #mayAsk(): boolean {
    return auth.isAuthenticated && auth.remediation === null && auth.can('settings:manage');
  }

  /** Der gespeicherte Weg; leer heißt Arbeitsspeicher. */
  get #saved(): string {
    return this.#settings.data?.browser_session_mode === 'cookie' ? 'cookie' : 'memory';
  }

  override willUpdate(): void {
    // Nach dem Abmelden gilt „später“ nicht mehr: bei der nächsten Anmeldung wird wieder gefragt.
    if (!auth.isAuthenticated && (this._dismissed || this._choice !== undefined)) {
      this._dismissed = false;
      this._choice = undefined;
      this._error = '';
    }
  }

  get #open(): boolean {
    const settings = this.#settings.data;
    return this.#mayAsk && !this._dismissed && settings !== undefined && settings.browser_session_mode_confirmed !== true;
  }

  async #apply(): Promise<void> {
    const saved = this.#saved;
    const mode = this._choice ?? saved;
    this._busy = true;
    this._error = '';
    try {
      await settingsApi.patchGlobal({ browser_session_mode: mode, browser_session_mode_confirmed: true });
    } catch (error) {
      this._error = errorText(error);
      return;
    } finally {
      this._busy = false;
    }
    // Der Server hat bei einem Wechsel alle Sitzungen beendet, auch diese.
    if (mode !== saved) await auth.leaveAfterSessionModeChange(router.state.path);
  }

  override render() {
    const open = this.#open;
    return html`<lcm-modal ?open=${open} size="m" heading=${t('settings.session.ask.title')} close-label=${t('settings.session.ask.close')} @lcm-close=${() => (this._dismissed = true)}>
      ${open
        ? html`<p>${t('settings.session.ask.lead')}</p>
            <lcm-session-mode-field
              .value=${this._choice ?? this.#saved}
              ?cookie-available=${this.#settings.data?.browser_session_cookie_available === true}
              @lcm-change=${(event: CustomEvent<{ value: string }>) => (this._choice = event.detail.value)}
            ></lcm-session-mode-field>`
        : nothing}
      ${open && this._error ? html`<lcm-notice slot="note" variant="line" tone="crit">${this._error}</lcm-notice>` : nothing}
      ${open
        ? html`<lcm-button slot="footer" variant="primary" ?busy=${this._busy} @click=${() => void this.#apply()}>${t('settings.session.ask.apply')}</lcm-button>`
        : nothing}
    </lcm-modal>
    ${!open && !this._busy && this.#mayAsk && this.#settings.data ? html`<lcm-release-notes-dialog></lcm-release-notes-dialog>` : nothing}`;
  }
}
customElements.define('lcm-session-mode-dialog', LcmSessionModeDialog);
