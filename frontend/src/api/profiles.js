/**
 * ProfilesApi - Berechtigungsprofile für Linux-Benutzer: benannte
 * Rechtebündel aus sudo-Kommandos, bearbeitbaren Dateien und
 * Verzeichnisrechten.
 */
export class ProfilesApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/privilege-profiles');
  }

  get(id) {
    return this.#client.get(`/privilege-profiles/${id}`);
  }

  /** data: {name, slug, description, sudo_rules, edit_rules, path_rules} */
  create(data) {
    return this.#client.post('/privilege-profiles', data);
  }

  /** Änderbare Kopie eines Profils anlegen (auch von mitgelieferten). */
  clone(id, slug, name) {
    return this.#client.post(`/privilege-profiles/${id}/clone`, { slug, name });
  }

  /** Der Slug ist unveränderlich und wird beim Ändern nicht ausgewertet. */
  update(id, data) {
    return this.#client.patch(`/privilege-profiles/${id}`, data);
  }

  remove(id) {
    return this.#client.delete(`/privilege-profiles/${id}`);
  }
}
