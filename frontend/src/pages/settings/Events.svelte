<script>
  // Ereignis-Ansicht: LCMs eigenes Log in der Oberfläche.
  //
  // Warum: Wer LCM über den Browser bedient, kam an seine eigenen
  // Störungsmeldungen bisher nicht heran - die stehen auf dem LCM-Host in
  // `journalctl -u lcm`. Genau die Zeilen, die eine Fehlersuche abkürzen,
  // lagen hinter einer SSH-Sitzung, die viele Betreiber gar nicht führen.
  import { onDestroy } from 'svelte';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  const LEVELS = ['', 'debug', 'info', 'warn', 'error'];
  // Mehr als das ist in einer Ansicht nicht mehr zu lesen; der Server deckelt
  // zusätzlich bei 2000.
  const MAX_LIVE = 2000;

  let entries = $state([]);
  let level = $state('');
  let query = $state('');
  let follow = $state(false);
  let error = $state('');
  let busy = $state(false);
  let logLevel = $state('info');
  let debugUntil = $state(null);

  let controller = null;
  let listEl = $state(null);

  async function load() {
    busy = true;
    error = '';
    try {
      const res = await api.system.logs({ lines: 300, level, q: query });
      entries = res.entries ?? [];
      logLevel = res.level ?? logLevel;
      scrollToEnd();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function loadLevel() {
    try {
      const res = await api.system.logLevel();
      logLevel = res.level;
      debugUntil = res.debug_until ?? null;
    } catch {
      /* Level ist Beiwerk - eine Störung hier darf die Ansicht nicht kippen. */
    }
  }

  // Der Strom beginnt am aktuellen Ende der Datei; der Verlauf davor kommt
  // aus load(). Beim Filterwechsel muss er neu aufgebaut werden, weil der
  // Server serverseitig filtert.
  function startFollow() {
    stopFollow();
    controller = new AbortController();
    const signal = controller.signal;
    api.system
      .streamLogs({ level, q: query }, signal, (event, data) => {
        if (event !== 'log') return; // "ping" ist nur der Herzschlag
        try {
          entries = [...entries, JSON.parse(data)].slice(-MAX_LIVE);
        } catch {
          /* unlesbares Ereignis übergehen statt die Ansicht zu verlieren */
        }
        scrollToEnd();
      })
      .catch((e) => {
        if (signal.aborted) return;
        error = e instanceof ApiError ? e.message : String(e);
        follow = false;
      });
  }

  function stopFollow() {
    controller?.abort();
    controller = null;
  }

  function toggleFollow() {
    follow = !follow;
    if (follow) startFollow();
    else stopFollow();
  }

  // Nach dem Anhängen ans Ende springen - aber nur, wenn der Betrachter dort
  // ohnehin steht. Wer nach oben gescrollt hat, liest etwas; ihn
  // wegzuschieben wäre das Gegenteil von hilfreich.
  function scrollToEnd() {
    if (!listEl) return;
    const amEnde = listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight < 80;
    if (!amEnde) return;
    queueMicrotask(() => listEl && (listEl.scrollTop = listEl.scrollHeight));
  }

  async function applyFilter() {
    await load();
    if (follow) startFollow();
  }

  async function toggleDebug() {
    busy = true;
    error = '';
    try {
      const res = await api.system.setDebug(logLevel !== 'debug');
      logLevel = res.level;
      debugUntil = res.debug_until ?? null;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  function badge(l) {
    switch ((l || '').toUpperCase()) {
      case 'ERROR': return 'text-bg-danger';
      case 'WARN': return 'text-bg-warning';
      case 'DEBUG': return 'text-bg-secondary';
      case 'INFO': return 'text-bg-primary';
      default: return 'text-bg-light text-dark';
    }
  }

  function zeit(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    return isNaN(d) ? iso : d.toLocaleTimeString();
  }

  $effect(() => {
    load();
    loadLevel();
  });

  onDestroy(stopFollow);
</script>

<SettingsLayout title={t('settings.events.title')}>
  <p class="text-body-secondary">{t('settings.events.intro')}</p>

  {#if error}
    <div class="alert alert-danger py-2 px-3" role="alert">{error}</div>
  {/if}

  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex flex-wrap gap-2 align-items-end">
        <div>
          <label class="form-label mb-1" for="ev-level">{t('settings.events.level')}</label>
          <select id="ev-level" class="form-select form-select-sm" style="width: 10rem"
            bind:value={level} onchange={applyFilter} data-testid="events-level">
            {#each LEVELS as l}
              <option value={l}>{l === '' ? t('settings.events.allLevels') : l}</option>
            {/each}
          </select>
        </div>
        <div class="flex-grow-1" style="min-width: 12rem">
          <label class="form-label mb-1" for="ev-q">{t('settings.events.search')}</label>
          <input id="ev-q" class="form-control form-control-sm" bind:value={query}
            onchange={applyFilter} placeholder={t('settings.events.searchPlaceholder')}
            data-testid="events-search" />
        </div>
        <button class="btn btn-sm btn-outline-secondary" onclick={load} disabled={busy}>
          {t('settings.events.reload')}
        </button>
        <button class="btn btn-sm {follow ? 'btn-primary' : 'btn-outline-primary'}"
          onclick={toggleFollow} data-testid="events-follow">
          {follow ? t('settings.events.following') : t('settings.events.follow')}
        </button>
      </div>
    </div>
  </div>

  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex flex-wrap gap-3 align-items-center">
        <div class="form-check form-switch mb-0">
          <input class="form-check-input" type="checkbox" role="switch" id="ev-debug"
            checked={logLevel === 'debug'} disabled={busy}
            onchange={toggleDebug} data-testid="events-debug" />
          <label class="form-check-label" for="ev-debug">{t('settings.events.debugLabel')}</label>
        </div>
        {#if debugUntil}
          <span class="badge text-bg-warning" data-testid="events-debug-until">
            {t('settings.events.debugUntil', { time: zeit(debugUntil) })}
          </span>
        {/if}
      </div>
      <div class="form-text mt-2">{t('settings.events.debugHint')}</div>
    </div>
  </div>

  <div class="card">
    <div class="card-body p-0">
      <div bind:this={listEl} class="log-view" data-testid="events-list">
        {#each entries as e, i (i)}
          <div class="log-line">
            <span class="log-time">{zeit(e.time)}</span>
            <span class="badge {badge(e.level)} log-level">{e.level || '—'}</span>
            <span class="log-msg">{e.msg || e.raw}</span>
          </div>
        {:else}
          <div class="p-3 text-body-secondary small">{t('settings.events.empty')}</div>
        {/each}
      </div>
    </div>
  </div>
</SettingsLayout>

<style>
  /* Eigener Scrollbereich statt Seiten-Scroll: Beim Mitlaufen springt sonst
     die ganze Seite, und die Filter oben wären ständig weg. */
  .log-view {
    max-height: 60vh;
    overflow-y: auto;
    overflow-x: hidden;
    font-family: var(--bs-font-monospace, monospace);
    font-size: 0.82rem;
  }
  .log-line {
    display: flex;
    gap: 0.6rem;
    align-items: baseline;
    padding: 0.2rem 0.75rem;
    border-bottom: 1px solid var(--bs-border-color);
  }
  .log-line:last-child { border-bottom: 0; }
  .log-time {
    color: var(--bs-secondary-color);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .log-level {
    flex-shrink: 0;
    min-width: 3.6rem;
    font-size: 0.7rem;
  }
  /* Lange Zeilen umbrechen statt die Ansicht seitlich zu ziehen. */
  .log-msg { overflow-wrap: anywhere; }
</style>
