<script>
  // Web-Konsole: eine interaktive Shell auf einem Server, im Browser.
  //
  // Der Verbindungsaufbau ist zweistufig, und das ist kein Umstand, sondern
  // Absicht: Die WebSocket-Schnittstelle des Browsers kann keine eigenen
  // Kopfzeilen setzen, der Anmelde-Token käme also nicht mit. Ihn an die URL
  // zu hängen verbietet sich - URLs stehen in Zugriffs- und Proxy-Protokollen,
  // und dieser Zugang öffnet eine Root-Shell. Also holen wir zuerst eine
  // Einmal-Fahrkarte über den normal angemeldeten Weg.
  import { onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let { serverId, serverName } = $props();

  let host = $state(null);
  let state = $state('idle'); // idle | connecting | open | closed | error
  let error = $state('');

  let term = null;
  let fit = null;
  let ws = null;
  let resizeObserver = null;

  async function open() {
    if (state === 'connecting' || state === 'open') return;
    state = 'connecting';
    error = '';
    try {
      const { ticket } = await api.servers.terminalTicket(serverId);
      startTerminal(ticket);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
      state = 'error';
    }
  }

  function startTerminal(ticket) {
    term = new Terminal({
      convertEol: true,
      fontSize: 13,
      fontFamily: 'var(--bs-font-monospace, monospace)',
      // Der Rücklauf ist bewusst knapp: Die vollständige Sitzung steht
      // ohnehin im SSH-Protokoll, hier geht es nur ums Arbeiten.
      scrollback: 5000,
    });
    fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${location.host}/api/v1/servers/${serverId}/terminal`
      + `?ticket=${encodeURIComponent(ticket)}`
      + `&cols=${term.cols}&rows=${term.rows}&term=xterm-256color`;

    ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      state = 'open';
      term.focus();
    };
    ws.onmessage = (ev) => {
      term.write(new Uint8Array(ev.data));
    };
    ws.onerror = () => {
      error = t('terminal.connectionFailed');
      state = 'error';
    };
    ws.onclose = () => {
      if (state !== 'error') state = 'closed';
      term?.writeln('\r\n\x1b[2m' + t('terminal.sessionEnded') + '\x1b[0m');
    };

    term.onData((data) => {
      if (ws?.readyState === WebSocket.OPEN) ws.send(data);
    });

    // Größenänderungen als Textnachricht, damit sie nicht als Eingabe im
    // Terminal landen (das Protokoll unterscheidet Text von Binär).
    const sendSize = () => {
      fit?.fit();
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      }
    };
    resizeObserver = new ResizeObserver(sendSize);
    resizeObserver.observe(host);
  }

  function close() {
    resizeObserver?.disconnect();
    resizeObserver = null;
    ws?.close();
    ws = null;
    term?.dispose();
    term = null;
    fit = null;
    if (state !== 'error') state = 'idle';
  }

  onDestroy(close);
</script>

<div class="card">
  <div class="card-body">
    <div class="d-flex justify-content-between align-items-start flex-wrap gap-2 mb-2">
      <div>
        <h2 class="h6 mb-1">{t('terminal.title')}</h2>
        <div class="small text-body-secondary">{t('terminal.subtitle', { name: serverName })}</div>
      </div>
      <div class="d-flex gap-2">
        {#if state === 'open'}
          <button class="btn btn-sm btn-outline-danger" onclick={close} data-testid="terminal-close">
            {t('terminal.close')}
          </button>
        {:else}
          <button class="btn btn-sm btn-primary" onclick={open}
            disabled={state === 'connecting'} data-testid="terminal-open">
            {state === 'connecting' ? t('terminal.connecting') : t('terminal.open')}
          </button>
        {/if}
      </div>
    </div>

    <!-- Was die Konsole kostet, gehört VOR den Klick, nicht in die Doku:
         Sie belegt den Server, und sie wird mitgeschnitten. -->
    <div class="alert alert-warning py-2 px-3 small mb-3" role="note">
      {t('terminal.notice')}
    </div>

    {#if error}
      <div class="alert alert-danger py-2 px-3" role="alert" data-testid="terminal-error">{error}</div>
    {/if}

    <div class="terminal-host" class:is-idle={state === 'idle'} bind:this={host}
      data-testid="terminal-host"></div>
  </div>
</div>

<style>
  /* Fester Rahmen mit eigenem Hintergrund: xterm zeichnet auf Canvas und
     braucht eine Fläche, die nicht mit dem Seitenthema wackelt. */
  .terminal-host {
    min-height: 26rem;
    background: #0a0b0e;
    border-radius: 4px;
    padding: 0.5rem;
    overflow: hidden;
  }
  .terminal-host.is-idle {
    min-height: 6rem;
    opacity: 0.35;
  }
</style>
