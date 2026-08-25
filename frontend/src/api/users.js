/**
 * UsersApi - User- und Rollen-Verwaltung (benötigt users/roles-Permissions).
 * Beispiel: const users = await api.users.getAll();
 */
export class UsersApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  getAll() {
    return this.#client.get('/users');
  }

  create({ username, email, password, firstName, lastName, roles }) {
    return this.#client.post('/users', {
      username,
      email,
      password,
      first_name: firstName,
      last_name: lastName,
      roles,
    });
  }

  updateRoles(id, roles) {
    return this.#client.put(`/users/${id}/roles`, { roles });
  }

  // Profil bearbeiten (E-Mail, Name) - Admin für alle, jeder für sich selbst.
  updateProfile(id, { email, firstName, lastName }) {
    return this.#client.patch(`/users/${id}/profile`, {
      email,
      first_name: firstName,
      last_name: lastName,
    });
  }

  // Passwort zurücksetzen - Admin für alle, jeder für sich selbst.
  // Beim eigenen Passwort verlangt der Server eine Re-Authentifizierung:
  // currentPassword (und bei aktivem TOTP einen code). Beim Admin-Reset
  // fremder Nutzer bleiben beide leer.
  resetPassword(id, password, currentPassword = '', code = '') {
    return this.#client.post(`/users/${id}/reset-password`, {
      password,
      current_password: currentPassword,
      code,
    });
  }

  /** Konto sperren/entsperren, ohne es zu löschen (R2-036). */
  setActive(id, active) {
    return this.#client.patch(`/users/${id}/active`, { active });
  }

  remove(id) {
    return this.#client.delete(`/users/${id}`);
  }

  // Aktivierungs-/Einladungslink für einen User erzeugen. sendEmail
  // verschickt ihn zusätzlich über den Standard-E-Mail-Versand.
  generateActivationLink(userId, { ttlHours = 0, sendEmail = false } = {}) {
    return this.#client.post('/users/activation-links/generate', {
      user_id: userId,
      ttl_hours: ttlHours,
      send_email: sendEmail,
    });
  }

  // Öffentlich: Aktivierungs-/Reset-Link einlösen und Passwort setzen.
  consumeActivationLink(token, password) {
    return this.#client.post('/users/activation-links/consume', { token, password });
  }

  getRoles() {
    return this.#client.get('/roles');
  }
}

