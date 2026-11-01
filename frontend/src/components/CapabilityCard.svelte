<script>
  // Karte „Betriebsfähigkeit" der Serverseite: kann LCM auf diesem Server noch
  // handeln - und wenn nicht, seit wann, woran es liegt und was zu tun ist.
  //
  // Warum eine eigene Karte: Ein Job-Status sagt nur, wie EIN Lauf ausging.
  // Scheitern Updates seit Tagen an einer dpkg-Sperre, sind das lauter
  // gleich aussehende Einzelfehler im Job-Protokoll. Hier steht der
  // Dauerzustand je Kategorie (Verbindung, Paketverwaltung, Regeln, CVE-Scan).
  //
  // Die Karte lädt sich selbst (beim Einblenden) und bietet reload() an: Die
  // Serverseite ruft es auf, wenn ein Job zu Ende ist - so entsteht kein
  // zweiter Polling-Mechanismus neben ihrer Job-Sperre. Starten von
  // Wiederholung/Reparatur läuft über onrun(), damit der Job wie jede andere
  // Paketaktion der Seite verfolgt wird (Toasts, Sperre, Neuladen).
  //
  // Props:
  //   serverId       Server-ID
  //   serverName     Anzeigename (für den Bestätigungsdialog)
  //   packageManager 'apt' | 'zypper' | … (wählt die Beschriftung der Reparatur)
  //   canWrite       darf der Benutzer Aktionen starten (servers:write)
  //   busy           Seite ist gerade beschäftigt oder ein Job läuft (sperrt die Knöpfe)
  //   onrun          async (fn, label) => … ; fn startet den Job und liefert { job_id }
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';
  import { auth } from '../stores/auth.svelte.js';
  import Modal from './Modal.svelte';
  import { lastSeen } from '../lib/format.js';
  import { icons } from '../lib/icons.js';
  import { causeText, durationSince } from '../lib/capability.js';

  const t = (k, p) => i18n.t(k, p);

  let { serverId, serverName = '', packageManager = '', canWrite = false, busy = false, onrun } = $props();

  let caps = $state(null); // null = noch nicht geladen
  let loadError = $state('');
  let expandedExcerpt = $state({}); // Kategorie → Fehlertext ausgeklappt
  let outputs = $state({}); // Kategorie → { open, text } der Job-Ausgabe
  let dlg = $state({ open: false, kind: '', cap: null }); // Bestätigungsdialog

  // Mehr als dieser Umfang bekommt „Mehr anzeigen"; bis dahin passt der Text
  // ohnehin in die gekürzte Ansicht.
  const EXCERPT_SHORT = 160;

  export async function reload() {
    try {
      caps = await api.servers.capabilities(serverId);
      loadError = '';
    } catch (e) {
      loadError = e instanceof ApiError ? e.message : String(e);
    }
  }

  $effect(() => {
    reload();
  });

  // Stufe einer Kategorie: ein Fehlschlag färbt nach `effective`; ohne
  // Fehlschlag kann nur der veraltete Paketstand die Stufe heben. Fehlt
  // `effective` bei einem Fehlschlag, gilt vorsichtshalber „kritisch".
  function level(c) {
    if (c.state === 'failed') return c.effective === 'warning' ? 'warning' : 'critical';
    if (c.stale) return c.stale === 'critical' ? 'critical' : 'warning';
    return 'ok';
  }

  const LEVELS = {
    ok: { badge: 'bg-success-subtle text-success-emphasis border border-success-subtle', dot: 'text-success', rank: 0 },
    warning: { badge: 'bg-warning text-dark', dot: 'text-warning', rank: 1 },
    critical: { badge: 'bg-danger', dot: 'text-danger', rank: 2 },
  };
  const LEVEL_LABEL = {
    ok: 'capability.stateOk',
    warning: 'capability.stateWarning',
    critical: 'capability.stateCritical',
  };
  const stateLabel = (c) =>
    c.state !== 'failed' && c.stale ? t('capability.stateStale') : t(LEVEL_LABEL[level(c)]);

  let overall = $derived(
    (caps ?? []).reduce((worst, c) => (LEVELS[level(c)].rank > LEVELS[worst].rank ? level(c) : worst), 'ok'),
  );
  const SUMMARY_LABEL = {
    ok: 'capability.summaryOk',
    warning: 'capability.summaryWarning',
    critical: 'capability.summaryCritical',
  };

  // Eine Zeile bleibt knapp, solange nichts zu erzählen ist.
  const hasDetails = (c) => c.state === 'failed' || !!c.stale || !!c.warning_class;

  // Hinweis zur Fehlerklasse. Für die Verbindung passen die Paket-Hinweise zu
  // Netzwerk, Zeitüberschreitung und unbekannter Ursache nicht.
  function hintKey(c) {
    const cls = c.last_error_class || 'unknown';
    if (c.category === 'connection' && ['network', 'timeout', 'unknown'].includes(cls)) return 'connection';
    return cls;
  }

  const isLong = (text) => text.length > EXCERPT_SHORT || text.split('\n').length > 3;

  // Reparatur-Varianten: Beschriftung, Tooltip und Bestätigungstext. Die
  // Server-Aktion `fix_deps` gilt für apt und zypper; nur bei apt ist der
  // Befehl bekannt, sonst bleibt es bei der allgemeinen Beschriftung.
  const REPAIR_TEXT = {
    dpkg: {
      label: 'capability.repairDpkg',
      title: 'capability.repairDpkgTitle',
      confirm: 'capability.repairConfirmDpkg',
    },
    fixApt: {
      label: 'capability.repairFixApt',
      title: 'capability.repairFixAptTitle',
      confirm: 'capability.repairConfirmFixApt',
    },
    fix: {
      label: 'capability.repairFix',
      title: 'capability.repairFixTitle',
      confirm: 'capability.repairConfirmFix',
    },
  };
  function repairText(c) {
    if (c.repair === 'dpkg_configure') return REPAIR_TEXT.dpkg;
    return packageManager === 'apt' ? REPAIR_TEXT.fixApt : REPAIR_TEXT.fix;
  }

  const showRetry = (c) => canWrite && c.state === 'failed' && c.retryable;
  const showRepair = (c) => canWrite && !!c.repair;

  function ask(kind, cap) {
    dlg = { open: true, kind, cap };
  }

  async function run() {
    const { kind, cap } = dlg;
    dlg = { open: false, kind: '', cap: null };
    if (!cap) return;
    const category = t('capability.category.' + cap.category);
    if (kind === 'retry') {
      await onrun(
        () => api.servers.retryCapability(serverId, cap.category),
        t('capability.jobLabelRetry', { category }),
      );
    } else {
      await onrun(
        () => api.servers.repairPackages(serverId, cap.repair),
        t(repairText(cap).label),
      );
    }
    await reload();
  }

  // Job-Ausgabe auf Abruf: Sie kann groß sein und gehört nicht in jede Ansicht.
  async function toggleOutput(c) {
    const cur = outputs[c.category];
    if (cur?.open) {
      outputs[c.category] = { ...cur, open: false };
      return;
    }
    outputs[c.category] = { open: true, text: t('common.loading') };
    try {
      const res = await api.jobs.consoleOutput(c.last_job_id);
      outputs[c.category] = { open: true, text: res.output || t('jobs.noOutput') };
    } catch (e) {
      outputs[c.category] = { open: true, text: e instanceof ApiError ? e.message : String(e) };
    }
  }
