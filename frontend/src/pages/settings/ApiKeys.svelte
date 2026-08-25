<script>
  // API-Key-Verwaltung (Service-Accounts). Der Klartext-Key ist nur direkt
  // nach dem Erstellen sichtbar.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';

  const t = (k, p) => i18n.t(k, p);

  let keys = $state([]);
  let error = $state('');
  let newKeyName = $state('');
  let newKeyScope = $state('readwrite');
  let createdKey = $state(null);
  // MCP-Keys werden separat unter Einstellungen → MCP verwaltet und hier
  // ausgeblendet (getrennte Auflistung).
  const restKeys = $derived(keys.filter((k) => k.scope !== 'mcp'));

  async function load() {
    try {
      keys = await api.apiKeys.getAll();
      error = '';
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  async function create(event) {
    event.preventDefault();
    error = '';
    try {
      createdKey = await api.apiKeys.create(newKeyName, newKeyScope);
      newKeyName = '';
      newKeyScope = 'readwrite';
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

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.apiKeys.title')}>
  <p class="text-body-secondary small">
    {t('settings.apiKeys.introA')}<code>X-API-Key</code>{t('settings.apiKeys.introB')}
  </p>

  {#if error}<div class="alert alert-danger">{error}</div>{/if}

  {#if createdKey}
    <div class="alert alert-warning">
      <strong>{t('settings.apiKeys.newKeyWarn')}</strong>
      <code class="d-block mt-2 user-select-all">{createdKey.key}</code>
    </div>
  {/if}

  <form class="row g-2 mb-4 align-items-center" onsubmit={create}>
    <div class="col-auto flex-grow-1">
      <input class="form-control" placeholder={t('settings.apiKeys.namePlaceholder')} bind:value={newKeyName} required />
    </div>
    <div class="col-auto">
      <div class="btn-group" role="group" aria-label="Scope">
        <input type="radio" class="btn-check" id="scope-rw" value="readwrite" bind:group={newKeyScope} />
        <label class="btn btn-outline-secondary" for="scope-rw">{t('settings.apiKeys.scopeReadWrite')}</label>
        <input type="radio" class="btn-check" id="scope-r" value="read" bind:group={newKeyScope} />
        <label class="btn btn-outline-secondary" for="scope-r">{t('settings.apiKeys.scopeRead')}</label>
      </div>
    </div>
    <div class="col-auto">
      <button class="btn btn-primary" type="submit">{t('settings.apiKeys.createKey')}</button>
    </div>
  </form>

  <div class="table-responsive">
    <table class="table align-middle">
      <thead>
        <tr><th>{t('common.name')}</th><th>{t('settings.apiKeys.colPrefix')}</th><th>{t('settings.apiKeys.colScope')}</th><th>{t('settings.apiKeys.colLastUsed')}</th><th>{t('common.status')}</th><th></th></tr>
      </thead>
      <tbody>
        {#each restKeys as key (key.id)}
          <tr>
            <td>{key.name}</td>
            <td><code>{key.prefix}…</code></td>
            <td>
              {#if key.scope === 'read'}
                <span class="badge text-bg-warning">{t('settings.apiKeys.badgeRead')}</span>
              {:else}
                <span class="badge text-bg-primary">{t('settings.apiKeys.badgeReadWrite')}</span>
              {/if}
            </td>
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
        {/each}
      </tbody>
    </table>
  </div>
</SettingsLayout>
