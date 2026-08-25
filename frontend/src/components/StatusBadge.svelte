<script>
  // Ampel-Badge für den Server-Status. Das Info-Icon (ⓘ) öffnet per Klick
  // ein Popover, das die Gründe (Insights) auflistet - „warum warnung/
  // kritisch?". Fixe Positionierung, damit es nicht vom Tabellen-Overflow
  // abgeschnitten wird.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let { status = 'red', insights = [] } = $props();

  // „Sehr gut" ist sattes Grün (bg-success), „OK" nur noch ein helles Grün
  // (subtle) - so hebt sich der makellose Zustand sichtbar ab. Der Punkt vor
  // dem Label ist ein einfacher „●"-Bullet statt eines Farb-Emoji (gleiches
  // Muster wie die Insights-Punkte unten) - theme-sauber via CSS-Textfarbe.
  const meta = {
    excellent: { cls: 'bg-success', key: 'excellent', dot: 'text-white' },
    green: { cls: 'bg-success-subtle text-success-emphasis border border-success-subtle', key: 'ok', dot: 'text-success' },
    yellow: { cls: 'bg-warning text-dark', key: 'warning', dot: 'text-dark' },
    red: { cls: 'bg-danger', key: 'critical', dot: 'text-white' },
  };
  let m = $derived(meta[status] ?? meta.red);
  let label = $derived(t('status.' + m.key));

  let open = $state(false);
  let px = $state(0);
  let py = $state(0);
  // bind:this-Ziel: Svelte 5 verlangt $state, sonst warnt der Compiler
  // (non_reactive_update) bei jedem Build.
  let btnEl = $state(null);

  function toggle(e) {
    open = !open;
    if (open) {
      const r = e.currentTarget.getBoundingClientRect();
      // Links am Icon ausrichten, aber nicht über den rechten Rand hinaus.
      px = Math.max(8, Math.min(r.left, window.innerWidth - 348));
      py = r.bottom + 4;
    }
  }

  function onDocClick(e) {
    if (btnEl && (btnEl === e.target || btnEl.contains(e.target))) return;
    open = false;
  }

  $effect(() => {
    if (!open) return;
    const close = () => (open = false);
    document.addEventListener('click', onDocClick, true);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
    return () => {
      document.removeEventListener('click', onDocClick, true);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
  });

  // Befundtext: Der Server liefert einen Übersetzungsschlüssel samt
  // Parametern; message ist die deutsche Fassung und greift nur, wenn ein
  // Befund (noch) keinen Schlüssel mitbringt.
  const insightText = (i) => (i.key ? t('insights.' + i.key, i.params) : i.message);

  // info = keine Probleme, sondern die Gründe, warum es bei „OK" (noch)
  // nicht für „Sehr gut" reicht - neutraler grauer Punkt.
  const dotClass = (sev) =>
    sev === 'critical' ? 'text-danger' : sev === 'info' ? 'text-secondary' : 'text-warning';
  // Popover-Überschrift: bei „OK" geht es nicht um Probleme, sondern um den
  // Weg zu „Sehr gut".
  let popTitle = $derived(status === 'green' ? t('status.whyNotExcellent') : t('status.why', { label }));
</script>

<span class="d-inline-flex align-items-center gap-1">
  <span class="badge {m.cls}"><span class="{m.dot}" aria-hidden="true">●</span> {label}</span>
  {#if insights && insights.length > 0}
    <button
      bind:this={btnEl}
      type="button"
      class="btn btn-link p-0 lh-1 text-body-secondary"
      style="text-decoration: none; font-size: 1rem"
      title={popTitle}
      aria-label={popTitle}
      onclick={toggle}>ⓘ</button>
  {/if}
</span>

{#if open}
  <div
    class="card shadow border"
    style="position: fixed; left: {px}px; top: {py}px; z-index: 1090; min-width: 260px; max-width: 340px"
    role="dialog">
    <div class="card-body p-2">
      <div class="small fw-semibold mb-2">{popTitle}</div>
      <ul class="list-unstyled mb-0 small">
        {#each insights as i (i.message)}
          <li class="d-flex gap-2 mb-1">
            <span class="{dotClass(i.severity)} lh-base">●</span>
            <span>{insightText(i)}</span>
          </li>
        {/each}
      </ul>
    </div>
  </div>
{/if}
