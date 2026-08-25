/**
 * JobsApi - Job-Historie, SSH-Konsolen-Output und Audit-Log.
 */
export class JobsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  /**
   * Gefilterte, seitenweise Job-Historie. serverId optional; opts kann
   * {page, page_size, status, type, q, triggered_by, hide_health, from, to}
   * enthalten. Antwort: {items, total, page, page_size}.
   */
  history(serverId, opts = {}) {
    const params = new URLSearchParams();
    if (serverId) params.set('server_id', serverId);
    for (const [k, v] of Object.entries(opts)) {
      if (v !== '' && v !== null && v !== undefined && v !== false) params.set(k, v);
    }
    const q = params.toString();
    return this.#client.get(`/jobs/history${q ? `?${q}` : ''}`);
  }

  /** Distinkte Typen und Auslöser für die Filter-Dropdowns. */
  filterOptions(serverId) {
    return this.#client.get(`/jobs/filter-options${serverId ? `?server_id=${serverId}` : ''}`);
  }

  /** Exakter Stdout/Stderr-Output eines Jobs zur Fehleranalyse. */
  consoleOutput(id) {
    return this.#client.get(`/jobs/${id}/ssh-output`);
  }

  /** SSH-Protokoll-Sessions (mit Kommandos) einer Job-Ausführung. */
  sshSessions(id) {
    return this.#client.get(`/jobs/${id}/ssh-sessions`);
  }

  /** Bricht einen laufenden Job manuell ab (hebt die Server-Sperre auf). */
  abort(id) {
    return this.#client.post(`/jobs/${id}/abort`);
  }
}
