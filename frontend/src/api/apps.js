/**
 * AppsApi - Anwendungskatalog: Steckbriefe der Software, die nicht über die
 * Paketverwaltung installiert wird (AdGuard Home, Nextcloud, mailcow …).
 * Je Eintrag steht darin, woran man die Anwendung erkennt, wie man ihre
 * Version erfährt und wo die neueste steht.
 */
export class AppsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/apps');
  }

  create(data) {
    return this.#client.post('/apps', data);
  }

  update(id, data) {
    return this.#client.put(`/apps/${id}`, data);
  }

  remove(id) {
    return this.#client.delete(`/apps/${id}`);
  }
}
