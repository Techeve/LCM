/**
 * ProfileBlocksApi - Regelbausteine: wiederverwendbare Rechte-Vorlagen, aus
 * denen sich Berechtigungsprofile zusammensetzen lassen. Je Baustein gibt es
 * Varianten pro Distributions-Familie und Parameter für Dienstnamen/Pfade.
 */
export class ProfileBlocksApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/profile-blocks');
  }

  /** Welche Profile den Baustein verwenden - der Nachweis vor einer Änderung. */
  usage(id) {
    return this.#client.get(`/profile-blocks/${id}/usage`);
  }

  /** Zeigt die Zeilen, die der Baustein für eine Familie ergibt. */
  preview(id, family, values) {
    return this.#client.post(`/profile-blocks/${id}/preview`, { family, values });
  }

  create(data) {
    return this.#client.post('/profile-blocks', data);
  }

  clone(id, slug, name) {
    return this.#client.post(`/profile-blocks/${id}/clone`, { slug, name });
  }

  update(id, data) {
    return this.#client.patch(`/profile-blocks/${id}`, data);
  }

  remove(id) {
    return this.#client.delete(`/profile-blocks/${id}`);
  }
}
