<script>
  // Fixierte Meldungs-Region (siehe stores/toast.svelte.js).
  //
  // Die Region liegt fest am unteren Rand und ist damit unabhaengig von der
  // Scrollposition sichtbar. z-index 1090 ist Bootstraps $zindex-toast: ueber
  // dem Modal (1055) und dessen Backdrop (1050) - Fehler aus einem Dialog
  // bleiben so lesbar, statt dahinter zu verschwinden.
  //
  // aria-live: Erfolg/Info werden hoeflich nachgereicht (polite), Fehler
  // unterbrechen (assertive) - sie erfordern eine Reaktion.
  import { toasts } from '../stores/toast.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  const CLASSES = { success: 'alert-success', error: 'alert-danger', info: 'alert-info' };
</script>

<div class="toast-region" data-testid="toast-region">
  <!-- Zwei getrennte Live-Bereiche: ein Wechsel der Dringlichkeit innerhalb
       EINES Bereichs wird von Screenreadern nicht zuverlaessig neu bewertet. -->
  <div aria-live="assertive" aria-atomic="false">
    {#each toasts.items.filter((x) => x.kind === 'error') as toast (toast.id)}
      <div class="alert {CLASSES[toast.kind]} alert-dismissible shadow d-flex align-items-start gap-2"
        role="alert" data-testid={toast.testid || 'toast'}>
        <span class="flex-grow-1">{toast.text}</span>
        <button type="button" class="btn-close" aria-label={t('common.close')}
          onclick={() => toasts.dismiss(toast.id)}></button>
      </div>
    {/each}
  </div>
  <div aria-live="polite" aria-atomic="false">
    {#each toasts.items.filter((x) => x.kind !== 'error') as toast (toast.id)}
      <div class="alert {CLASSES[toast.kind] ?? 'alert-secondary'} alert-dismissible shadow d-flex align-items-start gap-2"
        role="status" data-testid={toast.testid || 'toast'}>
        <span class="flex-grow-1">{toast.text}</span>
        <button type="button" class="btn-close" aria-label={t('common.close')}
          onclick={() => toasts.dismiss(toast.id)}></button>
      </div>
    {/each}
  </div>
</div>

<style>
  .toast-region {
    position: fixed;
    right: 1rem;
    bottom: 1rem;
    z-index: 1090; /* Bootstrap $zindex-toast - ueber Modal + Backdrop */
    width: min(28rem, calc(100vw - 2rem));
    /* Ohne Meldungen darf die Region keine Klicks abfangen. */
    pointer-events: none;
  }
  .toast-region .alert {
    pointer-events: auto;
    margin-bottom: 0.5rem;
    /* Lange Fehlertexte (z.B. SSH-Ausgaben) duerfen die Seite nicht sprengen. */
    max-height: 40vh;
    overflow-y: auto;
    overflow-wrap: anywhere;
  }
  .toast-region .alert:last-child {
    margin-bottom: 0;
  }
</style>
