/**
 * CustomActionsApi - benutzerdefinierte Aktionen (wiederverwendbare
 * Command-Listen für Gruppen-Rules).
 */
export class CustomActionsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/custom-actions');
  }

  /** data: {name, description, commands} - commands: ein Kommando pro Zeile. */
  create(data) {
    return this.#client.post('/custom-actions', data);
  }

  update(id, data) {
    return this.#client.patch(`/custom-actions/${id}`, data);
  }

  remove(id) {
    return this.#client.delete(`/custom-actions/${id}`);
  }
}
