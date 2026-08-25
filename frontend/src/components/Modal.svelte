<script>
  // Leichtgewichtiges Modal-Overlay (Bootstrap-CSS-Klassen + Svelte-State,
  // ohne Bootstrap-JS - Zero-Bloat). Schließt bei Klick auf Backdrop, X oder Esc.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);
  let { title, open = $bindable(false), size = '', children } = $props();

  function close() {
    open = false;
  }

  function onKey(e) {
    if (e.key === 'Escape') close();
  }
</script>

<svelte:window onkeydown={open ? onKey : undefined} />

{#if open}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="modal fade show d-block" tabindex="-1" role="dialog" aria-modal="true" onmousedown={close}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="modal-dialog modal-dialog-centered {size}"
      onmousedown={(e) => e.stopPropagation()}
    >
      <div class="modal-content">
        <div class="modal-header">
          <h5 class="modal-title">{title}</h5>
          <button type="button" class="btn-close" aria-label={t('modal.close')} onclick={close}></button>
        </div>
        <div class="modal-body">
          {@render children()}
        </div>
      </div>
    </div>
  </div>
  <div class="modal-backdrop fade show"></div>
{/if}
