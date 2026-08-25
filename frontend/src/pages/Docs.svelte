<script>
  // Mitgelieferte Anwender-Doku (/doku). Die Seiten sind Markdown-Dateien im
  // Repository, die das Backend eingebettet ausliefert - pflegen heißt: Datei
  // bearbeiten. Erreichbar ohne Anmeldung, weil die Schlüssel-Anleitung genau
  // dann gebraucht wird, wenn man noch keinen Zugang hat.
  import { push } from 'svelte-spa-router';
  import { api } from '../api';
  import { ApiError } from '../api/client.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let { params = {} } = $props();

  let pages = $state([]);
  let page = $state(null);
  let error = $state('');
  let loading = $state(true);

  // Der Inhalt hängt an der Sprache: Wechselt sie, wird neu geladen.
  let lang = $derived(i18n.locale);
  let slug = $derived(params.slug ?? '');

  $effect(() => {
    load(lang, slug);
  });

  async function load(currentLang, currentSlug) {
    loading = true;
    error = '';
    try {
      pages = await api.docs.list(currentLang);
      const wanted = currentSlug || pages[0]?.slug;
      page = wanted ? await api.docs.get(wanted, currentLang) : null;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
      page = null;
    } finally {
      loading = false;
    }
  }

  function open(s) {
    push(`/doku/${s}`);
  }
</script>

<div class="container py-4">
  <h1 class="h4 mb-3">{t('docs.title')}</h1>
  <p class="text-body-secondary">{t('docs.intro')}</p>

  {#if error}
    <div class="alert alert-danger" data-testid="docs-error">{error}</div>
  {:else if loading && !page}
    <div class="text-body-secondary">{t('common.loading')}</div>
  {:else}
    <div class="row g-4">
      <!-- Seitenliste: erst ab zwei Seiten sinnvoll. -->
      {#if pages.length > 1}
        <div class="col-12 col-md-3">
          <nav class="list-group" data-testid="docs-nav">
            {#each pages as p (p.slug)}
              <button
                class="list-group-item list-group-item-action {p.slug === page?.slug ? 'active' : ''}"
                onclick={() => open(p.slug)}>{p.title}</button>
            {/each}
          </nav>
        </div>
      {/if}
      <div class="col">
        {#if page}
          <!-- Der Inhalt stammt aus dem eigenen Renderer im Backend, der
               Roh-HTML konsequent maskiert (siehe internal/appdocs). -->
          <article class="lcm-docs" data-testid="docs-content">{@html page.html}</article>
        {:else}
          <div class="text-body-secondary" data-testid="docs-empty">{t('docs.empty')}</div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  /* Lesbarer Fließtext: begrenzte Zeilenlänge, ruhige Abstände. Die Doku ist
     zum Lesen da, nicht zum Überfliegen wie eine Tabellenansicht. */
  .lcm-docs {
    max-width: 52rem;
  }
  .lcm-docs :global(h1) {
    font-size: 1.6rem;
    margin-bottom: 1rem;
  }
  .lcm-docs :global(h2) {
    font-size: 1.25rem;
    margin-top: 2rem;
    margin-bottom: 0.75rem;
    padding-bottom: 0.3rem;
    border-bottom: 1px solid var(--bs-border-color);
  }
  .lcm-docs :global(h3) {
    font-size: 1.05rem;
    margin-top: 1.5rem;
    margin-bottom: 0.5rem;
  }
  .lcm-docs :global(p),
  .lcm-docs :global(li) {
    line-height: 1.65;
  }
  .lcm-docs :global(pre) {
    background: var(--bs-secondary-bg);
    border: 1px solid var(--bs-border-color);
    border-radius: 0.375rem;
    padding: 0.75rem 1rem;
    overflow-x: auto;
  }
  .lcm-docs :global(pre code) {
    background: none;
    padding: 0;
    color: inherit;
  }
  .lcm-docs :global(code) {
    background: var(--bs-secondary-bg);
    padding: 0.1rem 0.35rem;
    border-radius: 0.25rem;
    font-size: 0.875em;
  }
  .lcm-docs :global(blockquote) {
    border-left: 3px solid var(--bs-primary);
    background: var(--bs-secondary-bg);
    padding: 0.6rem 1rem;
    margin: 1rem 0;
    border-radius: 0 0.375rem 0.375rem 0;
  }
  .lcm-docs :global(table) {
    width: 100%;
    margin: 1rem 0;
  }
  .lcm-docs :global(th),
  .lcm-docs :global(td) {
    border-bottom: 1px solid var(--bs-border-color);
    padding: 0.5rem 0.75rem;
    text-align: left;
    vertical-align: top;
  }
</style>
