<script>
  // Root-Komponente: Navbar + Client-Side-Routing (svelte-spa-router,
  // hash-basiert - funktioniert ohne Server-Konfiguration im Single-Binary).
  import Router, { router } from 'svelte-spa-router';
  import { fly } from 'svelte/transition';
  import { api } from './api';
  import { auth } from './stores/auth.svelte.js';
  import { i18n } from './stores/i18n.svelte.js';
  import { icons } from './lib/icons.js';
  const t = (k, p) => i18n.t(k, p);
  // Der Theme-Store setzt beim Laden data-bs-theme auf <html> (Dark Mode).
  import './stores/theme.svelte.js';
  import Navbar from './components/Navbar.svelte';
  import Toasts from './components/Toasts.svelte';
  import Login from './pages/Login.svelte';
  import LinuxActivate from './pages/LinuxActivate.svelte';
  import Docs from './pages/Docs.svelte';
  import Activate from './pages/Activate.svelte';
  import Dashboard from './pages/Dashboard.svelte';
  import JoinWizard from './pages/JoinWizard.svelte';
  import ServerDetail from './pages/ServerDetail.svelte';
  import Groups from './pages/Groups.svelte';
  import Jobs from './pages/Jobs.svelte';
  import LinuxUsers from './pages/LinuxUsers.svelte';
  import LinuxUserProfiles from './pages/linuxusers/Profiles.svelte';
  import LinuxUserProfileBlocks from './pages/linuxusers/ProfileBlocks.svelte';
  import Security from './pages/Security.svelte';
  import Docker from './pages/Docker.svelte';
  import Account from './pages/Account.svelte';
  import NotFound from './pages/NotFound.svelte';
  // Einstellungs-Unterseiten
  import SettingsHome from './pages/settings/SettingsHome.svelte';
  import SettingsUsers from './pages/settings/Users.svelte';
  import SettingsApiKeys from './pages/settings/ApiKeys.svelte';
  import SettingsMCP from './pages/settings/MCP.svelte';
  import SettingsGeneral from './pages/settings/General.svelte';
  import SettingsSecurity from './pages/settings/Security.svelte';
  import SettingsEvents from './pages/settings/Events.svelte';
  import SettingsRepositories from './pages/settings/Repositories.svelte';
  import SettingsApps from './pages/settings/Apps.svelte';
  import SettingsAptCache from './pages/settings/AptCache.svelte';
  import SettingsDNS from './pages/settings/DNS.svelte';
  import SettingsTime from './pages/settings/Time.svelte';
  import SettingsCrowdSec from './pages/settings/CrowdSec.svelte';
  import SettingsAllowlists from './pages/settings/Allowlists.svelte';
  import SettingsBackups from './pages/settings/Backups.svelte';
  import SettingsSchedules from './pages/settings/Schedules.svelte';
  import SettingsCustomActions from './pages/settings/CustomActions.svelte';
  import MovedToLinuxUsers from './pages/settings/MovedToLinuxUsers.svelte';
  import SettingsNotifications from './pages/settings/Notifications.svelte';
  import SettingsAlerts from './pages/settings/Alerts.svelte';
  import SettingsSubscription from './pages/settings/Subscription.svelte';

  const routes = {
    '/': Dashboard,
    '/login': Login,
    '/servers/join': JoinWizard,
    '/servers/:id': ServerDetail,
    '/groups': Groups,
    '/jobs': Jobs,
    '/linux-users': LinuxUsers,
    '/linux-users/profiles': LinuxUserProfiles,
    '/linux-users/profile-blocks': LinuxUserProfileBlocks,
    '/security': Security,
    '/docker': Docker,
    '/linux-aktivierung': LinuxActivate,
    '/doku': Docs,
    '/doku/:slug': Docs,
    // Mein Konto (Profil, Passwort, 2FA) - über den Usernamen erreichbar.
    '/account': Account,
    // Einstellungen (Sektion mit Unterseiten; /settings leitet weiter)
    '/settings': SettingsHome,
    '/settings/users': SettingsUsers,
    '/settings/apikeys': SettingsApiKeys,
    '/settings/mcp': SettingsMCP,
    '/settings/security': SettingsSecurity,
    '/settings/events': SettingsEvents,
    '/settings/general': SettingsGeneral,
    '/settings/repositories': SettingsRepositories,
    '/settings/apps': SettingsApps,
    '/settings/apt-cache': SettingsAptCache,
    '/settings/dns': SettingsDNS,
    '/settings/time': SettingsTime,
    '/settings/crowdsec': SettingsCrowdSec,
    '/settings/allowlists': SettingsAllowlists,
    '/settings/backups': SettingsBackups,
    '/settings/schedules': SettingsSchedules,
    '/settings/custom-actions': SettingsCustomActions,
    // Umgezogen auf die Linux-Benutzer-Seite - die alten Pfade leiten weiter.
    '/settings/profiles': MovedToLinuxUsers,
    '/settings/profile-blocks': MovedToLinuxUsers,
    '/settings/notifications': SettingsNotifications,
    '/settings/alerts': SettingsAlerts,
    '/settings/subscription': SettingsSubscription,
    '*': NotFound,
  };

  // Hauptbereich der aktuellen Route (erstes Pfadsegment). Beim Wechsel
  // zwischen den großen Seiten (Dashboard → Gruppen → …) blendet der
  // Inhalt sanft ein; Navigation INNERHALB eines Bereichs (z.B. zwischen
  // Einstellungs-Unterseiten oder Server-Details) bleibt ohne Effekt.
  let section = $derived('/' + (router.location.split('/')[1] ?? ''));

  // ===========================================================================
  // IMPRESSUM - fest eincodiert. HIER die eigenen Angaben eintragen.
  // (Rechtlich verantwortlich für das Impressum ist der Betreiber der Instanz.)
  const IMPRINT = {
    operator: 'Techeve',
    owner: 'Tony Grätscher', // Inhaber
    address: 'Talweg 14',
    city: '07639 Bad Klosterlausnitz',
    country: 'Deutschland',
    email: 'info@techeve.de',
    web: 'https://techeve.de',
  };
  const year = new Date().getFullYear();
  // ===========================================================================

  // System-Info (öffentlich): Version/Build fürs Footer + Impressum-Popover.
  let sysInfo = $state(null);

  // Nach einem Update läuft auf dem Server eine neue Oberfläche - im Browser
  // aber weiter die alte: Die Seite ist längst geladen, ihr JavaScript liegt
  // im Speicher, und von den ausgetauschten Dateien erfährt sie nichts. Wer
  // nicht von sich aus neu lädt, bedient danach eine Oberfläche, die nicht
  // mehr zum Server passt.
  //
  // Beobachtet wird deshalb die Kennung des laufenden Builds. Sie zu holen
  // kostet einen kleinen, öffentlichen Aufruf; ändert sie sich, lädt die
  // Seite sich selbst neu. Der Hinweis davor ist kein Rückfragen-Dialog,
  // sondern eine Ansage - sonst verschwindet der eben noch getippte
  // Bildschirminhalt kommentarlos.
  let loadedBuild = null;
  let newBuildFound = $state(false);
  const buildPollMs = 60_000;
  const reloadDelayMs = 4_000;

  const buildKey = (i) => (i?.version ? `${i.version}/${i.build}` : null);

  async function checkBuild() {
    let info;
    try {
      info = await api.system.info();
    } catch {
      // Genau während des Updates ist der Dienst kurz weg. Kein Fehlerfall -
      // der nächste Durchlauf trifft ihn in der neuen Version an.
      return;
    }
    sysInfo = info;
    const key = buildKey(info);
    if (!key) return;
    if (loadedBuild === null) {
      loadedBuild = key; // erster Abruf: der Stand, mit dem diese Seite läuft
      return;
    }
    if (key !== loadedBuild && !newBuildFound) {
      newBuildFound = true;
      setTimeout(() => window.location.reload(), reloadDelayMs);
    }
  }

  checkBuild();
  $effect(() => {
    const timer = setInterval(checkBuild, buildPollMs);
    // Zusätzlich beim Zurückkommen zum Tab: Wer das Update angestoßen hat und
    // danach wieder hereinschaut, soll nicht erst den Takt abwarten müssen.
    const onVisible = () => {
      if (document.visibilityState === 'visible') checkBuild();
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
    };
  });

  // Update-Status: nur für eingeloggte Nutzer (Endpunkt ist authentifiziert).
  let updateInfo = $state(null);
  let dismissedVersion = $state(localStorage.getItem('lcm.update.dismissed') || '');
  $effect(() => {
    if (auth.isLoggedIn) {
      api.system
        .updateInfo()
        .then((u) => (updateInfo = u))
        .catch(() => {});
    } else {
      updateInfo = null;
    }
  });
  // „Jetzt prüfen" im Info-Fenster: fragt den Paketkanal des Hosts sofort ab,
  // statt auf den nächsten der dreistündigen Läufe zu warten.
  let checkingUpdate = $state(false);
  let checkError = $state('');
  // checkedNow: Es wurde von Hand geprüft - das Ergebnis gehört danach in
  // den Balken, auch wenn es „kein Update" lautet.
  let checkedNow = $state(false);
  async function checkUpdateNow() {
    checkingUpdate = true;
    checkError = '';
    try {
      // Fehler der Prüfung selbst (Quelle nicht erreichbar, Zugang abgelehnt)
      // stehen im zurückgelieferten Status; hier bleibt nur der Fall, dass
      // der Aufruf gar nicht durchkam.
      updateInfo = await api.system.checkUpdateNow();
      checkedNow = true;
      // Eine früher weggeklickte Version soll nach dem ausdrücklichen
      // Nachfragen wieder sichtbar sein - sonst bleibt der Balken stumm,
      // obwohl gerade jemand danach gefragt hat.
      dismissedVersion = '';
      localStorage.removeItem('lcm.update.dismissed');
      await loadSelfUpdate();
    } catch (e) {
      checkError = e?.message ?? String(e);
    } finally {
      checkingUpdate = false;
    }
  }

  // Banner nur, wenn eine neuere Version bekannt ist und diese nicht bereits
  // weggeklickt wurde (pro Version gemerkt).
  let updateAvailable = $derived(
    !!updateInfo?.update_available && updateInfo.latest_version !== dismissedVersion,
  );
  function dismissUpdate() {
    checkedNow = false;
    if (updateInfo?.latest_version) {
      dismissedVersion = updateInfo.latest_version;
      localStorage.setItem('lcm.update.dismissed', dismissedVersion);
    }
  }

  // ---- Selbst-Update ------------------------------------------------------
  // LCM kann sein eigenes Debian-Paket auf dem LCM-Host einspielen. Der
  // Zustand kommt vom Server, weil er den Neustart überdauern muss: Wer
  // während des Wartens die Seite neu lädt, soll den Balken wiederfinden.
  let selfUpdate = $state(null);
  let selfUpdateError = $state('');
  const selfUpdatePollMs = 5_000;

  async function loadSelfUpdate() {
    try {
      selfUpdate = await api.system.selfUpdateStatus();
    } catch {
      selfUpdate = null; // kein Recht oder Endpunkt nicht da - dann eben ohne
    }
  }
  $effect(() => {
    if (auth.isLoggedIn && auth.can('settings:manage')) {
      loadSelfUpdate();
    } else {
      selfUpdate = null;
    }
  });

  // Solange gewartet oder aktualisiert wird, den Stand nachführen - die
  // Liste der laufenden Jobs ist der Fortschritt, den es hier zu sehen gibt.
  let selfUpdateBusy = $derived(
    selfUpdate?.phase === 'waiting' || selfUpdate?.phase === 'backup' || selfUpdate?.phase === 'running'
  );
  $effect(() => {
    if (!selfUpdateBusy) return;
    const timer = setInterval(loadSelfUpdate, selfUpdatePollMs);
    return () => clearInterval(timer);
  });

  let startingUpdate = $state(false);
  async function startSelfUpdate() {
    startingUpdate = true;
    selfUpdateError = '';
    try {
      selfUpdate = await api.system.startSelfUpdate();
    } catch (e) {
      selfUpdateError = e?.message ?? String(e);
    } finally {
      startingUpdate = false;
    }
  }

  // Der Balken erscheint auch ohne neue Version - nämlich nach „Jetzt
  // prüfen". Wer danach fragt, will eine Antwort sehen, und „alles aktuell"
  // ist eine.
  let bannerVisible = $derived(auth.isLoggedIn && (updateAvailable || checkedNow || selfUpdateBusy));

  // Info-Fenster (zentriertes Modal; Klick auf den Copyright-Vermerk öffnet es).
  let showImprint = $state(false);
  let imprintEl = $state(null);
  $effect(() => {
    if (!showImprint) return;
    const onDoc = (e) => {
      if (imprintEl && !imprintEl.contains(e.target) && !e.target.closest('[data-testid="imprint-toggle"]')) {
        showImprint = false;
      }
    };
    const onKey = (e) => {
      if (e.key === 'Escape') showImprint = false;
    };
    document.addEventListener('click', onDoc, true);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('click', onDoc, true);
      document.removeEventListener('keydown', onKey);
    };
  });

  function fmtDate(s) {
    if (!s || s === 'unbekannt') return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleDateString('de-DE');
  }
