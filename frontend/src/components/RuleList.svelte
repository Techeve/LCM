<script>
  // Die Regeln EINES Auslösers (Zeitplan oder Grundsatz-Regeln) in
  // Ausführungsreihenfolge: oben die, die zuerst läuft. Umsortieren per
  // Ziehen oder mit den Pfeilen - die Pfeile gehen auch per Tastatur.
  // Die Liste meldet nur die neue Reihenfolge (onreorder); speichern und neu
  // laden ist Sache der Seite.
  import { i18n } from '../stores/i18n.svelte.js';
  import { icons } from '../lib/icons.js';

  const t = (k, p) => i18n.t(k, p);

  let { rules, canManage, busy, config, typeLabel, onreorder, onrun, ontoggle, onedit, ondelete } = $props();

  let dragFrom = $state(null);
  let dragOver = $state(null);

  function move(from, to) {
    if (from === to || to < 0 || to >= rules.length) return;
    const ids = rules.map((r) => r.id);
    const [id] = ids.splice(from, 1);
    ids.splice(to, 0, id);
    onreorder(ids);
  }

  function startDrag(e, index) {
    dragFrom = index;
    e.dataTransfer.effectAllowed = 'move';
  }

  function overDrag(e, index) {
    if (dragFrom === null) return;
    e.preventDefault();
    dragOver = index;
  }

  function endDrag() {
    dragFrom = null;
    dragOver = null;
  }

  function dropAt(e, index) {
    e.preventDefault();
    if (dragFrom !== null) move(dragFrom, index);
    endDrag();
  }
</script>

{#if rules.length === 0}
  <p class="small text-body-secondary mb-0">{t('groups.noRulesInTrigger')}</p>
{:else}
  <ol class="list-unstyled mb-0">
    {#each rules as r, i (r.id)}
      {@const detail = config(r)}
      <li
        class="rule-item d-flex align-items-center gap-2 border rounded px-2 py-1 mb-1"
        class:drop-target={dragOver === i && dragFrom !== i}
        class:opacity-50={!r.enabled}
        data-testid="rule-item"
        draggable={canManage && !busy}
        ondragstart={(e) => startDrag(e, i)}
        ondragover={(e) => overDrag(e, i)}
        ondrop={(e) => dropAt(e, i)}
        ondragend={endDrag}
      >
        {#if canManage}
          <span class="drag-handle text-body-secondary" title={t('groups.dragHint')} aria-hidden="true">{@html icons.grip}</span>
        {/if}
        <span class="badge text-bg-light border" data-testid="rule-position">{i + 1}</span>
        <div class="flex-grow-1 text-truncate">
          <span class="fw-semibold">{r.name}</span>
          {#if r.is_system}<span class="badge bg-secondary ms-1">{t('groups.system')}</span>{/if}
          {#if !r.enabled}<span class="badge text-bg-secondary ms-1">{t('groups.off')}</span>{/if}
          <div class="small text-body-secondary text-truncate">{typeLabel(r.type)}{#if detail && detail !== '-'}{` · ${detail}`}{/if}</div>
        </div>
        {#if canManage}
          <div class="d-flex flex-wrap justify-content-end gap-1">
            <button class="btn btn-sm btn-outline-secondary py-0" aria-label={t('groups.a11y.moveUp')} title={t('groups.a11y.moveUp')}
              disabled={busy || i === 0} onclick={() => move(i, i - 1)}>{@html icons.chevronUp}</button>
            <button class="btn btn-sm btn-outline-secondary py-0" aria-label={t('groups.a11y.moveDown')} title={t('groups.a11y.moveDown')}
              disabled={busy || i === rules.length - 1} onclick={() => move(i, i + 1)}>{@html icons.chevronDown}</button>
            <button class="btn btn-sm btn-outline-primary py-0" data-testid="rule-run-now" disabled={busy} onclick={() => onrun(r)}>{t('groups.now')}</button>
            <button class="btn btn-sm btn-outline-secondary py-0" disabled={busy} onclick={() => ontoggle(r)}>{r.enabled ? t('groups.off') : t('groups.on')}</button>
            {#if !r.is_system}
              <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => onedit(r)}>{t('groups.edit')}</button>
              <button class="btn btn-sm btn-outline-danger py-0" aria-label={t('groups.a11y.deleteRule')} disabled={busy} onclick={() => ondelete(r)}>×</button>
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ol>
{/if}

<style>
  .rule-item[draggable='true'] {
    cursor: grab;
  }
  .drop-target {
    border-color: var(--bs-primary) !important;
    box-shadow: 0 -2px 0 var(--bs-primary);
  }
</style>
