<script>
  // Abschottung des CVE-Scanners: Läuft Trivy eingesperrt oder nicht?
  //
  // Warum das sichtbar sein MUSS: Trivy startet als Kindprozess von LCM und
  // liefe ohne Sandbox mit denselben Rechten - also mit Zugriff auf das
  // LCM-Datenverzeichnis, in dem Datenbank und Master-Key nebeneinander
  // liegen. Genau darauf zielte die Trivy-Lieferkettenkompromittierung im
  // März 2026. Ein stiller Rückfall auf „läuft eben ungesperrt" wäre die
  // schlechteste Variante: Dann hielte man sich für geschützt, ohne es zu
  // sein.
  //
  // Eigene Komponente, weil derselbe Zustand an zwei Stellen gehört: auf der
  // Sicherheits-Seite (dort steht der übrige Scanner-Zustand, und sie
  // funktioniert OHNE einen Servereintrag für den eigenen Rechner) und auf
  // der LCM-Host-Karte, wo zusätzlich die Nachrüst-Schaltfläche sitzt.
  import { icons } from '../lib/icons.js';
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  // info ist die Antwort von GET /security/scanner; actions ist optionaler
  // Zusatzinhalt unter dem Hinweis (die Nachrüst-Schaltfläche).
  let { info, actions = undefined } = $props();
</script>

{#if info?.available}
  <span class="small" data-testid="trivy-sandbox">
    {#if info.sandboxed}
      <span class="badge text-bg-success">{@html icons.shield} {t('security.sandbox.on', { backend: info.sandbox_backend })}</span>
    {:else}
      <span class="badge text-bg-warning text-dark" data-testid="trivy-sandbox-off">{@html icons.warning} {t('security.sandbox.off')}</span>
      <span class="d-block text-body-secondary mt-1">
        {t('security.sandbox.offHint')}{#if info.sandbox_note} <span class="fst-italic">{info.sandbox_note}</span>{/if}
      </span>
      {#if actions}{@render actions()}{/if}
    {/if}
  </span>
{/if}
