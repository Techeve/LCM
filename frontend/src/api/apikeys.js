/**
 * ApiKeysApi - API-Key-Verwaltung (benötigt apikeys:manage).
 */
export class ApiKeysApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  getAll() {
    return this.#client.get('/apikeys');
  }

  /**
   * Rückgabe enthält den Klartext-Key GENAU EINMAL ({ key, api_key }).
   * scope: 'read' (nur GET/HEAD), 'readwrite' oder 'mcp' (nur MCP-Schnittstelle).
   */
  create(name, scope = 'readwrite', expiresInDays = 0) {
    // Unbefristet = Feld WEGLASSEN. Der Endpunkt weist expires_in_days <= 0
    // jetzt ab (R2-050: negative Werte ergaben früher still einen
    // unbefristeten Key).
    const body = { name, scope };
    if (expiresInDays > 0) body.expires_in_days = expiresInDays;
    return this.#client.post('/apikeys', body);
  }

  revoke(id) {
    return this.#client.delete(`/apikeys/${id}`);
  }
}
