<script>
  // Liste von Servern als Links, mit Kürzung bei vielen Einträgen.
  //
  // Bis einschließlich `max` Server stehen alle da. Darüber werden nur die
  // ersten (max - 1) gezeigt und der Rest hinter einer Schaltfläche
  // versteckt - sonst sprengt eine Zeile mit zwanzig Servernamen die ganze
  // Tabelle, und genau die Zeilen mit vielen Servern sind die
  // interessanten.
  import { link } from 'svelte-spa-router';
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let { servers = [], max = 3 } = $props();

  let open = $state(false);
  const uid = $props.id();

  let shown = $derived(servers.length > max ? servers.slice(0, max - 1) : servers);
  let hidden = $derived(servers.length > max ? servers.slice(max - 1) : []);
</script>

{#if servers.length === 0}
  <span class="text-body-secondary">-</span>
{:else}
  <span class="d-inline-flex flex-wrap align-items-center gap-1">
    {#each shown as s, i (s.id)}
      <span>
        <a href={`/servers/${s.id}`} use:link>{s.name}</a>{#if i < shown.length - 1 || hidden.length > 0},{/if}
      </span>
    {/each}

    {#if hidden.length > 0}
      <span class="position-relative">
        <button type="button" class="btn btn-sm btn-link p-0 align-baseline text-decoration-none"
          data-testid="server-links-more"
          aria-expanded={open}
          aria-controls={`servers-${uid}`}
          onclick={() => (open = !open)}>
          {t('serverLinks.more', { count: hidden.length })}
        </button>

        {#if open}
          <!-- Bewusst ein aufklappendes Feld statt eines echten Tooltips:
               Die Namen sollen anklickbar sein, und ein title-Tooltip kann
               keine Links tragen. -->
          <div id={`servers-${uid}`} class="card position-absolute shadow-sm p-2 mt-1"
            style="z-index: 1050; min-width: 12rem; max-height: 16rem; overflow-y: auto"
            data-testid="server-links-popover">
            <div class="d-flex justify-content-between align-items-center mb-1">
              <span class="small text-body-secondary">{t('serverLinks.allTitle', { count: servers.length })}</span>
              <button type="button" class="btn-close btn-sm" aria-label={t('common.close')}
                onclick={() => (open = false)}></button>
            </div>
            <ul class="list-unstyled mb-0 small">
              {#each servers as s (s.id)}
                <li><a href={`/servers/${s.id}`} use:link>{s.name}</a></li>
              {/each}
            </ul>
          </div>
        {/if}
      </span>
    {/if}
  </span>
{/if}
