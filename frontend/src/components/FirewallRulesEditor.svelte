<script>
  // Regel-Editor der Multi-Backend-Firewall: eine Tabelle aus Freigabe-Regeln
  // (Port, TCP/UDP, IP-Version, erlaubte Quellen, Bemerkung) plus Vorschlags-Chips aus dem
  // Port-Inventar des Scans (lauschende Dienste) und den Docker-Exposures.
  // Wird vom Server-Firewall-Dialog UND vom Gruppen-Regel-Editor genutzt -
  // gleiche UI-Struktur für alle Firewall-Backends.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let {
    rules = $bindable([]),
    // listening = ListeningPort[] aus server.listening_ports (Scan).
    listening = [],
    // dockerPorts = veröffentlichte Docker-Host-Ports (Zahlen).
    dockerPorts = [],
    sshPort = 22,
    // showSSH: die (nie löschbare) SSH-Freigabe-Zeile anzeigen
    // (Server-Firewall-Dialog). Bei Gruppen-Regeln aus, da der SSH-Port je
    // Server unterschiedlich ist und automatisch offen bleibt.
    showSSH = false,
    // sshSources = Quell-Einschränkung der SSH-Freigabe (von WO darf sich
    // jemand anmelden) - gleiche Form wie bei den übrigen Regeln.
    sshSources = $bindable({ allowlist_ids: [], source_ips: [] }),
    // lcmSourceIP = Quell-IP, mit der LCM diesen Server erreicht. Als Vorlage
    // für die SSH-Quellen: genau diese Adresse muss offen bleiben, sonst
    // sperrt sich LCM selbst aus.
    lcmSourceIP = '',
    // onRescan = lauschende Dienste jetzt vom Server einlesen (optional).
    onRescan = null,
    rescanBusy = false,
    // allowlists = verfügbare benannte IP-Allowlists [{id, name, entries}].
    // Ist eine (oder mehrere) an einer Regel gewählt, gibt sie den Port NUR
    // für die Quell-IPs dieser Listen frei.
    allowlists = [],
    disabled = false,
  } = $props();

  // --- Allowlist-Menü (Popover) ---------------------------------------------
  //
  // Das Menü liegt im Top-Layer statt absolut positioniert in der Zelle. Vorher
  // steckte es in `.table-responsive` (overflow-x: auto) und wurde an dessen
  // Rand abgeschnitten: Bei mehreren Regeln war die Auswahl nicht mehr
  // erreichbar, stattdessen entstand ein zweiter Scrollbereich in der Tabelle.
  //
  // Der Top-Layer löst das grundsätzlich - er ist von overflow, z-index und
  // transformierten Vorfahren unabhängig. Das Element bleibt dabei an seiner
  // Stelle im DOM (nur die Darstellung wandert), deshalb greifen die
  // bestehenden Test-Selektoren und die Formularbindungen unverändert.
  //
  // Die Position müssen wir selbst setzen: Im Top-Layer besteht keine
  // Beziehung mehr zum Auslöser. (CSS Anchor Positioning könnte das nativ,
  // ist aber noch nicht überall verfügbar.)
  let openMenuId = $state(null);

  function placeMenu(id) {
    const menu = document.getElementById(id);
    const btn = document.querySelector(`[popovertarget="${CSS.escape(id)}"]`);
    if (!menu || !btn) return;

    const r = btn.getBoundingClientRect();
    const gap = 2;
    const edge = 8; // Mindestabstand zum Fensterrand
    const mh = menu.offsetHeight;
    const mw = menu.offsetWidth;

    // Nach unten öffnen; reicht der Platz nicht und ist oben mehr, nach oben
    // klappen. Sonst bliebe das Menü am unteren Rand kleben und die letzten
    // Einträge wären unerreichbar.
    let top = r.bottom + gap;
    if (top + mh > window.innerHeight - edge && r.top - gap - mh > edge) {
      top = r.top - gap - mh;
    }

    // Am Auslöser ausrichten, aber im sichtbaren Bereich halten.
    let left = r.left;
    if (left + mw > window.innerWidth - edge) {
      left = Math.max(edge, window.innerWidth - edge - mw);
    }

    menu.style.top = `${Math.max(edge, top)}px`;
    menu.style.left = `${Math.max(edge, left)}px`;
    // Mindestens so breit wie der Auslöser - schmaler wirkt wie ein Fehler.
    menu.style.minWidth = `${Math.max(r.width, 224)}px`;
  }

  function onMenuToggle(e, id) {
    if (e.newState === 'open') {
      openMenuId = id;
      placeMenu(id);
    } else if (openMenuId === id) {
      openMenuId = null;
    }
  }

  // Solange ein Menü offen ist, der Position folgen: Ein Scroll in der Tabelle
  // oder im Dialog würde den Auslöser sonst unter dem stehenden Menü
  // wegziehen. `true` = Capture-Phase, damit auch das Scrollen innerer
  // Container erfasst wird.
  $effect(() => {
    if (!openMenuId) return;
    const reposition = () => placeMenu(openMenuId);
    window.addEventListener('scroll', reposition, true);
    window.addEventListener('resize', reposition);
    return () => {
      window.removeEventListener('scroll', reposition, true);
      window.removeEventListener('resize', reposition);
    };
  });

  // Die SSH-Freigabe gilt immer für beide Adressfamilien - die Einschränkung
  // findet über die erlaubten Quellen statt (nur Anzeige).
  let sshVersion = $derived(t('firewall.editor.anyVersion'));

  // Vorschläge: lauschende Sockets, die noch nicht abgedeckt sind (SSH ist
  // immer freigegeben; ein Eintrag gilt als abgedeckt, sobald eine Regel mit
  // gleichem Port+Protokoll existiert). Wildcard-Sockets (0.0.0.0 und ::)
  // desselben Dienstes werden zu EINEM Chip zusammengefasst - sonst erschiene
  // z. B. sshd doppelt (v4- und v6-Socket mit identischem Label).
  let suggestions = $derived.by(() => {
    const seen = new Set();
    const docker = new Set(dockerPorts ?? []);
    return (listening ?? []).filter((l) => {
      if (l.port === sshPort || rules.some((r) => Number(r.port) === l.port && r.proto === l.proto)) return false;
      // Von Docker veröffentlichte Ports gehören nicht in die Vorschläge:
      // Docker trägt seine Weiterleitungen selbst in iptables nat/PREROUTING
      // ein und umgeht die Host-Firewall damit vollständig. Eine hier
      // angelegte Regel ändert an der Erreichbarkeit nichts - sie würde nur
      // vortäuschen, der Port sei durch LCM geregelt. Der Weg führt über die
      // Port-Bindung im Container (siehe Hinweis unten).
      if (docker.has(l.port)) return false;
      const wildcard = l.bind === '0.0.0.0' || l.bind === '::';
      const key = `${l.port}/${l.proto}/${wildcard ? '*' : l.bind}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  });
  // Reine Information, kein Vorschlag: diese Ports sind offen, ohne dass die
  // Firewall etwas dazu beitragen kann.
  let dockerBypassPorts = $derived((dockerPorts ?? []).filter((p) => p !== sshPort));

  function addRule(rule = { port: '', proto: 'tcp', ip_version: 'any', allowlist_ids: [], source_ips: [], comment: '' }) {
    rules = [...rules, rule];
  }

  // Quell-Einschränkung je Regel: Allowlist-Mehrfachauswahl (Checkboxen) plus
  // händisch eingetragene IPs/CIDRs - beides zusammen bildet die Union der
  // erlaubten Quellen.
  function toggleAllowlist(rule, id) {
    const cur = rule.allowlist_ids ?? [];
    rule.allowlist_ids = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id];
    rules = [...rules]; // Reaktivität anstoßen
  }
  // Freitext (komma-/leerzeichengetrennt) → source_ips-Liste; Commit bei
  // change (blur/Enter), damit das Feld beim Tippen nicht normalisiert wird.
  function setSourceIPs(rule, text) {
    rule.source_ips = (text ?? '').split(/[\s,]+/).filter(Boolean);
    rules = [...rules];
  }
  function sourceLabel(rule) {
    const names = allowlists.filter((a) => (rule.allowlist_ids ?? []).includes(a.id)).map((a) => a.name);
    const ips = rule.source_ips ?? [];
    if (ips.length > 0) names.push(t('firewall.editor.ipCount', { count: ips.length }));
    return names.length ? names.join(', ') : t('firewall.editor.allowlistNone');
  }

  // Dieselben Helfer für die SSH-Zeile: sie ist keine gewöhnliche Regel
  // (nicht löschbar, Port folgt dem Server), trägt ihre Quellen aber genauso.
  function toggleSSHAllowlist(id) {
    const cur = sshSources.allowlist_ids ?? [];
    sshSources = {
      ...sshSources,
      allowlist_ids: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id],
    };
  }
  function setSSHSourceIPs(text) {
    sshSources = { ...sshSources, source_ips: (text ?? '').split(/[\s,]+/).filter(Boolean) };
  }
  function addLCMSourceIP() {
    const ip = (lcmSourceIP ?? '').trim();
    if (!ip) return;
    const cur = sshSources.source_ips ?? [];
    if (!cur.includes(ip)) sshSources = { ...sshSources, source_ips: [...cur, ip] };
  }
  let sshSourceLabel = $derived.by(() => {
    const names = allowlists.filter((a) => (sshSources.allowlist_ids ?? []).includes(a.id)).map((a) => a.name);
    const ips = sshSources.source_ips ?? [];
    if (ips.length > 0) names.push(t('firewall.editor.ipCount', { count: ips.length }));
    return names.length ? names.join(', ') : t('firewall.editor.allowlistNone');
  });
  // Warnung: Eine Quell-Einschränkung ohne die eigene Adresse sperrt LCM aus.
  let sshLocksOutLCM = $derived.by(() => {
    const ips = sshSources.source_ips ?? [];
    const lists = sshSources.allowlist_ids ?? [];
    if (ips.length === 0 && lists.length === 0) return false; // keine Einschränkung
    if (lists.length > 0) return false; // Listeninhalt kennt nur der Server
    const ip = (lcmSourceIP ?? '').trim();
    return !!ip && !ips.includes(ip);
  });

  function removeRule(i) {
    rules = rules.filter((_, idx) => idx !== i);
  }

  function addSuggestion(l) {
    // Lauscht der Dienst auf allen Adressen (0.0.0.0/::), gilt die Freigabe
    // für beide Adressfamilien; lauscht er nur auf einer konkreten Adresse,
    // ist deren Familie die passende Vorgabe.
    const wildcard = l.bind === '0.0.0.0' || l.bind === '::';
    addRule({
      port: l.port,
      proto: l.proto,
      ip_version: wildcard ? 'any' : l.ip_version || 'any',
      // Der Dienstname ist die naheliegendste Bemerkung - genau die Frage,
      // die man sich später stellt: „wofür war dieser Port noch mal offen?"
      comment: l.process ?? '',
    });
  }

  // Client-seitige Spiegelung der Backend-Validierung (Portbereich, Quell-IPs
  // als IP oder CIDR) - nur fürs frühe Feedback, maßgeblich prüft das Backend.
  const reIP = /^([0-9.]+|[0-9a-fA-F:.]+)(\/\d{1,3})?$/;
  export function invalidRows() {
    if (showSSH && (sshSources.source_ips ?? []).some((ip) => !reIP.test(ip))) return true;
    return rules.some((r) => {
      const port = Number(r.port);
      if (!Number.isInteger(port) || port < 1 || port > 65535) return true;
      if ((r.source_ips ?? []).some((ip) => !reIP.test(ip))) return true;
      return false;
    });
  }
</script>

<div data-testid="firewall-rules-editor">
  {#if showSSH || rules.length > 0}
    <div class="table-responsive">
      <table class="table table-sm align-middle mb-2">
        <thead>
          <tr class="small text-body-secondary">
            <th style="width: 7.5rem">{t('firewall.editor.port')}</th>
            <th style="width: 6.5rem">{t('firewall.editor.proto')}</th>
            <th style="width: 7.5rem">{t('firewall.editor.ipVersion')}</th>
            <th style="width: 12rem">{t('firewall.editor.allowlist')}</th>
            <th style="width: 14rem">{t('firewall.editor.comment')}</th>
            <th style="width: 2.5rem"></th>
          </tr>
        </thead>
        <tbody>
          {#if showSSH}
            <!-- SSH-Freigabe: immer erzwungen, nicht löschbar; einschränken
                 lässt sie sich über die erlaubten Quellen. -->
            <tr data-testid="fw-ssh-row" class="table-active">
              <td>
                <div class="input-group input-group-sm">
                  <input type="number" class="form-control" value={sshPort} readonly disabled aria-label={t('firewall.editor.port')} />
                  <span class="input-group-text" title={t('firewall.editor.sshLockedHint')}>
                    <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 1a2 2 0 0 1 2 2v4H6V3a2 2 0 0 1 2-2m3 6V3a3 3 0 0 0-6 0v4a2 2 0 0 0-2 2v5a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2"/></svg>
                  </span>
                </div>
              </td>
              <td><input class="form-control form-control-sm" value="TCP" readonly disabled aria-label={t('firewall.editor.proto')} /></td>
              <td><input class="form-control form-control-sm" value={sshVersion} readonly disabled aria-label={t('firewall.editor.ipVersion')} /></td>
              <td>
                <!-- Quell-Einschränkung der SSH-Freigabe: dieselbe Auswahl wie
                     bei jeder anderen Regel. Ohne sie ist SSH von überall
                     erreichbar - was für den Standard-Port der häufigste
                     Grund für Anmelde-Versuche fremder Rechner ist. -->
                <button type="button" class="form-select form-select-sm text-truncate fw-allowlist-toggle"
                  data-testid="fw-ssh-sources" popovertarget="fw-allow-ssh"
                  title={t('firewall.editor.sshSourcesHint')} {disabled}
                  aria-haspopup="true" aria-expanded={openMenuId === 'fw-allow-ssh'}
                  class:text-body-secondary={(sshSources.allowlist_ids ?? []).length === 0 && (sshSources.source_ips ?? []).length === 0}>
                  {sshSourceLabel}
                </button>
                <div id="fw-allow-ssh" popover="auto" class="fw-allowlist-menu card card-body p-2"
                  ontoggle={(e) => onMenuToggle(e, 'fw-allow-ssh')}>
                  {#if allowlists.length > 0}
                    {#each allowlists as a (a.id)}
                      <label class="d-flex align-items-center gap-2 small mb-1">
                        <input type="checkbox" class="form-check-input mt-0" {disabled}
                          checked={(sshSources.allowlist_ids ?? []).includes(a.id)}
                          onchange={() => toggleSSHAllowlist(a.id)} />
                        <span>{a.name}</span>
                      </label>
                    {/each}
                    <hr class="my-1" />
                  {/if}
                  <label class="small text-body-secondary mb-1" for="fw-src-ips-ssh">{t('firewall.editor.sourceIPs')}</label>
                  <input id="fw-src-ips-ssh" class="form-control form-control-sm" data-testid="fw-ssh-source-ips" {disabled}
                    value={(sshSources.source_ips ?? []).join(', ')}
                    placeholder={t('firewall.editor.sourceIPsPlaceholder')}
                    class:is-invalid={(sshSources.source_ips ?? []).some((ip) => !reIP.test(ip))}
                    onchange={(e) => setSSHSourceIPs(e.target.value)} />
                  {#if lcmSourceIP}
                    <!-- Vorlage: die Adresse, mit der LCM diesen Server
                         erreicht. Fehlt sie in der Liste, sperrt die
                         Einschränkung LCM selbst aus. -->
                    <button type="button" class="btn btn-sm btn-outline-secondary mt-2" data-testid="fw-ssh-add-lcm-ip"
                      onclick={addLCMSourceIP} {disabled}>
                      + {t('firewall.editor.lcmSourceIP', { ip: lcmSourceIP })}
                    </button>
                  {/if}
                  <div class="form-text small mb-0">{t('firewall.editor.sshSourcesHint')}</div>
                </div>
                {#if sshLocksOutLCM}
                  <div class="form-text text-danger small" data-testid="fw-ssh-lockout-warning">
                    {t('firewall.editor.sshLockoutWarning', { ip: lcmSourceIP })}
                  </div>
                {/if}
              </td>
              <td class="text-body-secondary small">{t('firewall.editor.sshCommentFixed')}</td>
              <td class="text-end"><span class="text-body-secondary small" title={t('firewall.editor.sshLockedHint')}>SSH</span></td>
            </tr>
          {/if}
          {#each rules as rule, i (i)}
            <tr>
              <td><input type="number" min="1" max="65535" class="form-control form-control-sm" data-testid="fw-rule-port"
                bind:value={rule.port} placeholder="443" {disabled} /></td>
              <td>
                <select class="form-select form-select-sm" bind:value={rule.proto} {disabled}>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                </select>
              </td>
              <td>
                <select class="form-select form-select-sm" bind:value={rule.ip_version} {disabled}>
                  <option value="any">{t('firewall.editor.anyVersion')}</option>
                  <option value="v4">IPv4</option>
                  <option value="v6">IPv6</option>
                </select>
              </td>
              <td>
                <!-- Quell-Einschränkung: Allowlist-Mehrfachauswahl plus eigene
                     IPs/CIDRs (leichtgewichtiges details-Dropdown, ohne
                     Bootstrap-JS). -->
                <button type="button" class="form-select form-select-sm text-truncate fw-allowlist-toggle"
                  data-testid="fw-rule-allowlist" popovertarget={'fw-allow-' + i}
                  title={t('firewall.editor.allowlistHint')} {disabled}
                  aria-haspopup="true" aria-expanded={openMenuId === 'fw-allow-' + i}
                  class:text-body-secondary={(rule.allowlist_ids ?? []).length === 0 && (rule.source_ips ?? []).length === 0}>
                  {sourceLabel(rule)}
                </button>
                <div id={'fw-allow-' + i} popover="auto" class="fw-allowlist-menu card card-body p-2"
                  ontoggle={(e) => onMenuToggle(e, 'fw-allow-' + i)}>
                    {#if allowlists.length > 0}
                      {#each allowlists as a (a.id)}
                        <label class="d-flex align-items-center gap-2 small mb-1">
                          <input type="checkbox" class="form-check-input mt-0" {disabled}
                            checked={(rule.allowlist_ids ?? []).includes(a.id)}
                            onchange={() => toggleAllowlist(rule, a.id)} />
                          <span>{a.name}</span>
                        </label>
                      {/each}
                      <hr class="my-1" />
                    {/if}
                    <label class="small text-body-secondary mb-1" for={'fw-src-ips-' + i}>{t('firewall.editor.sourceIPs')}</label>
                    <input id={'fw-src-ips-' + i} class="form-control form-control-sm" data-testid="fw-rule-source-ips" {disabled}
                      value={(rule.source_ips ?? []).join(', ')}
                      placeholder={t('firewall.editor.sourceIPsPlaceholder')}
                      title={t('firewall.editor.sourceIPsHint')}
                      class:is-invalid={(rule.source_ips ?? []).some((ip) => !reIP.test(ip))}
                      onchange={(e) => setSourceIPs(rule, e.target.value)} />
                    <div class="form-text small mb-0">{t('firewall.editor.sourceIPsHint')}</div>
                </div>
              </td>
              <td>
                <input class="form-control form-control-sm" data-testid="fw-rule-comment"
                  bind:value={rule.comment} maxlength="120"
                  placeholder={t('firewall.editor.commentPlaceholder')}
                  title={t('firewall.editor.commentHint')} {disabled} />
              </td>
              <td class="text-end">
                <button type="button" class="btn btn-sm btn-outline-danger border-0" title={t('firewall.editor.removeRule')}
                  onclick={() => removeRule(i)} {disabled} aria-label={t('firewall.editor.removeRule')}>
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M5.5 5.5A.5.5 0 0 1 6 6v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5m2.5 0a.5.5 0 0 1 .5.5v6a.5.5 0 0 1-1 0V6a.5.5 0 0 1 .5-.5m3 .5a.5.5 0 0 0-1 0v6a.5.5 0 0 0 1 0z"/><path d="M14.5 3a1 1 0 0 1-1 1H13v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4h-.5a1 1 0 0 1-1-1V2a1 1 0 0 1 1-1H6a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1h3.5a1 1 0 0 1 1 1zM4.118 4 4 4.059V13a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V4.059L11.882 4zM2.5 3h11V2h-11z"/></svg>
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
  <button type="button" class="btn btn-sm btn-outline-secondary" data-testid="fw-add-rule" onclick={() => addRule()} {disabled}>
    + {t('firewall.editor.addRule')}
  </button>

  <div class="mt-2 small d-flex flex-wrap align-items-center gap-2">
    {#if onRescan}
      <!-- „vom System abfragen": Der Dialog zeigte bisher nur, was der letzte
           Server-Scan zufällig erfasst hatte. Ein seither installierter Dienst
           tauchte nie auf, ohne dass man erkennen konnte, woran es liegt. -->
      <button type="button" class="btn btn-sm btn-outline-secondary" data-testid="fw-rescan-ports"
        onclick={onRescan} disabled={disabled || rescanBusy}>
        {rescanBusy ? t('firewall.editor.rescanBusy') : t('firewall.editor.rescan')}
      </button>
    {/if}
    {#if (listening ?? []).length === 0}
      <span class="text-body-secondary" data-testid="fw-no-listening">{t('firewall.editor.noListening')}</span>
    {/if}
  </div>

  {#if suggestions.length > 0}
    <div class="mt-2 small" data-testid="fw-suggestions">
      <span class="text-body-secondary">{t('firewall.editor.suggestionsListening')}</span>
      {#each suggestions as l (l.port + '/' + l.proto + '/' + l.ip_version + '/' + l.bind)}
        <button type="button" class="badge text-bg-primary border-0 ms-1" style="cursor: pointer" data-testid="fw-suggest-listening"
          title={t('firewall.editor.addSuggestionTitle', { port: l.port, proto: l.proto })}
          onclick={() => addSuggestion(l)} {disabled}>
          + {l.port}/{l.proto}{#if l.process}&nbsp;·&nbsp;{l.process}{/if}{#if l.bind !== '0.0.0.0' && l.bind !== '::'}&nbsp;({l.bind}){/if}
        </button>
      {/each}
    </div>
  {/if}
  {#if dockerBypassPorts.length > 0}
    <div class="mt-2 small" data-testid="fw-docker-bypass">
      <span class="text-body-secondary">{t('firewall.editor.dockerBypass')}</span>
      {#each dockerBypassPorts as p (p)}
        <span class="badge text-bg-secondary ms-1">{p}</span>
      {/each}
      <div class="form-text">{t('firewall.editor.dockerBypassHint')}</div>
    </div>
  {/if}
</div>

<style>
  /* Allowlist-Dropdown: Auslöser sieht aus wie ein Select, das Menü liegt im
     Top-Layer (popover) und wird per JS am Auslöser ausgerichtet. */
  .fw-allowlist-toggle {
    cursor: pointer;
    white-space: nowrap;
    text-align: left;
  }

  .fw-allowlist-menu {
    /* Die Standard-Darstellung eines Popovers zentriert es im Fenster
       (inset: 0; margin: auto). Beides muss weg, sonst wirkt die berechnete
       Position nicht. */
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 0.5rem;
    /* Breite: mindestens so breit wie der Auslöser (per JS gesetzt), aber nie
       breiter als das Fenster - sonst entsteht auf Mobilgeräten ein
       horizontaler Überlauf. */
    max-width: min(24rem, calc(100vw - 1rem));
    /* Höhe begrenzt, damit lange Listen im Menü scrollen statt die Seite zu
       sprengen. 60vh lässt auch auf kleinen Schirmen Kontext stehen. */
    max-height: min(20rem, 60vh);
    overflow-y: auto;
    /* Nur wirksam, falls der Top-Layer nicht greift - dann verhält es sich
       wenigstens wie ein hoch gestapeltes fixed-Element. */
    z-index: 1080;
  }

  /* Geschlossene Popover ausblenden. Das erledigt normalerweise das
     Browser-Stylesheet ([popover]:not(:popover-open) { display: none }) - hier
     aber nicht: Bootstraps `.card` setzt `display: flex`, und Autoren-CSS
     schlägt das UA-Stylesheet grundsätzlich. Ohne diese Regel bleiben die
     Menüs aller übrigen Regeln dauerhaft sichtbar. */
  .fw-allowlist-menu:not(:popover-open) {
    display: none;
  }

  /* Ohne Popover-Unterstützung bliebe das Menü dauerhaft sichtbar. Der
     Rückfall versteckt es und macht es beim Fokussieren des Auslösers
     sichtbar, damit die Auswahl auch dort erreichbar bleibt. */
  @supports not selector(:popover-open) {
    .fw-allowlist-menu {
      display: none;
    }
    .fw-allowlist-toggle:focus + .fw-allowlist-menu,
    .fw-allowlist-menu:focus-within {
      display: block;
    }
  }
</style>
