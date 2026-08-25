/**
 * AuthApi - Login (inkl. 2FA), Logout, Profil und 2FA-Verwaltung.
 */
export class AuthApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  /**
   * Login-Schritt 1. Liefert eines von:
   *   { twofa_required: true, challenge }   -> Schritt 2 nötig
   *   { done: true, user, must_setup_2fa }  -> eingeloggt
   */
  async login(username, password) {
    const res = await this.#client.post('/auth/login', { username, password });
    if (res.twofa_required) {
      return { twofa_required: true, challenge: res.challenge };
    }
    this.#client.startSession(res.token, res.user);
    return { done: true, user: res.user, must_setup_2fa: res.must_setup_2fa };
  }

  /** Login-Schritt 2: TOTP-Code gegen die Challenge verifizieren. */
  async loginTotp(challenge, code) {
    const { token, user } = await this.#client.post('/auth/login/2fa', { challenge, code });
    this.#client.startSession(token, user);
    return user;
  }

  /**
   * Self-Service-Passwort-Reset anstoßen (öffentlich). Die Antwort ist
   * bewusst immer gleich - ob die Adresse existiert, verrät der Server nicht.
   */
  requestPasswordReset(email) {
    return this.#client.post('/auth/password-reset', { email });
  }

  async logout() {
    try {
      await this.#client.post('/auth/logout');
    } catch {
      /* Token evtl. schon abgelaufen - lokaler Logout genügt */
    }
    this.#client.logout();
  }

  // ---- 2FA-Verwaltung ----
  setup2fa() {
    return this.#client.post('/auth/2fa/setup');
  }

  enable2fa(code) {
    return this.#client.post('/auth/2fa/enable', { code });
  }

  disable2fa(code) {
    return this.#client.post('/auth/2fa/disable', { code });
  }
}
