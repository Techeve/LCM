/**
 * GroupsApi - Servergruppen, Rules und Schedules.
 */
export class GroupsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  list() {
    return this.#client.get('/server-groups');
  }

  get(id) {
    return this.#client.get(`/server-groups/${id}`);
  }

  create(name, description, priority) {
    return this.#client.post('/server-groups/create', { name, description, priority });
  }

  updateSettings(id, name, description, priority) {
    return this.#client.patch(`/server-groups/${id}/settings`, { name, description, priority });
  }

  assignServer(id, serverId) {
    return this.#client.post(`/server-groups/${id}/assign-server`, { server_id: serverId });
  }

  removeServer(id, serverId) {
    return this.#client.post(`/server-groups/${id}/remove-server`, { server_id: serverId });
  }

  /** Gruppe auflösen (löschen). System-Gruppe ist geschützt (409). */
  disband(id) {
    return this.#client.post(`/server-groups/${id}/disband`, {});
  }

  // ---- Schedules (Zeitpläne mit mehreren Rules) ----
  listSchedules(groupId) {
    return this.#client.get(`/server-groups/${groupId}/schedules`);
  }

  defineSchedule(groupId, data) {
    return this.#client.post(`/server-groups/${groupId}/schedules/define`, data);
  }

  updateSchedule(scheduleId, data) {
    return this.#client.patch(`/schedules/${scheduleId}/update`, data);
  }

  removeSchedule(scheduleId) {
    return this.#client.delete(`/schedules/${scheduleId}/remove`);
  }

  enableSchedule(scheduleId) {
    return this.#client.post(`/schedules/${scheduleId}/enable`);
  }

  disableSchedule(scheduleId) {
    return this.#client.post(`/schedules/${scheduleId}/disable`);
  }

  /** Führt alle Rules des Schedules sofort aus. */
  triggerSchedule(scheduleId) {
    return this.#client.post(`/schedules/${scheduleId}/trigger-now`);
  }

  // ---- Rules ----
  listRules(groupId) {
    return this.#client.get(`/server-groups/${groupId}/rules`);
  }

  /** data: {name, type, command, schedule_id} ODER {name, type, command, enforce: true}. */
  defineRule(groupId, data) {
    return this.#client.post(`/server-groups/${groupId}/rules/define`, data);
  }

  updateRule(ruleId, data) {
    return this.#client.patch(`/rules/${ruleId}/update`, data);
  }

  removeRule(ruleId) {
    return this.#client.delete(`/rules/${ruleId}/remove`);
  }

  enableRule(ruleId) {
    return this.#client.post(`/rules/${ruleId}/enable`);
  }

  disableRule(ruleId) {
    return this.#client.post(`/rules/${ruleId}/disable`);
  }

  triggerRule(ruleId) {
    return this.#client.post(`/rules/${ruleId}/trigger-now`);
  }
}
