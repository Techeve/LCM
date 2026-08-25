/**
 * NotificationsApi - Benachrichtigungskanäle (Provider-Instanzen des
 * generischen Notification-Service, MVP: E-Mail/SMTP).
 */
export class NotificationsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/notification-channels');
  }

  /**
   * data: {name, type, enabled, config, secret}
   * config ist ein provider-spezifisches Objekt (E-Mail:
   * {host, port, username, from, recipients, use_tls}).
   * secret ist der sensible Teil (SMTP-Passwort); leer = unverändert.
   */
  create(data) {
    return this.#client.post('/notification-channels', data);
  }

  update(id, data) {
    return this.#client.patch(`/notification-channels/${id}`, data);
  }

  remove(id) {
    return this.#client.delete(`/notification-channels/${id}`);
  }

  /** Versendet eine Test-Benachrichtigung über den Kanal. */
  test(id) {
    return this.#client.post(`/notification-channels/${id}/test`);
  }
}
