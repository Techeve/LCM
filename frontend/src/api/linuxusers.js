/**
 * LinuxUsersApi - Betriebssystem-Benutzer der verwalteten Server
 * (getrennt von den LCM-Login-Benutzern).
 */
export class LinuxUsersApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/linux-users');
  }

  get(id) {
    return this.#client.get(`/linux-users/${id}`);
  }

  create({ username, fullName, email, shell, sudo }) {
    return this.#client.post('/linux-users', {
      username,
      full_name: fullName,
      email,
      shell,
      sudo,
    });
  }

  update(id, { fullName, email, shell, sudo, active, defaultProfileId }) {
    return this.#client.patch(`/linux-users/${id}`, {
      full_name: fullName,
      email,
      shell,
      sudo,
      active,
      default_profile_id: defaultProfileId,
    });
  }

  // Deprovisioniert den Benutzer aktiv von allen Servern (userdel) und löst
  // die Zuordnungen - Voraussetzung fürs Löschen.
  removeFromServers(id) {
    return this.#client.post(`/linux-users/${id}/remove-from-servers`);
  }

  remove(id) {
    return this.#client.delete(`/linux-users/${id}`);
  }

  // ---- SSH-Keys ----
  addKey(id, name, publicKey) {
    return this.#client.post(`/linux-users/${id}/keys`, { name, public_key: publicKey });
  }

  // Erzeugt serverseitig ein ed25519-Schlüsselpaar; die Antwort enthält den
  // privaten Schlüssel GENAU EINMAL (wird nie gespeichert) plus den Key-Datensatz.
  generateKey(id, name) {
    return this.#client.post(`/linux-users/${id}/keys/generate`, { name });
  }

  removeKey(keyId) {
    return this.#client.delete(`/linux-users/keys/${keyId}`);
  }

  // ---- Zuordnung zu Gruppen ----
  assignGroup(id, groupId, profileId) {
    return this.#client.post(`/linux-users/${id}/assign-group`, { group_id: groupId, profile_id: profileId });
  }

  // Profil einer bestehenden Gruppen-Zuweisung ändern (null = Standardprofil
  // des Benutzers).
  setGroupProfile(id, groupId, profileId) {
    return this.#client.post(`/linux-users/${id}/group-profile`, { group_id: groupId, profile_id: profileId });
  }

  groupAssignments(id) {
    return this.#client.get(`/linux-users/${id}/group-assignments`);
  }

  removeGroup(id, groupId) {
    return this.#client.post(`/linux-users/${id}/remove-group`, { group_id: groupId });
  }

  // ---- Aktivierungslinks (Self-Service Credentials) ----
  generateActivation(id, ttlHours) {
    return this.#client.post(`/linux-users/${id}/activation-links/generate`, { ttl_hours: ttlHours });
  }

  consumeActivation({ token, password, keyName, publicKey, generateKey }) {
    return this.#client.post('/linux-users/activation-links/consume', {
      token,
      password,
      key_name: keyName,
      public_key: publicKey,
      generate_key: generateKey ?? false,
    });
  }
}
