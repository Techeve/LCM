import { html, type TemplateResult } from 'lit';
import '../settings/session-mode-field';
import { auth } from '../../lib/auth';
import { router } from '../../lib/router';
import { sessionBody, sessionModeOf } from './setup-model';
import { SetupStep } from './setup-step';

/**
 * Schritt „Anmeldung im Browser“: die Wahl zwischen Arbeitsspeicher und Sitzungs-Cookie (`lcm-session-mode-field`, wie in
 * Einstellungen → Allgemein). Das Fenster, das sonst nach den Pflichtschritten aufgeht, ist damit dieser Schritt. Ein
 * Wechsel des Wegs beendet alle Sitzungen, auch diese: Danach geht es über die Anmeldung beim nächsten Schritt weiter.
 */
export class SetupSession extends SetupStep {
  static override properties = { ...SetupStep.properties, _choice: { state: true } };

  readonly step = 'session' as const;
  declare _choice: string | undefined;

  protected override async save(): Promise<void> {
    const saved = sessionModeOf(this.settings);
    const mode = this._choice ?? saved;
    // Jetzt merken: Meldet der Ereigniskanal das Ende der Sitzung vor der Antwort, steht der Router schon auf der Anmeldung.
    const returnTo = router.state.path;
    const updated = await this.patch(sessionBody(mode));
    if (mode === saved) this.done(updated);
    else await auth.leaveAfterSessionModeChange(returnTo);
  }

  protected override body(): TemplateResult {
    return html`<lcm-session-mode-field
      data-field="session-mode"
      .value=${this._choice ?? sessionModeOf(this.settings)}
      ?cookie-available=${this.settings.browser_session_cookie_available === true}
      @lcm-change=${(event: CustomEvent<{ value: string }>) => (this._choice = event.detail.value)}
    ></lcm-session-mode-field>`;
  }
}
customElements.define('lcm-setup-session', SetupSession);