</script>

<Navbar />
<!-- Meldungs-Region: fest positioniert, daher hier einmal global gemountet. -->
<Toasts />
{#if newBuildFound}
  <!-- Auch ohne Login: Die veraltete Oberfläche betrifft jeden, der die Seite
       gerade offen hat - der Anmeldebildschirm eingeschlossen. -->
  <div class="alert alert-success rounded-0 border-0 border-bottom py-2 update-banner" role="status" data-testid="reload-banner">
    <div class="container d-flex flex-wrap align-items-center gap-2">
      <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>
      <span>{t('update.reloading')}</span>
      <button type="button" class="btn btn-sm btn-outline-success ms-auto" onclick={() => window.location.reload()}>
        {t('update.reloadNow')}
      </button>
    </div>
  </div>
{/if}
{#if bannerVisible}
  <!-- Bündig unter der Navbar: deren mb-4 (1.5rem) wird hier aufgehoben und
       stattdessen UNTER dem Banner wieder eingefügt - sonst schwebt das Banner
       losgelöst im Abstand und klebt ohne Luft am Seiteninhalt.

       Ein Balken, drei Lagen: Läuft ein Selbst-Update, hat dessen Fortschritt
       Vorrang vor allem anderen. Sonst die neue Version - und wenn es keine
       gibt, aber jemand gerade ausdrücklich geprüft hat, die Entwarnung. -->
  <div
    class="alert {selfUpdateBusy ? 'alert-warning' : updateAvailable ? 'alert-info' : 'alert-success'} {selfUpdateBusy ? '' : 'alert-dismissible'} rounded-0 border-0 border-bottom py-2 update-banner"
    role="alert"
    data-testid="update-banner"
  >
    <div class="container d-flex flex-wrap align-items-center gap-2">
      {#if selfUpdateBusy}
        <span class="spinner-border spinner-border-sm" aria-hidden="true"></span>
        {#if selfUpdate.waiting_for?.length}
          <span data-testid="update-waiting">
            {t('update.waitingForJobs', {
              version: selfUpdate.target_version,
              jobs: selfUpdate.waiting_for.join(', '),
            })}
          </span>
        {:else if selfUpdate.phase === 'backup'}
          <!-- Die Sicherung läuft vor dem Update. Sie gehört benannt: Sonst
               wirkt die Wartezeit wie ein hängendes Update. -->
          <span data-testid="update-backup">{t('update.backupRunning', { version: selfUpdate.target_version })}</span>
        {:else}
          <span data-testid="update-installing">{t('update.installing', { version: selfUpdate.target_version })}</span>
        {/if}
      {:else if updateAvailable}
        <span>{@html icons.arrowUpCircle} {t('update.available', { latest: updateInfo.latest_version, current: updateInfo.current_version })}</span>
        {#if updateInfo.latest_url}
          <a href={updateInfo.latest_url} target="_blank" rel="noopener noreferrer" class="alert-link">{t('update.viewRelease')} ↗</a>
        {/if}
        {#if selfUpdate?.supported}
          <button
            type="button"
            class="btn btn-sm btn-primary"
            onclick={startSelfUpdate}
            disabled={startingUpdate}
            data-testid="update-now"
          >
            {#if startingUpdate}<span class="spinner-border spinner-border-sm me-1"></span>{/if}
            {t('update.installNow')}
          </button>
          <!-- Die Zusage steht am Knopf, nicht im Kleingedruckten: Vor dem
               Einspielen wird gesichert, und ohne Sicherung wird nicht
               aktualisiert. -->
          <span class="small text-body-secondary" data-testid="update-backup-hint">{t('update.backupFirst')}</span>
        {:else if selfUpdate?.reason}
          <!-- Warum es hier keine Schaltfläche gibt, gehört dazu: sonst sucht
               jemand nach einem Knopf, den es aus gutem Grund nicht gibt. -->
          <span class="small text-body-secondary" data-testid="update-unsupported">{selfUpdate.reason}</span>
        {/if}
      {:else}
        <span data-testid="update-current">{t('update.upToDate', { current: updateInfo?.current_version ?? '' })}</span>
      {/if}
      {#if selfUpdate?.backup_file && selfUpdateBusy}
        <span class="small text-body-secondary" data-testid="update-backup-file">
          {t('update.backupDone', { file: selfUpdate.backup_file })}
        </span>
      {/if}
      {#if selfUpdate?.phase === 'failed' && selfUpdate.error}
        <span class="small text-danger" data-testid="update-error">{selfUpdate.error}</span>
      {/if}
      {#if selfUpdateError}
        <span class="small text-danger">{selfUpdateError}</span>
      {/if}
      {#if !selfUpdateBusy}
        <button type="button" class="btn-close ms-auto" aria-label={t('update.dismiss')} onclick={dismissUpdate}></button>
      {/if}
    </div>
  </div>
{/if}
<main class="pb-5">
  {#if router.location.startsWith('/linux-aktivierung')}
    <!-- Öffentliche Self-Service-Aktivierung: auch ohne Login erreichbar. -->
    <LinuxActivate />
  {:else if router.location.startsWith('/aktivierung')}
    <!-- Öffentlich: LCM-Zugang aktivieren / Passwort per Reset-Link setzen. -->
    <Activate />
  {:else if router.location.startsWith('/doku')}
    <!-- Öffentlich: die Anleitung zum Einrichten des SSH-Schlüssels wird
         gebraucht, BEVOR man einen Zugang hat (etwa aus der Aktivierungs-Mail
         heraus). Die Seiten enthalten nichts Vertrauliches. -->
    <Router {routes} />
  {:else if !auth.isLoggedIn}
    <Login />
  {:else}
    {#key section}
      <div in:fly={{ y: 12, duration: 220 }}>
        <Router {routes} />
      </div>
    {/key}
  {/if}
</main>

<footer class="text-center text-body-secondary small py-3 border-top position-relative">
  <button
    type="button"
    class="btn btn-link btn-sm text-body-secondary text-decoration-none p-0 align-baseline"
    data-testid="imprint-toggle"
    title={t('footer.info')}
    onclick={() => (showImprint = !showImprint)}
  >© {year} {IMPRINT.operator}</button>
  <span class="mx-1">·</span>
  <!-- Der Commit gehört sichtbar hierher: Version und Build-Nummer allein sagen
       nicht eindeutig, welcher Quellstand läuft. Ein Build aus einem unsauberen
       Arbeitsbaum (dirty) bekommt zusätzlich ein deutliches Abzeichen - er darf
       nie mit einem Release verwechselt werden. -->
  <span data-testid="app-version">LCM v{sysInfo?.version ?? '…'} (Build {sysInfo?.build ?? '…'}{sysInfo?.commit ? `, ${sysInfo.commit}` : ''}){sysInfo ? ` - ${sysInfo.platform}` : ''}</span>
  {#if sysInfo?.dirty}
    <span class="badge text-bg-warning ms-1" title={t('footer.dirtyBuildHint')}>{t('footer.dirtyBuild')}</span>
  {/if}

  {#if showImprint}
    <!-- Abdunkelnder Hintergrund (Klick schließt über den Klick-außerhalb-Handler). -->
    <div class="position-fixed top-0 start-0 w-100 h-100" style="background: rgba(0, 0, 0, 0.5); z-index: 1055;"></div>
    <!-- Zentriertes Info-Fenster. -->
    <div
      bind:this={imprintEl}
      class="card shadow-lg border position-fixed top-50 start-50 translate-middle text-start"
      style="z-index: 1060; width: 480px; max-width: calc(100vw - 24px);"
      data-testid="imprint-popover"
      role="dialog"
      aria-modal="true"
      aria-label={t('footer.info')}
    >
      <div class="card-header d-flex align-items-center py-2">
        <h6 class="mb-0 flex-grow-1">{t('footer.info')}</h6>
        <button
          type="button"
          class="btn-close"
          aria-label={t('footer.close')}
          data-testid="imprint-close"
          onclick={() => (showImprint = false)}
        ></button>
      </div>
      <div class="card-body p-4">
        <!-- Firmenname, darunter mittig das LCM-Logo. -->
        <div class="text-center mb-3">
          <div class="fw-semibold fs-5">{IMPRINT.operator}</div>
          <img src="/logo.svg" alt={t('footer.logo')} width="128" height="128" class="mt-2" data-testid="imprint-logo" />
        </div>
        <div class="lh-sm">
          <div>{t('footer.owner')}: {IMPRINT.owner}</div>
          <div>{IMPRINT.address}</div>
          <div>{IMPRINT.city}</div>
          <div>{IMPRINT.country}</div>
        </div>
        <div class="mt-3 lh-sm">
          <div class="fw-semibold">{t('footer.contact')}</div>
          <div>{t('footer.email')}: <a href={`mailto:${IMPRINT.email}`}>{IMPRINT.email}</a></div>
          <div><a href={IMPRINT.web} target="_blank" rel="noopener noreferrer">{IMPRINT.web}</a></div>
        </div>
        <hr class="my-3" />
        <div class="text-body-secondary small lh-sm">
          <div>LCM v{sysInfo?.version ?? '…'} · Build {sysInfo?.build ?? '…'}</div>
          {#if fmtDate(sysInfo?.built_at)}<div>{t('footer.buildDate')}: {fmtDate(sysInfo?.built_at)}</div>{/if}
          {#if updateInfo?.latest_version}
            <!-- Der Kanal gehört dazu: „neueste Version" heißt je Kanal etwas
                 anderes, und ohne die Angabe wäre die Zahl nicht einzuordnen. -->
            <div>
              {t('footer.latestVersion')}: {updateInfo.latest_version}
              {#if updateInfo.channel}
                <span class="text-body-tertiary">({t(`footer.channel.${updateInfo.channel}`)})</span>
              {/if}
            </div>
          {/if}
          {#if updateInfo?.error}
            <div class="text-warning">{updateInfo.error}</div>
          {/if}
          {#if checkError}
            <div class="text-danger">{checkError}</div>
          {/if}
        </div>
        {#if auth.isLoggedIn}
          <button
            class="btn btn-outline-secondary btn-sm mt-3"
            onclick={checkUpdateNow}
            disabled={checkingUpdate}
            data-testid="update-check-now"
          >
            {#if checkingUpdate}<span class="spinner-border spinner-border-sm me-1"></span>{/if}
            {t('footer.checkNow')}
          </button>
        {/if}
      </div>
    </div>
  {/if}
</footer>

<style>
  /* Hebt das mb-4 (1.5rem) der Navbar auf, damit das Banner direkt an ihr
     anliegt, und stellt denselben Abstand unterhalb wieder her. */
  .update-banner {
    margin-top: -1.5rem;
    margin-bottom: 1.5rem;
  }
</style>
