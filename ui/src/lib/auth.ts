// Angemeldeter Benutzer, Rechte, Anmelden und Abmelden. Hier steht, was `/auth/me`
// über den Benutzer sagt. Die Sitzung selbst hält api.ts: im Arbeitsspeicher
// (`session_token` aus der Login-Antwort, nur dieser Tab) oder, wenn der Betreiber
// die Cookie-Variante gewählt hat, im httpOnly-Cookie, das JavaScript nie sieht.

import { api, ApiError, endSessionOnUnload, onUnauthorized, setCsrfToken, setSessionToken } from './api';
import { broadcast, onBroadcast } from './broadcast';
import { events } from './events';
import { i18n } from './i18n';
import { remediationOf, type Remediation } from './remediation';
import { router } from './router';
import { loginHref } from './safe-path';
import { store } from './store';

export interface CurrentUser {
  id: number;
  username: string;
  email?: string;
  first_name?: string;
  last_name?: string;
  display_name?: string;
  roles: string[];
  permissions: string[];
  totp_enabled: boolean;
  must_change_password: boolean;
  must_setup_2fa: boolean;
  /** Wie der Browser die Sitzung trägt; fehlt bei einem Zugriff ohne Sitzung. */
  session_mode?: SessionMode;
  /** Nur bei der Cookie-Variante. */
  csrf_token?: string;
  language: string;
}

export type SessionMode = 'memory' | 'cookie';

export type LoginResult = { kind: 'done' } | { kind: 'two-factor'; challenge: string };

interface LoginResponse {
  twofa_required?: boolean;
  challenge?: string;
  /** Arbeitsspeicher-Variante: der Sitzungswert dieses Tabs. */
  session_token?: string;
}

const ME = '/api/v1/auth/me';
const anonymous = { anonymous: true };

export class Auth {
  #user: CurrentUser | null = null;
  #listeners = new Set<() => void>();

  get user(): CurrentUser | null {
    return this.#user;
  }

  get isAuthenticated(): boolean {
    return this.#user !== null;
  }

  can(permission: string): boolean {
    return this.#user?.permissions.includes(permission) ?? false;
  }

  /** Pflichtschritt, den der Benutzer vor allem anderen nachholen muss. */
  get remediation(): Remediation | null {
    return remediationOf(this.#user);
  }

  onChange(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  /** Wie diese Sitzung getragen wird (`null`, wenn niemand angemeldet ist). */
  get sessionMode(): SessionMode | null {
    return this.#user?.session_mode ?? null;
  }

  /**
   * Beim Start: Abmeldungen und Sitzungsende beobachten, vorhandene Sitzung laden. In der
   * Arbeitsspeicher-Variante gibt es nach dem Neuladen keine: der Tab beginnt abgemeldet.
   */
  async start(): Promise<void> {
    onUnauthorized(() => this.#expire());
    events.onAuthLost(() => this.#expire());
    onBroadcast((message) => {
      // Nur die Cookie-Sitzung gehört dem ganzen Browser. Jeder Tab der anderen Variante hat seine eigene.
      if (message.type === 'logout' && this.sessionMode === 'cookie') this.#leave();
    });
    // Geht der Tab, endet seine Sitzung auf dem Server (nur Arbeitsspeicher-Variante, sonst ohne Wirkung).
    window.addEventListener('pagehide', () => endSessionOnUnload());
    // Aus dem Seitenspeicher des Browsers zurückgeholt: Das pagehide hat die Sitzung beendet.
    window.addEventListener('pageshow', (event) => {
      if (event.persisted && this.sessionMode === 'memory') this.#expire();
    });
    try {
      await this.#loadUser();
    } catch (error) {
      if (!(error instanceof ApiError && error.status === 401)) console.error('auth: /auth/me failed', error);
    }
  }

  async login(username: string, password: string): Promise<LoginResult> {
    const response = await api.post<LoginResponse>('/api/v1/auth/login', { username, password }, anonymous);
    if (response.twofa_required && response.challenge) return { kind: 'two-factor', challenge: response.challenge };
    await this.#openSession(response);
    return { kind: 'done' };
  }

  async loginTwoFactor(challenge: string, code: string): Promise<void> {
    await this.#openSession(await api.post<LoginResponse>('/api/v1/auth/login/2fa', { challenge, code }, anonymous));
  }

  /** Übernimmt den Sitzungswert der Login-Antwort (Arbeitsspeicher-Variante) und lädt den Benutzer. */
  async #openSession(response: LoginResponse): Promise<void> {
    setSessionToken(response.session_token ?? null);
    await this.#loadUser();
  }

  /** Lädt den Benutzer neu, z. B. nachdem ein Pflichtschritt erledigt ist. */
  refresh(): Promise<void> {
    return this.#loadUser();
  }

  /** Meldet ab: dieser Tab sofort, bei der Cookie-Sitzung auch die anderen Tabs (BroadcastChannel). */
  async logout(): Promise<void> {
    const shared = this.sessionMode === 'cookie';
    try {
      await api.post('/api/v1/auth/logout', undefined, anonymous);
    } catch {
      // Die Sitzung war vielleicht schon abgelaufen, lokal wird trotzdem abgemeldet.
    }
    if (shared) broadcast({ type: 'logout' });
    this.#leave();
  }

  /**
   * Der Betreiber hat den Weg der Anmeldung gewechselt (Einstellungen → Allgemein): Der Server hat alle Sitzungen
   * beendet, auch diese. Zur Anmeldung, mit Erklärung und Rückkehr zur Seite.
   */
  async leaveAfterSessionModeChange(returnTo: string): Promise<void> {
    this.#forget();
    await router.navigate(loginHref(returnTo, 'session-mode'), { replace: true });
  }

  /** Sprache wählen und, wenn angemeldet, am Konto speichern (gilt auf allen Geräten). */
  async changeLanguage(language: string): Promise<void> {
    await i18n.setLanguage(language);
    if (this.#user) await api.patch('/api/v1/auth/me/preferences', { language }, anonymous);
  }

  async #loadUser(): Promise<void> {
    const user = await api.get<CurrentUser>(ME, anonymous);
    this.#user = user;
    setCsrfToken(user.csrf_token ?? null);
    if (user.language && user.language !== i18n.language && i18n.languages.includes(user.language)) {
      await i18n.setLanguage(user.language);
    }
    if (remediationOf(user)) events.stop(); // das Backend sperrt den Ereigniskanal im Pflichtschritt
    else events.start();
    this.#notify();
  }

  /** Sitzung abgelaufen (401 oder 4401): vergessen, dann sorgt der Router für `/login?next=…`. */
  #expire(): void {
    if (!this.#user) return;
    this.#forget();
    void router.reevaluate();
  }

  /** Bewusstes Abmelden: zur Anmeldung ohne Rückkehrziel. */
  #leave(): void {
    this.#forget();
    void router.navigate('/login', { replace: true });
  }

  #forget(): void {
    this.#user = null;
    setCsrfToken(null);
    setSessionToken(null);
    events.stop();
    store.clear();
    this.#notify();
  }

  #notify(): void {
    this.#listeners.forEach((listener) => listener());
  }
}

export const auth = new Auth();
