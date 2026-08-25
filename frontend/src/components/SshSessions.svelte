<script>
  // Zeigt SSH-Protokoll-Sessions als aufklappbare Liste. Jede Session listet
  // beim Aufklappen ihre Kommandos (Zeit, Dauer, Exit-Code, Ausgabe).
  // Sessions ohne mitgelieferte Kommandos werden per loadCommands(id) lazy
  // nachgeladen (Server-Ansicht); Job-Ansicht liefert die Kommandos direkt.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);
  let { sessions = [], loadCommands = null } = $props();

  let openId = $state(null);
  let commands = $state({}); // sessionId -> Kommando-Array
  let loading = $state(false);

  async function toggle(s) {
    if (openId === s.id) {
      openId = null;
      return;
    }
    openId = s.id;
    if (commands[s.id]) return;
    if (s.commands) {
      commands[s.id] = s.commands;
      return;
    }
    if (loadCommands) {
      loading = true;
      try {
        const full = await loadCommands(s.id);
        commands[s.id] = full.commands ?? [];
      } finally {
        loading = false;
      }
    }
  }

  function when(iso) {
    return iso ? new Date(iso).toLocaleString() : '-';
  }
</script>

{#if sessions.length === 0}
  <p class="text-body-secondary small mb-0">{t('ssh.none')}</p>
{:else}
  <div class="list-group">
    {#each sessions as s (s.id)}
      <div class="list-group-item p-0">
        <button
          class="btn btn-link text-decoration-none w-100 text-start d-flex justify-content-between align-items-center px-3 py-2"
          onclick={() => toggle(s)}
        >
          <span class="d-flex align-items-center gap-2 flex-wrap">
            <span class="badge {s.had_error ? 'text-bg-danger' : 'text-bg-secondary'}">{s.purpose}</span>
            <span class="small text-body-secondary">{when(s.opened_at)}</span>
            {#if s.actor}<span class="small text-body-secondary">· {s.actor}</span>{/if}
            {#if s.job_id}<span class="badge border text-body-secondary">Job #{s.job_id}</span>{/if}
          </span>
          <span class="small text-body-secondary">{s.command_count} {s.command_count === 1 ? t('ssh.commandOne') : t('ssh.commandMany')} {openId === s.id ? '▲' : '▼'}</span>
        </button>
        {#if openId === s.id}
          <div class="px-3 pb-3">
            {#if loading && !commands[s.id]}
              <div class="small text-body-secondary">{t('ssh.loadingCommands')}</div>
            {:else if (commands[s.id] ?? []).length === 0}
              <div class="small text-body-secondary">{t('ssh.noCommands')}</div>
            {:else}
              {#each commands[s.id] as c (c.id)}
                <div class="mb-2">
                  <div class="d-flex justify-content-between align-items-center">
                    <code class="small text-primary-emphasis">$ {c.command}</code>
                    <span class="small text-body-secondary ms-2 text-nowrap">
                      <span class="badge {c.exit_code === 0 ? 'text-bg-success' : 'text-bg-danger'}">exit {c.exit_code}</span>
                      {c.duration_ms} ms
                    </span>
                  </div>
                  {#if c.output}
                    <pre class="bg-body-secondary p-2 rounded mb-0 mt-1 small" style="max-height: 260px; overflow: auto"><code>{c.output}</code></pre>
                  {/if}
                  {#if c.err}<div class="small text-danger">{t('ssh.transportError')}{c.err}</div>{/if}
                </div>
              {/each}
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}
