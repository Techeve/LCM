<script>
  // Hilfe zu den Tastenkürzeln: „?" oder die Schaltfläche unten links öffnet
  // ein Dialogfenster mit allen Kürzeln - die festen (Sprünge, Suche,
  // Speichern, Pfeiltasten) und die der gerade offenen Seite, gelesen aus den
  // data-hotkey-Attributen (siehe lib/hotkeys.svelte.js).
  import { hotkeys, visibleGoto, keyLabel } from '../lib/hotkeys.svelte.js';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let closeBtn = $state(null);
  let opener = null; // wohin der Fokus nach dem Schließen zurückgeht
  let pageActions = $state([]);

  $effect(() => {
    if (hotkeys.helpOpen) {
      opener = document.activeElement;
      pageActions = hotkeys.pageActions();
      queueMicrotask(() => closeBtn?.focus());
    } else if (opener?.isConnected) {
      opener.focus();
      opener = null;
    }
  });

  function close() {
    hotkeys.helpOpen = false;
  }

  // Fokus bleibt im Dialog (Tab am Ende springt zum Anfang).
  function trap(e) {
    if (e.key !== 'Tab') return;
    const items = [...e.currentTarget.querySelectorAll('button, input, [tabindex]:not([tabindex="-1"])')];
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }

  const GLOBAL = [
    { keys: ['?'], labelKey: 'hotkeys.help' },
    { keys: ['/'], labelKey: 'hotkeys.search' },
    { keys: [keyLabel('mod+s')], labelKey: 'hotkeys.save' },
    { keys: ['Esc'], labelKey: 'hotkeys.escape' },
    { keys: ['←', '→', '↑', '↓'], labelKey: 'hotkeys.arrows' },
    { keys: ['Tab'], labelKey: 'hotkeys.tab' },
  ];
</script>

{#if hotkeys.helpOpen}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="modal fade show d-block" tabindex="-1" role="dialog" aria-modal="true"
    aria-labelledby="hotkey-help-title" onmousedown={close} onkeydown={trap} data-testid="hotkey-help">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-dialog modal-dialog-centered modal-lg modal-dialog-scrollable" onmousedown={(e) => e.stopPropagation()}>
      <div class="modal-content">
        <div class="modal-header">
          <h2 class="modal-title h5" id="hotkey-help-title">{t('hotkeys.title')}</h2>
          <button type="button" class="btn-close" bind:this={closeBtn} aria-label={t('common.close')} onclick={close}></button>
        </div>
        <div class="modal-body">
          <p class="text-body-secondary small">{t('hotkeys.intro')}</p>
          <div class="row g-4">
            <div class="col-md-6">
              <h3 class="h6">{t('hotkeys.groupGoto')}</h3>
              <p class="small text-body-secondary mb-2">{t('hotkeys.gotoHint')}</p>
              <dl class="hotkey-list">
                {#each visibleGoto() as g (g.key)}
                  <div>
                    <dt><kbd>G</kbd> <kbd>{keyLabel(g.key)}</kbd></dt>
                    <dd>{t(g.labelKey)}</dd>
                  </div>
                {/each}
              </dl>
            </div>
            <div class="col-md-6">
              <h3 class="h6">{t('hotkeys.groupGlobal')}</h3>
              <dl class="hotkey-list">
                {#each GLOBAL as g (g.labelKey)}
                  <div>
                    <dt>{#each g.keys as k, i (k)}{#if i > 0}{' '}{/if}<kbd>{k}</kbd>{/each}</dt>
                    <dd>{t(g.labelKey)}</dd>
                  </div>
                {/each}
              </dl>
              <h3 class="h6 mt-3">{t('hotkeys.groupPage')}</h3>
              {#if pageActions.length}
                <dl class="hotkey-list" data-testid="hotkey-page-actions">
                  {#each pageActions as a (a.key)}
                    <div>
                      <dt><kbd>{keyLabel(a.key)}</kbd></dt>
                      <dd>{a.label}</dd>
                    </div>
                  {/each}
                </dl>
              {:else}
                <p class="small text-body-secondary">{t('hotkeys.noPageActions')}</p>
              {/if}
            </div>
          </div>
          <hr />
          <div class="form-check form-switch">
            <input class="form-check-input" type="checkbox" role="switch" id="hotkey-badges"
              checked={hotkeys.badges} onchange={(e) => (hotkeys.badges = e.currentTarget.checked)} />
            <label class="form-check-label" for="hotkey-badges">{t('hotkeys.showBadges')}</label>
          </div>
        </div>
      </div>
    </div>
  </div>
  <div class="modal-backdrop fade show"></div>
{/if}

<style>
  .hotkey-list {
    margin: 0;
  }
  .hotkey-list > div {
    display: flex;
    gap: 0.75rem;
    align-items: baseline;
    padding: 0.2rem 0;
  }
  .hotkey-list dt {
    flex: 0 0 7rem;
    font-weight: normal;
    white-space: nowrap;
  }
  .hotkey-list dd {
    margin: 0;
  }
</style>
