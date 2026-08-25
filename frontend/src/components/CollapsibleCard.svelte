<script>
  // Karte, die sich ein- und ausklappen lässt - standardmäßig zu.
  //
  // Der Anlass: Auf der Sicherheits-Ansicht eines Servers stehen mehrere
  // Verwaltungs-Karten (fail2ban, CrowdSec, SSH-2FA) mit je einem Dutzend
  // Knöpfen und einer Sperrliste. Ausgeklappt füllten sie den Bildschirm,
  // obwohl man sie meist nur nachschlägt. Die Kopfzeile bleibt sichtbar und
  // trägt den Zustand - man sieht also weiterhin auf einen Blick, was
  // installiert und aktiv ist, ohne scrollen zu müssen.
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let { title, testid = undefined, badge, children } = $props();

  let expanded = $state(false);
  const uid = $props.id();
</script>

<div class="card mb-3" data-testid={testid}>
  <!-- Die ganze Kopfzeile ist die Schaltfläche: ein größeres Ziel als ein
       Pfeil am Rand, und mit Tastatur ohne Umweg erreichbar. -->
  <button
    type="button"
    class="card-header bg-transparent border-0 d-flex align-items-center gap-2 w-100 text-start"
    aria-expanded={expanded}
    aria-controls={`card-${uid}`}
    data-testid={testid ? `${testid}-toggle` : undefined}
    onclick={() => (expanded = !expanded)}
  >
    <span class="h6 mb-0">{title}</span>
    {#if badge}{@render badge()}{/if}
    <span class="ms-auto text-body-secondary small">
      {expanded ? t('common.collapse') : t('common.expand')}
      <span aria-hidden="true">{expanded ? '▾' : '▸'}</span>
    </span>
  </button>

  {#if expanded}
    <div class="card-body pt-0" id={`card-${uid}`}>
      {@render children()}
    </div>
  {/if}
</div>
