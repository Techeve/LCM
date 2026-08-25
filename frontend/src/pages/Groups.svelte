<script>
  // Servergruppen mit Schedules & Rules. Erfassungs- und Bearbeitungsmasken
  // öffnen sich als Popups (Modals) über „+"- bzw. Bearbeiten-Schaltflächen;
  // die Seite selbst zeigt nur die Tabellen (Server, Schedules, Rules).
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import Modal from '../components/Modal.svelte';
  import FirewallRulesEditor from '../components/FirewallRulesEditor.svelte';
  import CronBuilder from '../components/CronBuilder.svelte';
  import { icons } from '../lib/icons.js';
  import { toasts } from '../stores/toast.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let groups = $state([]);
  let servers = $state([]);
  let selected = $state(null);
  let schedules = $state([]);
  let rules = $state([]);
  let customActions = $state([]);
  let addServerId = $state('');

  // Modal-Zustände.
  let groupOpen = $state(false);
  // Vorrang einer Gruppe: kleinere Zahl gewinnt, wenn für einen Server
  // mehrere Gruppen dasselbe regeln (siehe domain.DefaultGroupPriority).
  const DEFAULT_PRIORITY = 100;

  let groupForm = $state({ id: null, name: '', description: '', priority: DEFAULT_PRIORITY });

  let schedOpen = $state(false);
  let schedForm = $state({ id: null, name: '', cron_expr: '0 3 * * *' });

  let ruleOpen = $state(false);
  let ruleForm = $state({ id: null, name: '', type: 'update', command: '', target: '' });
  // Firewall-Regel-Objekte des Editors ({port, proto, ip_version, …}) -
  // werden beim Speichern als JSON in ruleForm.command serialisiert.
  let fwRules = $state([]);
  let fwEditor = $state(); // Editor-Instanz (Client-Validierung)
  let ipAllowlists = $state([]); // benannte IP-Allowlists (Auswahl in Firewall-Regeln)

  // Ableitungen für das Rule-Modal.
  let ruleIsEnforce = $derived(ruleForm.target === 'enforce');
  let ruleIsEdit = $derived(ruleForm.id !== null);
  let ruleTypeOptions = $derived(
    // Grundsatz-Regeln tragen einen Soll-Zustand, gegen den geprüft wird -
    // ein Shell-Kommando hat keinen und lief hier unprotokolliert alle 15
    // Minuten (R2-087). „script" gibt es daher nur noch an Zeitplänen.
    ruleIsEnforce
      ? [
          ['firewall', t('groups.types.firewall')],
          ['apt-proxy', t('groups.types.aptProxyEnforce')],
          ['acl-setup', t('groups.types.aclSetup')],
          ['perm-sync', t('groups.types.permSync')],
        ]
      : [
          ['update', t('groups.types.update')],
          ['packages', t('groups.types.packages')],
          ['security', t('groups.types.security')],
          ['package-scan', t('groups.types.packageScan')],
          ['autoremove', t('groups.types.autoremove')],
          ['script', t('groups.types.script')],
          ['custom', t('groups.types.custom')],
          ['sync', t('groups.types.sync')],
          ['health', t('groups.types.health')],
          ['docker-prune', t('groups.types.dockerPrune')],
          ['docker-update-unused', t('groups.types.dockerUpdateUnused')],
          ['reboot', t('groups.types.reboot')],
          ['reboot-if-needed', t('groups.types.rebootIfNeeded')],
          ['apt-proxy', t('groups.types.aptProxy')],
          ['dns-test', t('groups.types.dnsTest')],
          ['deep-scan', t('groups.types.deepScan')],
        ],
  );

  // Speicherbarkeit der Rule: Name Pflicht; „Paket-Updates" brauchen einen
  // Paketnamen, „Custom-Aktion" eine gewählte Aktion (beides im command-Feld).
  let ruleValid = $derived(
    !!ruleForm.name.trim() &&
      (ruleForm.type !== 'packages' || !!ruleForm.command.trim()) &&
      (ruleForm.type !== 'custom' || !!ruleForm.command),
  );

  // Server, die noch NICHT in der gewählten Gruppe sind (Dedup).
  let availableServers = $derived(
    selected
      ? servers.filter((s) => !(selected.servers ?? []).some((m) => m.id === s.id))
      : [],
  );

  // Anzeige-Name des Ziels einer Rule (Schedule-Name oder „Grundsatz").
  function ruleTarget(r) {
    if (r.enforce) return t('groups.policy');
    const s = schedules.find((x) => x.id === r.schedule_id);
    return s ? s.name : '-';
  }

  function customActionName(id) {
    const a = customActions.find((x) => String(x.id) === String(id));
    return a ? a.name : `#${id}`;
  }

  // Firewall-Command (JSON-Regeln oder Legacy-CSV) → Regel-Objekte.
  function parseFirewallCommand(cmd) {
    const s = (cmd || '').trim();
    if (s.startsWith('[')) {
      try {
        return JSON.parse(s).map((r) => ({
          port: r.port,
          proto: r.proto || 'tcp',
          ip_version: r.ip_version || 'any',
          allowlist_ids: r.allowlist_ids ?? [],
          source_ips: r.source_ips ?? [],
          comment: r.comment ?? '',
        }));
      } catch {
        return [];
      }
    }
    return s
      .split(',')
      .map((p) => Number(p.trim()))
      .filter((p) => Number.isInteger(p) && p > 0)
      .map((p) => ({ port: p, proto: 'tcp', ip_version: 'any', allowlist_ids: [], source_ips: [], comment: '' }));
  }

  // Kurzfassung für die Regel-Tabelle: "80/tcp, 443/tcp (Webshop)".
  function firewallSummary(cmd) {
    const rules = parseFirewallCommand(cmd);
    if (!rules.length) return '-';
    return rules.map((r) => `${r.port}/${r.proto}${r.comment ? ` (${r.comment})` : ''}`).join(', ');
  }

  function ruleConfig(r) {
    if (r.type === 'firewall') return t('groups.configPorts', { ports: firewallSummary(r.command) });
    if (r.type === 'custom') return t('groups.configAction', { name: customActionName(r.command) });
    if (r.type === 'autoremove') return t('groups.configAutoremove');
    if (r.type === 'package-scan') return t('groups.configPackageScan');
    if (r.type === 'docker-prune') return 'docker image prune -af';
    if (r.type === 'docker-update-unused') return t('groups.configDockerUpdateUnused');
    if (r.type === 'reboot') return t('groups.configReboot');
    if (r.type === 'reboot-if-needed') return t('groups.configRebootIfNeeded');
    if (r.type === 'apt-proxy') return t('groups.configAptProxy');
    if (r.type === 'dns-test') return t('groups.configDnsTest');
    if (r.type === 'deep-scan') return t('groups.configDeepScan');
    return r.command || '-';
  }

  async function load() {
    try {
      groups = await api.groups.list();
      servers = await api.servers.list();
      if (auth.can('rules:manage')) customActions = await api.customActions.list();
      // Allowlists für die Quell-Auswahl in Firewall-Grundsatz-Regeln (best effort).
      ipAllowlists = await api.servers.ipAllowlists().catch(() => []);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function select(g) {
    selected = await api.groups.get(g.id);
    schedules = await api.groups.listSchedules(g.id);
    rules = await api.groups.listRules(g.id);
  }

  // Jede Aktion dieser Seite laeuft ueber run(). busy sperrt dabei ALLE
  // Aktionsknoepfe: „Jetzt" auf einem Schedule stoesst einen Lauf ueber die
  // ganze Gruppe an - ein zweiter Klick vor der Antwort loest ihn ein zweites
  // Mal aus. Die Rueckmeldung geht in die fixierte Toast-Region, weil die
  // Knoepfe (Schedules/Rules) weit unterhalb eines Alerts am Seitenanfang
  // liegen.
  let busy = $state(false);
  async function run(fn, msg) {
    if (busy) return;
    busy = true;
    toasts.clear();
    try {
      await fn();
      toasts.success(msg);
      await load();
      if (selected) await select(selected);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  // ---- Gruppe -------------------------------------------------------------
  function openNewGroup() {
    groupForm = { id: null, name: '', description: '', priority: DEFAULT_PRIORITY };
    groupOpen = true;
  }
  function openEditGroup() {
    groupForm = {
      id: selected.id,
      name: selected.name,
      description: selected.description ?? '',
      priority: selected.priority ?? DEFAULT_PRIORITY,
    };
    groupOpen = true;
  }
  // R2-066: Eine Gruppe ließ sich anlegen, aber nirgends wieder löschen -
  // der Disband-Endpunkt existierte, hatte aber keinen Knopf.
  async function disbandGroup() {
    if (!confirm(t('groups.confirms.disband', { name: selected.name }))) return;
    const id = selected.id;
    await run(() => api.groups.disband(id), t('groups.notices.disbanded'));
    selected = null;
  }
  async function saveGroup() {
    const f = { ...groupForm };
    const priority = Number(f.priority) || DEFAULT_PRIORITY;
    if (f.id) {
      await run(() => api.groups.updateSettings(f.id, f.name, f.description, priority), t('groups.notices.groupSaved'));
    } else {
      await run(() => api.groups.create(f.name, f.description, priority), t('groups.notices.groupCreated'));
    }
    groupOpen = false;
  }

  async function addServer() {
    if (!addServerId) return;
    await run(() => api.groups.assignServer(selected.id, Number(addServerId)), t('groups.notices.serverAdded'));
    addServerId = '';
  }

  // ---- Schedule -----------------------------------------------------------
  function openNewSchedule() {
    schedForm = { id: null, name: '', cron_expr: '0 3 * * *' };
    schedOpen = true;
  }
  function openEditSchedule(s) {
    schedForm = { id: s.id, name: s.name, cron_expr: s.cron_expr };
    schedOpen = true;
  }
  async function saveSchedule() {
    const f = { ...schedForm };
    if (f.id) {
      await run(() => api.groups.updateSchedule(f.id, { name: f.name, cron_expr: f.cron_expr }), t('groups.notices.schedSaved'));
    } else {
      await run(() => api.groups.defineSchedule(selected.id, { name: f.name, cron_expr: f.cron_expr }), t('groups.notices.schedCreated'));
    }
    schedOpen = false;
  }

  // ---- Rule ---------------------------------------------------------------
  function openNewRule() {
    const first = schedules[0];
    ruleForm = { id: null, name: '', type: 'update', command: '', target: first ? `sched:${first.id}` : 'enforce' };
    fwRules = [];
    ruleOpen = true;
  }
  function openEditRule(r) {
    ruleForm = {
      id: r.id, name: r.name, type: r.type, command: r.command,
      target: r.enforce ? 'enforce' : `sched:${r.schedule_id}`,
    };
    fwRules = parseFirewallCommand(r.type === 'firewall' ? r.command : '');
    ruleOpen = true;
  }
  // Beim Wechsel des Ziels ggf. den Typ auf einen erlaubten Wert normalisieren.
  function onTargetChange() {
    const allowed = ruleTypeOptions.map((o) => o[0]);
    if (!allowed.includes(ruleForm.type)) ruleForm.type = allowed[0];
  }
  async function saveRule() {
    const f = { ...ruleForm };
    // Firewall-Regeln aus dem Editor als JSON in den Command serialisieren.
    if (f.type === 'firewall') {
      // Ungültige Zeilen im Editor? Dann nicht speichern (Markierung zeigt der Editor).
      if (fwEditor?.invalidRows()) return;
      f.command = JSON.stringify(
        fwRules.map((r) => ({
          port: Number(r.port),
          proto: r.proto || 'tcp',
          ip_version: r.ip_version || 'any',
          allowlist_ids: (r.allowlist_ids ?? []).length ? r.allowlist_ids : undefined,
          source_ips: (r.source_ips ?? []).length ? r.source_ips : undefined,
          comment: r.comment?.trim() || undefined,
        })),
      );
    }
    if (f.id) {
      await run(() => api.groups.updateRule(f.id, { name: f.name, command: f.command }), t('groups.notices.ruleSaved'));
    } else {
      const data = { name: f.name, type: f.type, command: f.command };
      if (f.target === 'enforce') data.enforce = true;
      else data.schedule_id = Number(f.target.slice('sched:'.length));
      await run(() => api.groups.defineRule(selected.id, data), t('groups.notices.ruleCreated'));
    }
    ruleOpen = false;
  }

  $effect(() => {
    if (auth.isLoggedIn) load();
  });
</script>

<div class="container">
  <h1 class="h3 mb-4">{t('groups.title')}</h1>

  <div class="row">
    <div class="col-md-3">
      <div class="d-flex justify-content-between align-items-center mb-2">
        <h2 class="h6 mb-0">{t('groups.listTitle')}</h2>
        {#if auth.can('groups:write')}
          <button class="btn btn-sm btn-primary" onclick={openNewGroup} title={t('groups.newGroup')}>+</button>
        {/if}
      </div>
      <div class="list-group">
        {#each groups as g (g.id)}
          <button class="list-group-item list-group-item-action {selected?.id === g.id ? 'active' : ''}" onclick={() => select(g)}>
            {g.name}
            {#if g.is_system}<span class="badge bg-secondary float-end">{t('groups.system')}</span>{/if}
          </button>
        {/each}
      </div>
    </div>

    <div class="col-md-9">
      {#if selected}
        <!-- Detailbereich als Karte, damit er sich wie überall sonst als
             Fläche vom Seitenhintergrund abhebt (statt frei zu schweben). -->
        <div class="card">
          <div class="card-body">
        <div class="d-flex align-items-center gap-2">
          <h2 class="h5 mb-0">{selected.name}</h2>
          {#if auth.can('groups:write') && !selected.is_system}
            <button class="btn btn-sm btn-outline-secondary py-0" title={t('groups.editGroup')} aria-label={t('groups.editGroup')} onclick={openEditGroup}>{@html icons.pencil}</button>
            <button class="btn btn-sm btn-outline-danger py-0" title={t('groups.disbandGroup')} aria-label={t('groups.disbandGroup')} data-testid="group-disband" onclick={disbandGroup}>{@html icons.trash}</button>
          {/if}
        </div>
        <p class="text-body-secondary mt-1 mb-1">{selected.description}</p>
        <!-- Der Vorrang entscheidet bei Grundsatz-Regeln, welche Gruppe sich
             durchsetzt - er gehört sichtbar an die Gruppe, nicht nur in den
             Bearbeiten-Dialog. -->
        <p class="small text-body-secondary mb-1">
          {t('groups.priority')}: <strong>{selected.priority ?? DEFAULT_PRIORITY}</strong>
          <span class="ms-1">{t('groups.priorityShortHint')}</span>
        </p>
        <!-- ACL-Zustand: entscheidet, ob ein Profil mit Verzeichnisrechten auf
             dieser Gruppe überhaupt wirken kann. -->
        {#if (selected.acl_servers ?? 0) > 0}
          <p class="small">
            {#if selected.acl_capable === selected.acl_servers}
              <span class="badge text-bg-success">{t('groups.aclFull', { total: selected.acl_servers })}</span>
            {:else if selected.acl_capable > 0}
              <span class="badge text-bg-warning">{t('groups.aclPartial', { ok: selected.acl_capable, total: selected.acl_servers })}</span>
            {:else}
              <span class="badge text-bg-secondary">{t('groups.aclNone')}</span>
            {/if}
            <span class="text-body-secondary ms-1">{t('groups.aclHint')}</span>
          </p>
        {/if}

        <!-- Server-Tabelle -->
        <div class="d-flex justify-content-between align-items-center mb-2">
          <h3 class="h6 mb-0">{t('groups.serversHeading')} ({selected.servers?.length ?? 0})</h3>
          {#if auth.can('groups:write')}
            <div class="input-group input-group-sm" style="max-width: 340px">
              <select class="form-select" bind:value={addServerId}>
                <option value="">{t('groups.addServerPlaceholder')}</option>
                {#each availableServers as s (s.id)}<option value={s.id}>{s.name} ({s.host})</option>{/each}
              </select>
              <button class="btn btn-primary" aria-label={t('groups.a11y.addServer')} onclick={addServer} disabled={!addServerId || busy}>+</button>
            </div>
          {/if}
        </div>
        <div class="table-responsive mb-4">
          <table class="table table-sm align-middle">
            <thead><tr><th>{t('groups.colName')}</th><th>{t('groups.colHost')}</th>{#if auth.can('groups:write')}<th></th>{/if}</tr></thead>
            <tbody>
              {#each selected.servers ?? [] as s (s.id)}
                <tr>
                  <td>{s.name}</td>
                  <td class="text-body-secondary small">{s.host}</td>
                  {#if auth.can('groups:write')}
                    <td class="text-end"><button class="btn btn-sm btn-outline-danger py-0" aria-label={t('groups.a11y.removeServer')} disabled={busy} onclick={() => run(() => api.groups.removeServer(selected.id, s.id), t('groups.notices.serverRemoved'))}>×</button></td>
                  {/if}
                </tr>
              {:else}
                <tr><td colspan="3" class="text-body-secondary small">{t('groups.noServersAssigned')}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>

        <!-- Schedules-Tabelle -->
        <div class="d-flex justify-content-between align-items-center mb-2">
          <h3 class="h6 mb-0">{t('groups.schedulesTitle')}</h3>
          {#if auth.can('rules:manage')}
            <button class="btn btn-sm btn-primary" onclick={openNewSchedule} title={t('groups.newSchedule')}>+</button>
          {/if}
        </div>
        <p class="small text-body-secondary">{t('groups.schedulesIntro')}</p>
        <div class="table-responsive mb-4">
          <table class="table table-sm align-middle">
            <thead><tr><th>{t('groups.colName')}</th><th>{t('groups.colSchedule')}</th><th>{t('groups.colRules')}</th><th>{t('groups.colActive')}</th>{#if auth.can('rules:manage')}<th class="text-end">{t('groups.colActions')}</th>{/if}</tr></thead>
            <tbody>
              {#each schedules as sc (sc.id)}
                <tr>
                  <td>{sc.name}{#if sc.is_system} <span class="badge bg-secondary">{t('groups.system')}</span>{/if}</td>
                  <td><code class="small">{sc.cron_expr}</code></td>
                  <td class="small text-body-secondary">{(sc.rules ?? []).length}</td>
                  <td>{sc.enabled ? '✓' : '-'}</td>
                  {#if auth.can('rules:manage')}
                    <td class="text-end text-nowrap">
                      <button class="btn btn-sm btn-outline-primary py-0" data-testid="sched-run-now" disabled={busy} onclick={() => run(() => api.groups.triggerSchedule(sc.id), t('groups.notices.schedTriggered'))}>{t('groups.now')}</button>
                      <button class="btn btn-sm btn-outline-secondary py-0" disabled={busy} onclick={() => run(() => (sc.enabled ? api.groups.disableSchedule(sc.id) : api.groups.enableSchedule(sc.id)), t('groups.notices.schedChanged'))}>{sc.enabled ? t('groups.off') : t('groups.on')}</button>
                      {#if !sc.is_system}
                        <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEditSchedule(sc)}>{t('groups.edit')}</button>
                        <button class="btn btn-sm btn-outline-danger py-0" aria-label={t('groups.a11y.deleteSchedule')} disabled={busy} onclick={() => { if (!confirm(t('groups.confirms.deleteSchedule', { name: sc.name }))) return; run(() => api.groups.removeSchedule(sc.id), t('groups.notices.schedDeleted')); }}>×</button>
                      {/if}
                    </td>
                  {/if}
                </tr>
              {:else}
                <tr><td colspan="5" class="text-body-secondary small">{t('groups.noSchedules')}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>

        <!-- Rules-Tabelle -->
        <div class="d-flex justify-content-between align-items-center mb-2">
          <h3 class="h6 mb-0">{t('groups.rulesTitle')}</h3>
          {#if auth.can('rules:manage')}
            <button class="btn btn-sm btn-primary" onclick={openNewRule} title={t('groups.newRule')}>+</button>
          {/if}
        </div>
        <p class="small text-body-secondary">
          {t('groups.rulesIntro')}
        </p>
        <div class="table-responsive">
          <table class="table table-sm align-middle">
            <thead><tr><th>{t('groups.colName')}</th><th>{t('groups.colType')}</th><th>{t('groups.colTarget')}</th><th>{t('groups.colConfig')}</th><th>{t('groups.colActive')}</th>{#if auth.can('rules:manage')}<th class="text-end">{t('groups.colActions')}</th>{/if}</tr></thead>
            <tbody>
              {#each rules as r (r.id)}
                <tr>
                  <td>{r.name}{#if r.is_system} <span class="badge bg-secondary">{t('groups.system')}</span>{/if}</td>
                  <td class="small">{r.type}</td>
                  <td class="small">
                    {#if r.enforce}<span class="badge text-bg-info">{t('groups.policy')}</span>{:else}<span class="text-body-secondary">{ruleTarget(r)}</span>{/if}
                  </td>
                  <td class="small text-body-secondary text-truncate" style="max-width: 220px">{ruleConfig(r)}</td>
                  <td>{r.enabled ? '✓' : '-'}</td>
                  {#if auth.can('rules:manage')}
                    <td class="text-end text-nowrap">
                      <button class="btn btn-sm btn-outline-primary py-0" data-testid="rule-run-now" disabled={busy} onclick={() => run(() => api.groups.triggerRule(r.id), t('groups.notices.ruleTriggered'))}>{t('groups.now')}</button>
                      <button class="btn btn-sm btn-outline-secondary py-0" disabled={busy} onclick={() => run(() => (r.enabled ? api.groups.disableRule(r.id) : api.groups.enableRule(r.id)), t('groups.notices.ruleChanged'))}>{r.enabled ? t('groups.off') : t('groups.on')}</button>
                      {#if !r.is_system}
                        <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => openEditRule(r)}>{t('groups.edit')}</button>
                        <button class="btn btn-sm btn-outline-danger py-0" aria-label={t('groups.a11y.deleteRule')} disabled={busy} onclick={() => { if (!confirm(t('groups.confirms.deleteRule', { name: r.name }))) return; run(() => api.groups.removeRule(r.id), t('groups.notices.ruleDeleted')); }}>×</button>
                      {/if}
                    </td>
                  {/if}
                </tr>
              {:else}
                <tr><td colspan="6" class="text-body-secondary small">{t('groups.noRules')}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
          </div>
        </div>
      {:else}
        <div class="text-body-secondary">{t('groups.selectGroupHint')}</div>
      {/if}
    </div>
  </div>
</div>

<!-- Popup: Gruppe anlegen/bearbeiten -->
<Modal title={groupForm.id ? t('groups.editGroup') : t('groups.newGroup')} bind:open={groupOpen}>
  <label class="form-label small mb-1" for="gf-name">{t('groups.name')}</label>
  <input id="gf-name" class="form-control mb-2" placeholder={t('groups.name')} bind:value={groupForm.name} />
  <label class="form-label small mb-1" for="gf-desc">{t('groups.description')}</label>
  <input id="gf-desc" class="form-control mb-2" placeholder={t('groups.description')} bind:value={groupForm.description} />
  <label class="form-label small mb-1" for="gf-priority">{t('groups.priority')}</label>
  <input id="gf-priority" class="form-control mb-1" type="number" min="1" max="9999" bind:value={groupForm.priority} />
  <p class="small text-body-secondary mb-3">{t('groups.priorityHint')}</p>
  <div class="text-end">
    <button class="btn btn-secondary" onclick={() => (groupOpen = false)}>{t('groups.cancel')}</button>
    <button class="btn btn-primary" onclick={saveGroup} disabled={!groupForm.name || busy}>{groupForm.id ? t('groups.save') : t('groups.create')}</button>
  </div>
</Modal>

<!-- Popup: Schedule anlegen/bearbeiten -->
<Modal title={schedForm.id ? t('groups.editScheduleTitle') : t('groups.newScheduleTitle')} bind:open={schedOpen}>
  <label class="form-label small mb-1" for="sf-name">{t('groups.name')}</label>
  <input id="sf-name" class="form-control mb-2" placeholder={t('groups.schedNamePlaceholder')} bind:value={schedForm.name} />
  <CronBuilder bind:value={schedForm.cron_expr} />
  <label class="form-label small mb-1" for="sf-cron">{t('groups.cronExpr')}</label>
  <input id="sf-cron" class="form-control mb-1" placeholder="0 3 * * *" bind:value={schedForm.cron_expr} />
  <p class="small text-body-secondary">{t('groups.cronHint')}</p>
  <div class="text-end">
    <button class="btn btn-secondary" onclick={() => (schedOpen = false)}>{t('groups.cancel')}</button>
    <button class="btn btn-primary" onclick={saveSchedule} disabled={!schedForm.name || !schedForm.cron_expr || busy}>{t('groups.save')}</button>
  </div>
</Modal>

<!-- Popup: Rule anlegen/bearbeiten -->
<Modal title={ruleIsEdit ? t('groups.editRuleTitle') : t('groups.newRuleTitle')} bind:open={ruleOpen}>
  <label class="form-label small mb-1" for="rf-name">{t('groups.name')}</label>
  <input id="rf-name" class="form-control mb-2" placeholder={t('groups.name')} bind:value={ruleForm.name} />

  {#if !ruleIsEdit}
    <label class="form-label small mb-1" for="rf-target">{t('groups.colTarget')}</label>
    <select id="rf-target" class="form-select mb-2" bind:value={ruleForm.target} onchange={onTargetChange}>
      {#each schedules as sc (sc.id)}<option value="sched:{sc.id}">{t('groups.scheduleOption', { name: sc.name, cron: sc.cron_expr })}</option>{/each}
      <option value="enforce">{t('groups.policyRuleOption')}</option>
    </select>

    <label class="form-label small mb-1" for="rf-type">{t('groups.colType')}</label>
    <select id="rf-type" class="form-select mb-2" bind:value={ruleForm.type} onchange={() => { ruleForm.command = ''; fwRules = []; }}>
      {#each ruleTypeOptions as [val, label] (val)}<option value={val}>{label}</option>{/each}
    </select>
  {:else}
    <p class="small text-body-secondary">{t('groups.typeLabel')} <strong>{ruleForm.type}</strong> · {ruleIsEnforce ? t('groups.policyRule') : t('groups.atSchedule')}</p>
  {/if}

  {#if ruleForm.type === 'script'}
    <label class="form-label small mb-1" for="rf-cmd">{t('groups.command')}</label>
    <input id="rf-cmd" class="form-control mb-2" placeholder={t('groups.command')} bind:value={ruleForm.command} />
  {:else if ruleForm.type === 'packages'}
    <label class="form-label small mb-1" for="rf-cmd">{t('groups.packageNames')}</label>
    <input id="rf-cmd" class="form-control mb-2" placeholder={t('groups.packagesPlaceholder')} bind:value={ruleForm.command} />
  {:else if ruleForm.type === 'firewall'}
    <div class="mb-2">
      <span class="form-label small mb-1 d-block">{t('groups.firewallRules')}</span>
      <FirewallRulesEditor bind:this={fwEditor} bind:rules={fwRules} allowlists={ipAllowlists} />
      <div class="form-text">{t('groups.firewallRulesHint')}</div>
    </div>
  {:else if ruleForm.type === 'custom'}
    <label class="form-label small mb-1" for="rf-custom">{t('groups.customAction')}</label>
    {#if customActions.length}
      <select id="rf-custom" class="form-select mb-2" bind:value={ruleForm.command}>
        <option value="">{t('groups.chooseOption')}</option>
        {#each customActions as a (a.id)}<option value={String(a.id)}>{a.name}</option>{/each}
      </select>
    {:else}
      <p class="small text-body-secondary">{t('groups.noCustomA')}<strong>{t('groups.noCustomBold')}</strong>{t('groups.noCustomB')}</p>
    {/if}
  {:else if ruleForm.type === 'apt-proxy'}
    <p class="small text-body-secondary">
      {t('groups.aptProxyA')}<strong>{t('groups.aptProxyBold')}</strong>{t('groups.aptProxyB')}
    </p>
  {/if}

  <div class="text-end mt-2">
    <button class="btn btn-secondary" onclick={() => (ruleOpen = false)}>{t('groups.cancel')}</button>
    <button class="btn btn-primary" onclick={saveRule} disabled={!ruleValid || busy}>{t('groups.save')}</button>
  </div>
</Modal>
