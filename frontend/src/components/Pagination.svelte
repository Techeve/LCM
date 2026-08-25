<script>
  // Einheitliche Seitennavigation unter Tabellen.
  //
  // Vorher war das je Seite anders aufgebaut - mal zentrierte Bootstrap-
  // Pagination mit Pfeil-Glyphen, mal eine Button-Gruppe rechts außen mit
  // Text-Schaltern. Diese Komponente ist ab jetzt die einzige Variante:
  // links der Bereichs-/Seitenhinweis, rechts die Blätter-Schalter.
  //
  // Ohne `total`/`pageSize` entfällt der Bereichshinweis, es bleibt die
  // Seitenangabe. Bei einer einzigen Seite rendert die Komponente nichts.
  import { i18n } from '../stores/i18n.svelte.js';
  import { fmtNum } from '../lib/format.js';

  const t = (k, p) => i18n.t(k, p);

  let {
    page = 1,
    pageCount = 1,
    total = null,
    pageSize = null,
    onchange,
    label = '',
    testid = 'pagination',
  } = $props();

  let pages = $derived(Math.max(1, pageCount));
  let current = $derived(Math.min(Math.max(1, page), pages));
  let hasRange = $derived(total != null && pageSize != null);
  let from = $derived(hasRange && total > 0 ? (current - 1) * pageSize + 1 : 0);
  let to = $derived(hasRange ? Math.min(current * pageSize, total) : 0);

  function goto(p) {
    const target = Math.min(Math.max(1, p), pages);
    if (target !== current) onchange(target);
  }
</script>

{#if pages > 1}
  <nav
    class="d-flex flex-wrap align-items-center justify-content-between gap-2 mt-3"
    aria-label={label || t('common.pageNav')}
    data-testid={testid}
  >
    <span class="small text-body-secondary">
      {#if hasRange}
        {t('common.pageRange', { from: fmtNum(from), to: fmtNum(to), total: fmtNum(total) })} -
      {/if}
      {t('common.pageOf', { page: current, pages })}
    </span>
    <div class="btn-group btn-group-sm">
      <button
        class="btn btn-outline-secondary"
        disabled={current <= 1}
        onclick={() => goto(1)}
        title={t('common.firstPage')}
        aria-label={t('common.firstPage')}>«</button
      >
      <button class="btn btn-outline-secondary" disabled={current <= 1} onclick={() => goto(current - 1)}>
        {t('common.back')}
      </button>
      <button class="btn btn-outline-secondary" disabled={current >= pages} onclick={() => goto(current + 1)}>
        {t('common.next')}
      </button>
      <button
        class="btn btn-outline-secondary"
        disabled={current >= pages}
        onclick={() => goto(pages)}
        title={t('common.lastPage')}
        aria-label={t('common.lastPage')}>»</button
      >
    </div>
  </nav>
{/if}
