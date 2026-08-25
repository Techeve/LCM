<script>
  // MCP-Schnittstelle: an-/abschalten, Bind-Adresse/Port setzen, MCP-API-Keys
  // separat verwalten und ein fertiges Anbindungsbeispiel anzeigen.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let enabled = $state(false);
  let bindHost = $state('127.0.0.1');
  let port = $state(9330);
  let error = $state('');
  let saved = $state(false);

  let keys = $state([]);
  let newKeyName = $state('');
  let createdKey = $state(null);
  let copied = $state(false);

  const mcpKeys = $derived(keys.filter((k) => k.scope === 'mcp'));
  // Für das Beispiel: 0.0.0.0 ist keine anwählbare Adresse - Platzhalter zeigen.
  const exampleHost = $derived(bindHost === '0.0.0.0' || !bindHost ? '<lcm-host>' : bindHost);
  const mcpUrl = $derived(`http://${exampleHost}:${port}/mcp`);
  const configExample = $derived(
    JSON.stringify(
      { mcpServers: { lcm: { type: 'http', url: mcpUrl, headers: { Authorization: `Bearer ${t('settings.mcp.keyPlaceholder')}` } } } },
      null,
      2,
    ),
  );

  async function load() {
    try {
      const s = await api.system.getSettings();
      enabled = !!s.mcp_enabled;
      bindHost = s.mcp_bind_host || '127.0.0.1';
      port = s.mcp_port || 9330;
      keys = await api.apiKeys.getAll();
      error = '';
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function save() {
    error = '';
    saved = false;
    try {
      const res = await api.system.setMCP({ enabled, bind_host: bindHost, port: Number(port) });
      enabled = !!res.mcp_enabled;
      bindHost = res.mcp_bind_host;
      port = res.mcp_port;
      saved = true;
      setTimeout(() => (saved = false), 2500);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function createKey(event) {
    event.preventDefault();
    error = '';
    try {
      createdKey = await api.apiKeys.create(newKeyName, 'mcp');
      newKeyName = '';
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function revoke(key) {
    if (!confirm(t('settings.apiKeys.revokeConfirm', { name: key.name }))) return;
    try {
      await api.apiKeys.revoke(key.id);
      await load();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(configExample);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      /* Clipboard nicht verfügbar - ignorieren */
    }
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.mcp.title')}>
  <p class="text-body-secondary small">{t('settings.mcp.intro')}</p>

  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if saved}<div class="alert alert-success py-2">{t('settings.mcp.saved')}</div>{/if}

  <!-- Aktivierung + Bind/Port -->
  <div class="card mb-4">
    <div class="card-body">
      <div class="form-check form-switch mb-3">
        <input class="form-check-input" type="checkbox" id="mcp-enabled" bind:checked={enabled} data-testid="mcp-enabled" />
        <label class="form-check-label" for="mcp-enabled">{t('settings.mcp.enableLabel')}</label>
        <div class="form-text">{t('settings.mcp.enableHint')}</div>
      </div>
      <div class="row g-2">
        <div class="col-sm-8">
          <label class="form-label" for="mcp-host">{t('settings.mcp.bindHost')}</label>
          <input id="mcp-host" class="form-control" bind:value={bindHost} placeholder="127.0.0.1" />
          <div class="form-text">{t('settings.mcp.bindHostHint')}</div>
        </div>
        <div class="col-sm-4">
          <label class="form-label" for="mcp-port">{t('settings.mcp.port')}</label>
          <input id="mcp-port" type="number" class="form-control" bind:value={port} />
        </div>
      </div>
      <button class="btn btn-primary mt-3" onclick={save} data-testid="mcp-save">{t('common.save')}</button>
    </div>
  </div>

  <!-- MCP-API-Keys (separate Auflistung) -->
  <h2 class="h6">{t('settings.mcp.keysTitle')}</h2>
  <p class="text-body-secondary small">{t('settings.mcp.keysHint')}</p>

  {#if createdKey}
    <div class="alert alert-warning">
      <strong>{t('settings.apiKeys.newKeyWarn')}</strong>
      <code class="d-block mt-2 user-select-all">{createdKey.key}</code>
    </div>
  {/if}

  <form class="row g-2 mb-3 align-items-center" onsubmit={createKey}>
    <div class="col-auto flex-grow-1">
      <input class="form-control" placeholder={t('settings.mcp.keyNamePlaceholder')} bind:value={newKeyName} required data-testid="mcp-key-name" />
    </div>
    <div class="col-auto">
      <button class="btn btn-primary" type="submit" data-testid="mcp-key-create">{t('settings.mcp.createKey')}</button>
    </div>
  </form>

  <div class="table-responsive mb-4">
    <table class="table align-middle">
      <thead>
        <tr><th>{t('common.name')}</th><th>{t('settings.apiKeys.colPrefix')}</th><th>{t('settings.apiKeys.colLastUsed')}</th><th>{t('common.status')}</th><th></th></tr>
      </thead>
      <tbody>
        {#each mcpKeys as key (key.id)}
          <tr>
            <td>{key.name}</td>
            <td><code>{key.prefix}…</code></td>
            <td class="small">{key.last_used_at ? new Date(key.last_used_at).toLocaleString() : '-'}</td>
            <td>
              {#if key.revoked}<span class="badge text-bg-danger">{t('settings.apiKeys.revoked')}</span>
              {:else}<span class="badge text-bg-success">{t('settings.apiKeys.active')}</span>{/if}
            </td>
            <td class="text-end">
              {#if !key.revoked}
                <button class="btn btn-outline-danger btn-sm" onclick={() => revoke(key)}>{t('settings.apiKeys.revoke')}</button>
              {/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="5" class="text-body-secondary small">{t('settings.mcp.noKeys')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>

  <!-- Anbindungsbeispiel -->
  <h2 class="h6">{t('settings.mcp.exampleTitle')}</h2>
  <p class="text-body-secondary small">{t('settings.mcp.exampleHint')}</p>
  <div class="position-relative">
    <button class="btn btn-outline-secondary btn-sm position-absolute top-0 end-0 m-2" onclick={copyConfig}>
      {copied ? t('joinWizard.agent.copied') : t('common.copy')}
    </button>
    <pre class="bg-body-secondary p-3 rounded"><code>{configExample}</code></pre>
  </div>
  <p class="small text-body-secondary">
    {t('settings.mcp.curlHint')}
  </p>
  <pre class="bg-body-secondary p-3 rounded"><code>curl -s {mcpUrl} \
  -H "Authorization: Bearer {t('settings.mcp.keyPlaceholder')}" \
  -H "Content-Type: application/json" \
  -d '&lbrace;"jsonrpc":"2.0","id":1,"method":"tools/call","params":&lbrace;"name":"list_servers","arguments":&lbrace;&rbrace;&rbrace;&rbrace;'</code></pre>
</SettingsLayout>
