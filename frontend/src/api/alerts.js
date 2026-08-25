/**
 * AlertsApi - Alarm-Regeln (Monitoring-/Trigger-Kriterien), Alarm-Historie
 * und die manuelle Auswertung.
 */
export class AlertsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  listRules() {
    return this.#client.get('/alert-rules');
  }

  /**
   * data: {name, type, enabled, group_ids, channel_id, severity,
   *        threshold_percent, forecast_days, max_outdated, min_severity,
   *        heartbeat_hours, cooldown_minutes}
   * type: disk_capacity | storage_forecast | security_cve | failed_updates | heartbeat
   */
  createRule(data) {
    return this.#client.post('/alert-rules', data);
  }

  updateRule(id, data) {
    return this.#client.patch(`/alert-rules/${id}`, data);
  }

  removeRule(id) {
    return this.#client.delete(`/alert-rules/${id}`);
  }

  /**
   * Alarm-Historie (ausgelöste Events, neueste zuerst), seitenweise.
   * Rückgabe: {items, total, page, page_size} (R2-023 - vorher fest die
   * neuesten 200 ohne Paging). Ohne Argumente kompatibel: erste Seite.
   */
  events({ page = 1, pageSize = 100, type, severity, serverId } = {}) {
    const q = new URLSearchParams({ page, page_size: pageSize });
    if (type) q.set('type', type);
    if (severity) q.set('severity', severity);
    if (serverId) q.set('server_id', serverId);
    return this.#client.get(`/alert-events?${q}`);
  }

  /** Löst sofort eine Auswertung aller aktiven Alarm-Regeln aus. */
  evaluate() {
    return this.#client.post('/alerts/evaluate');
  }
}