</script>

<div class="card mb-3" data-testid="capability-card">
  <div class="card-body">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-1">
      <h2 class="h6 mb-0">{t('capability.title')}</h2>
      {#if caps}
        <span class="badge {LEVELS[overall].badge}" data-testid="capability-overall" data-level={overall}>
          <span class={overall === 'ok' ? 'text-success' : overall === 'warning' ? 'text-dark' : 'text-white'} aria-hidden="true">●</span>
          {t(SUMMARY_LABEL[overall])}
        </span>
      {/if}
    </div>

    {#if loadError}
      <div class="alert alert-danger py-2 small d-flex align-items-center gap-2 mb-0" data-testid="capability-error">
        <span>{t('capability.loadError', { error: loadError })}</span>
        <button class="btn btn-sm btn-outline-danger ms-auto" onclick={reload}>{@html icons.refresh} {t('capability.reload')}</button>
      </div>
    {:else if !caps}
      <div class="small text-body-secondary">{t('common.loading')}</div>
    {:else}
      <p class="small text-body-secondary mb-2">{t('capability.intro')}</p>
      <ul class="list-unstyled mb-0">
        {#each caps as c (c.category)}
          {@const lvl = level(c)}
          <li class="py-2 border-top" data-testid="capability-{c.category}" data-level={lvl}>
            <div class="d-flex flex-wrap align-items-center gap-2">
              <span class="fw-semibold" title={t('capability.categoryHelp.' + c.category)}>
                {t('capability.category.' + c.category)}
              </span>
              <span class="badge {LEVELS[lvl].badge}" data-testid="capability-{c.category}-state">
                <span class={lvl === 'ok' ? 'text-success' : lvl === 'warning' ? 'text-dark' : 'text-white'} aria-hidden="true">●</span>
                {stateLabel(c)}
              </span>
              {#if c.state === 'failed'}
                {#if c.failing_since}
                  <span class="small text-body-secondary" title={t('capability.failingSinceTitle', { when: lastSeen(c.failing_since) })}>
                    {t('capability.failingSince', { duration: durationSince(c.failing_since) })}
                  </span>
                {/if}
                {#if c.consecutive_failures > 0}
                  <span class="small text-body-secondary">· {t('capability.failures', { count: c.consecutive_failures })}</span>
                {/if}
              {/if}
            </div>

            {#if hasDetails(c)}
              <div class="mt-1 ps-1">
                {#if c.state === 'failed'}
                  {#if c.last_error_class}
                    <div class="small" data-testid="capability-{c.category}-cause">
                      <span class="{LEVELS[lvl].rank > 1 ? 'text-danger' : 'text-warning-emphasis'}">{@html icons.warning}</span>
                      {causeText(c.last_error_class, c.last_error_params)}
                    </div>
                  {/if}
                  <div class="small text-body-secondary mt-1">{t('capabilityHint.' + hintKey(c))}</div>

                  <!-- Bei einer belegten Sperre ist der Auszug nur „PID … (Name)“ -
                       das steht schon in der Ursache darüber. -->
                  {#if c.last_error_message && c.last_error_class !== 'lock_busy'}
                    <div class="mt-2">
                      <div class="small text-body-secondary">{t('capability.lastError')}</div>
                      <pre
                        class="bg-body-tertiary border rounded p-2 small font-monospace mb-1"
                        style="white-space: pre-wrap; overflow: hidden; {expandedExcerpt[c.category] || !isLong(c.last_error_message) ? 'max-height: 20em; overflow: auto;' : 'max-height: 4.5em;'}"
                        data-testid="capability-{c.category}-excerpt">{c.last_error_message}</pre>
                      {#if isLong(c.last_error_message)}
                        <button class="btn btn-link btn-sm p-0" onclick={() => (expandedExcerpt[c.category] = !expandedExcerpt[c.category])}>
                          {expandedExcerpt[c.category] ? t('capability.excerptLess') : t('capability.excerptMore')}
                        </button>
                      {/if}
                    </div>
                  {/if}

                  <div class="small text-body-secondary mt-1">
                    {c.last_success_at ? t('capability.lastSuccess', { when: lastSeen(c.last_success_at) }) : t('capability.neverSucceeded')}
                  </div>

                  {#if c.last_job_id}
                    <div class="small mt-1 d-flex flex-wrap align-items-center gap-2">
                      <span>{t('capability.lastJob', { name: c.last_job_name || c.last_job_id })}</span>
                      <!-- Ausgabe und Jobliste verlangen jobs:read - ohne das Recht
                           liefen beide ins Leere (403 bzw. leere Seite). -->
                      {#if auth.can('jobs:read')}
                        <button class="btn btn-link btn-sm p-0" onclick={() => toggleOutput(c)} data-testid="capability-{c.category}-output-toggle">
                          {outputs[c.category]?.open ? t('capability.hideOutput') : t('capability.showOutput')}
                        </button>
                        <a href="/jobs" use:link>{t('capability.jobHistory')}</a>
                      {/if}
                    </div>
                    {#if outputs[c.category]?.open}
                      <pre class="bg-body-tertiary border rounded p-2 small mt-1 mb-0" style="max-height: 320px; overflow: auto; white-space: pre-wrap;"
                        data-testid="capability-{c.category}-output">{outputs[c.category].text}</pre>
                    {/if}
                  {/if}
                {/if}

                {#if c.stale}
                  <div class="small text-warning-emphasis mt-1" data-testid="capability-{c.category}-stale">
                    {@html icons.clock}
                    {c.last_success_at
                      ? t('capability.staleSince', { duration: durationSince(c.last_success_at) })
                      : t('capability.staleNever')}
                  </div>
                {/if}

                {#if c.warning_class}
                  <!-- Teilproblem eines ERFOLGREICHEN Laufs (z. B. eine kaputte
                       Paketquelle unter vielen): gelb, ohne die Kategorie rot zu
                       färben. -->
                  <div class="small text-warning-emphasis mt-1" data-testid="capability-{c.category}-warning">
                    {@html icons.warning}
                    {t('capability.warningClass.' + c.warning_class)}{c.warning_message ? `: ${c.warning_message}` : ''}
                  </div>
                {/if}

                {#if showRepair(c) || showRetry(c)}
                  <div class="d-flex flex-wrap gap-2 mt-2">
                    {#if showRepair(c)}
                      <button class="btn btn-sm btn-outline-primary" disabled={busy}
                        title={busy ? t('capability.busyTitle') : t(repairText(c).title)}
                        data-testid="capability-{c.category}-repair"
                        onclick={() => ask('repair', c)}>{t(repairText(c).label)}</button>
                    {/if}
                    {#if showRetry(c)}
                      <button class="btn btn-sm btn-outline-secondary" disabled={busy}
                        title={busy ? t('capability.busyTitle') : t('capability.retryTitle')}
                        data-testid="capability-{c.category}-retry"
                        onclick={() => ask('retry', c)}>{@html icons.refresh} {t('capability.retry')}</button>
                    {/if}
                  </div>
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>

<Modal
  title={dlg.kind === 'repair' ? t('capability.repairConfirmTitle') : t('capability.retryConfirmTitle')}
  bind:open={dlg.open}>
  {#if dlg.cap}
    <p data-testid="capability-confirm-text">
      {#if dlg.kind === 'repair'}
        {t(repairText(dlg.cap).confirm, { server: serverName })}
      {:else}
        {dlg.cap.last_job_name
          ? t('capability.retryConfirmJob', { category: t('capability.category.' + dlg.cap.category), server: serverName, job: dlg.cap.last_job_name })
          : t('capability.retryConfirm', { category: t('capability.category.' + dlg.cap.category), server: serverName })}
      {/if}
    </p>
    <div class="d-flex gap-2">
      <button class="btn btn-outline-secondary" onclick={() => (dlg.open = false)}>{t('common.cancel')}</button>
      <button class="btn btn-primary" onclick={run} data-testid="capability-confirm-run">{t('capability.confirmRun')}</button>
    </div>
  {/if}
</Modal>
