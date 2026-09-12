<script>
  // Leichtgewichtiges Modal-Overlay (Bootstrap-CSS-Klassen + Svelte-State,
  // ohne Bootstrap-JS - Zero-Bloat). Schließt bei Klick auf Backdrop, X oder Esc.
  //
  // Tastatur: Beim Öffnen springt der Fokus in den Dialog (erstes Feld, sonst
  // die Schließen-Schaltfläche), Tab bleibt darin, und beim Schließen kehrt
  // der Fokus zu dem Element zurück, das den Dialog geöffnet hat. Ohne das
  // stünde ein Screenreader-Nutzer nach dem Klick noch hinter dem Dialog.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);
  let { title, open = $bindable(false), size = '', children } = $props();

  let dialogEl = $state(null);
  let opener = null;

  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  $effect(() => {
    if (!open || !dialogEl) return;
    opener = document.activeElement;
    const fields = [...dialogEl.querySelectorAll(FOCUSABLE)];
    const first = fields.find((el) => !el.classList.contains('btn-close')) ?? fields[0];
    first?.focus();
    return () => {
      if (opener?.isConnected) opener.focus();
      opener = null;
    };
  });

  function close() {
    open = false;
  }

  function onKey(e) {
    if (e.key === 'Escape') {
      close();
      return;
    }
    if (e.key !== 'Tab' || !dialogEl) return;
    const items = [...dialogEl.querySelectorAll(FOCUSABLE)];
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
</script>

<svelte:window onkeydown={open ? onKey : undefined} />

{#if open}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="modal fade show d-block" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="modal-title" onmousedown={close}>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="modal-dialog modal-dialog-centered {size}"
      onmousedown={(e) => e.stopPropagation()}
    >
      <div class="modal-content" bind:this={dialogEl}>
        <div class="modal-header">
          <h2 class="modal-title h5" id="modal-title">{title}</h2>
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
