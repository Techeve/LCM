<script>
  // Server-Detailansicht mit Tabs: Status/Hardware, Pakete, Repos, Jobs.
  // Aktionen: SSH härten, Firewall, User-Sync, Key-Rotation, Decommission.
  import { push, link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import StatusBadge from '../components/StatusBadge.svelte';
  import OsLogo from '../components/OsLogo.svelte';
  import SshSessions from '../components/SshSessions.svelte';
  import ReconnectWizard from '../components/ReconnectWizard.svelte';
  import Modal from '../components/Modal.svelte';
  import FirewallRulesEditor from '../components/FirewallRulesEditor.svelte';
  import Pagination from '../components/Pagination.svelte';
  import CollapsibleCard from '../components/CollapsibleCard.svelte';
  import { PAGE_SIZE, pageCount, pageSlice } from '../lib/paging.js';
  import SandboxBadge from '../components/SandboxBadge.svelte';
  import { lastSeen, fmtSize, severityBadge, severityLabel } from '../lib/format.js';
  import { waitForJob } from '../lib/jobs.js';
  import { toasts } from '../stores/toast.svelte.js';
  import { icons } from '../lib/icons.js';

  const t = (k, p) => i18n.t(k, p);

  let { params } = $props();
  let id = $derived(Number(params.id));

  // Virtualisierung: rohe systemd-detect-virt-Ausgabe → Symbol + Klartext
  // (Container / Virtuelle Maschine / physisches Blech).
  const CONTAINER_VIRT = ['lxc', 'lxc-libvirt', 'openvz', 'docker', 'podman', 'systemd-nspawn', 'rkt', 'wsl', 'proot'];
  const PRETTY_VIRT = {
    kvm: 'KVM', qemu: 'QEMU', lxc: 'LXC', 'lxc-libvirt': 'LXC (libvirt)',
    vmware: 'VMware', microsoft: 'Hyper-V', oracle: 'VirtualBox', xen: 'Xen',
  };
  function virtInfo(raw) {
    const v = (raw || '').toLowerCase().trim();
    if (!v) return { icon: icons.question, label: t('serverDetail.virt.unknown') };
    if (v === 'none') return { icon: icons.cpu, label: t('serverDetail.virt.bareMetal') };
    const name = PRETTY_VIRT[v] || v.charAt(0).toUpperCase() + v.slice(1);
    if (CONTAINER_VIRT.includes(v)) return { icon: icons.box, label: t('serverDetail.virt.container', { name }) };
    return { icon: icons.monitor, label: t('serverDetail.virt.vm', { name }) };
  }

  // Paketverwaltung menschenlesbar (apt/dnf/yum/zypper/pacman/apk).
  const PKG_MGR_LABELS = { apt: 'APT (dpkg)', dnf: 'DNF (RPM)', yum: 'YUM (RPM)', zypper: 'Zypper (RPM)', pacman: 'pacman', apk: 'apk (Alpine)' };
  function pkgMgrLabel(m) {
    return PKG_MGR_LABELS[m] || (m ? m : t('serverDetail.pkgMgr.unknown'));
  }

  // OS-Support-Badge aus status.os_support. EOL (nicht unterstützt) und „läuft
  // in weniger als einem Monat aus" (eol_soon) sind beide kritisch → rot.
  function supportBadge(s) {
    if (!s?.known) return 'border text-body-secondary';
    if (!s.supported) return 'bg-danger';
    return s.eol_soon ? 'bg-warning text-dark' : 'bg-success';
  }
  // Der erklärende Satz zum Support-Stand kommt wie die Status-Befunde als
  // Schlüssel samt Parametern; summary ist die deutsche Rückfallebene.
  function supportSummary(s) {
    return s?.summary_key ? t('insights.' + s.summary_key, s.summary_params) : (s?.summary ?? '');
  }
  function supportLabel(s) {
    if (!s?.known) return t('serverDetail.support.unknown');
    if (!s.supported) return t('serverDetail.support.eol');
    if (s.eol_soon) return t('serverDetail.support.eolSoon', { eol: s.eol });
    return s.eol ? t('serverDetail.support.until', { eol: s.eol }) : t('serverDetail.support.supported');
  }

  let server = $state(null);
  let status = $state(null);
  // Kernel-Sicht (laufender Kernel + installierte Kernel-Pakete). Kommt mit
  // dem Status-Endpunkt, weil erst der Abgleich beider Angaben etwas aussagt.
  let kernel = $derived(status?.kernel ?? null);
  // Position des laufenden Kernels in der (neueste zuerst sortierten) Liste.
  // Alles davor ist NEUER und wartet auf den Neustart, alles danach ist die
  // ältere Rückfallebene. -1 = der laufende Kernel steckt in keinem Paket.
  let runningIndex = $derived((kernel?.installed ?? []).findIndex((k) => k.running));
  // Von außen erreichbare Docker-Ports (kommen mit dem Status-Endpunkt).
  // Docker legt sein DNAT vor die ufw-Kette - ohne diese Angabe wäre die
  // Firewall-Zeile eine unzutreffende Aussage über die Erreichbarkeit.
  let dockerExposures = $derived(status?.docker_port_exposures ?? []);
  let packages = $state([]);
  let snaps = $state([]); // zweite Paketverwaltung (nur wenn vorhanden)
  let outdated = $state([]);
  let repos = $state([]);
  let jobs = $state([]);
  let sessions = $state([]);
  let vulnReport = $state(null); // CVE-Scan-Ergebnis (Trivy)
  let vulnScanning = $state(false);
  let docker = $state(null); // Docker-Inventar (Container + Images + CVE-Zähler)
  let dockerBusy = $state(false);
  let storage = $state(null); // Speicher-Verlauf (Tagesdurchschnitte + Live-Wert)
  let serverUsers = $state(null); // Benutzer-Übersicht: gescannte Linux-Konten des Zielsystems
  let serverUsersBusy = $state(false);
  // Offene Benutzer-Abgleiche: liegengeblieben, weil der Server im Moment der
  // Änderung nicht erreichbar war (siehe user_sync_backlog.go).
  let pendingUserSyncs = $state([]);
  let lcmUsers = $state([]); // LCM-Linux-Benutzer für die Zuordnung
  let assignUserId = $state('');
  let openLogins = $state(''); // Konto, dessen Anmelde-Historie aufgeklappt ist
  let logins = $state([]);
  let loginsBusy = $state(false);
  let hideHealth = $state(true); // Health-Check-Pings standardmäßig ausblenden
  let tab = $state('overview');

  // Abgeschottete Verzeichnisse: Ein Profil GIBT Rechte - den Grundzustand
  // härtet man getrennt davon, je Verzeichnis.
  let hardenedPaths = $state([]);
  let hardenForm = $state({ path: '', group: '', unit: '' });
  // Vorschläge der generellen Härtung: Verzeichnisse mit Welt-Rechten, die
  // LCM auf dem Server gefunden hat. „chosen" hält die angehakten Pfade.
  let hardenSuggestions = $state(null);
  let hardenChosen = $state(new Set());

  // Health-Check-Session erkennen: neuer stabiler Zweck "health-check" sowie
  // ältere Aufzeichnungen, die noch den Rule-Namen tragen (rule:Health-Check …).
  function isHealthCheck(s) {
    const p = s.purpose || '';
    return p === 'health-check' || p.includes('Health-Check');
  }

  // Sichtbare Sessions (Health-Checks optional ausgeblendet, da sehr häufig).
  let visibleSessions = $derived(
    hideHealth ? sessions.filter((s) => !isHealthCheck(s)) : sessions,
  );
  // Dieselbe Ausblenden-Logik für die Job-Historie (Typ "health").
  let visibleJobs = $derived(hideHealth ? jobs.filter((j) => j.type !== 'health') : jobs);
  let busy = $state(false);

  // Job-Sperre: der aktuell laufende Job des Servers (alle 5 s gepollt).
  // Solange einer läuft, zeigt ein Banner Name + Laufzeit und die
  // Aktions-Buttons sind gesperrt - die UI weiß damit von der Sperre,
  // bevor man in einen 409 läuft.
  let activeJob = $state(null);
  let aborting = $state(false);
  let nowTick = $state(Date.now());
  let jobLocked = $derived(!!activeJob);
  let activeJobMinutes = $derived.by(() => {
    if (!activeJob?.started_at) return 0;
    return Math.max(0, Math.floor((nowTick - new Date(activeJob.started_at).getTime()) / 60000));
  });

  async function pollActiveJob() {
    try {
      const res = await api.servers.activeJob(id);
      const job = res?.job ?? null;
      const justFinished = activeJob && !job;
      activeJob = job;
      // Job gerade zu Ende gegangen: Ansicht auffrischen (Status, Werte).
      if (justFinished) await load();
    } catch {
      // Polling-Fehler still ignorieren (z.B. kurzzeitig offline).
    }
  }

  async function abortActiveJob() {
    if (!activeJob) return;
    if (!confirm(t('serverDetail.activeJob.confirmAbort', { name: activeJob.name }))) return;
    aborting = true;
    toasts.clear();
    try {
      await api.jobs.abort(activeJob.id);
      toasts.success(t('serverDetail.activeJob.aborted'));
      activeJob = null;
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      aborting = false;
    }
  }
  let allGroups = $state([]);
  let addGroupId = $state('');
  let knownRepos = $state([]);
  let addRepoKey = $state('');
  // Paket-Update-Zustand.
  let pkgVersionsFor = $state(null); // Paketname, dessen Versions-Auswahl offen ist
  let pkgVersions = $state([]); // geladene Versionen
  let pkgSelectedVersion = $state('');
  let pkgBusy = $state(false);
  let reconnectOpen = $state(false);
  let removeOpen = $state(false);
  let settingsOpen = $state(false);
  let restrictOpen = $state(false);
  let removePurge = $state(true); // Ziel-Bereinigung, Default an (wenn erreichbar)
  let firewallOpen = $state(false);
  let firewallRules = $state([]); // Regel-Objekte {port, proto, ip_version, allowlist_ids, source_ips, comment}
  // Quell-Einschränkung der SSH-Freigabe: von WO darf sich jemand anmelden.
  // Die Bind-Adresse oben ist die Gegenrichtung (auf welcher lokalen Adresse
  // gelauscht wird) - beides zusammen ergibt erst eine enge SSH-Freigabe.
  let firewallSSHSources = $state({ allowlist_ids: [], source_ips: [] });
  let listeningScanBusy = $state(false);
  let ipAllowlists = $state([]); // benannte IP-Allowlists (Auswahl in Firewall-Regeln)
  let firewallEditor = $state(); // Editor-Instanz (Client-Validierung)
  let repoOutput = $state(''); // Konsolen-Output der letzten Repo-Aktion

  // "Aktionen"-Dropdown (Zertifikat rotieren, Neu verbinden, User-Sync,
  // Neustart, Entfernen) - state-gesteuert ohne Bootstrap-JS, gleiches
  // Muster wie das Konto-Dropdown in der Navbar.
  let actionsMenu = $state(false);
  let actionsMenuEl; // Wurzel des Dropdowns (für Klick-außerhalb)
  function onActionsDocClick(e) {
    if (actionsMenuEl && !actionsMenuEl.contains(e.target)) actionsMenu = false;
  }
  $effect(() => {
    if (!actionsMenu) return;
    document.addEventListener('click', onActionsDocClick, true);
    return () => document.removeEventListener('click', onActionsDocClick, true);
  });
  function runMenuAction(fn) {
    actionsMenu = false;
    fn();
  }

  // Proxmox-Erkennung: Typ → Produktname. Auf Proxmox-Systemen sperrt LCM
  // Repos-Hinzufügen, ufw-Firewall und Benutzer-Sync (Proxmox verwaltet
  // diese Bereiche selbst) - die Buttons sind entsprechend deaktiviert.
  const proxmoxNames = {
    pve: 'Proxmox VE',
    pbs: 'Proxmox Backup Server',
    pmg: 'Proxmox Mail Gateway',
    pdm: 'Proxmox Datacenter Manager',
  };
  let isProxmox = $derived(!!server?.proxmox_type);
  // MikroTik RouterOS: nur Versions-Überwachung - Firewall/CVE/Repos/Benutzer/
  // Härtung sind gesperrt und werden in der Aktionsleiste ausgeblendet.
  let isRouterOS = $derived(server?.os_id === 'routeros');
  // Synology DSM: Überwachung über die DSM-Web-API - dieselbe Einschränkung
  // wie bei RouterOS (keine Shell/Paketverwaltung), deshalb wird die
  // Aktionsleiste über isAPIDevice gemeinsam ausgeblendet.
  let isDSM = $derived(server?.os_id === 'dsm');
  let isAPIDevice = $derived(isRouterOS || isDSM);

  // LCM-Host (localhost): der Server, auf dem LCM selbst läuft. Für ihn zeigen
  // wir eine Karte mit Trivy- und apt-cacher-ng-Einrichtung.
  // LCM-Host = Loopback UND Standard-SSH-Port (Spiegel von Server.IsLcmHost
  // im Backend): 127.0.0.1:<hoher Port> ist ein Port-Forward auf eine andere
  // Maschine, kein LCM-Host.
  let isLcmHost = $derived(
    ['localhost', '127.0.0.1', '::1'].includes((server?.host ?? '').trim().toLowerCase()) &&
      (!server?.ssh_port || server.ssh_port === 22),
  );
  let lcmHost = $state(null); // { trivy_installed, apt_cacher_installed, package_manager }
  // Stand des CVE-Scanners (Version + Schwachstellen-Datenbank). Nur für den
  // LCM-Host interessant - dort läuft der Scanner.
  let scannerInfo = $state(null);
  async function loadLcmHost() {
    try {
      lcmHost = await api.servers.lcmHostStatus(id);
    } catch {
      lcmHost = null;
    }
    try {
      scannerInfo = await api.system.scannerStatus();
    } catch {
      scannerInfo = null;
    }
  }
  async function installTrivy() {
    await action(() => api.servers.installTrivy(id), t('serverDetail.lcmHost.trivyStarted'));
    loadLcmHost();
  }
  // Sandbox nachrüsten: nur bubblewrap installieren und die Erkennung neu
  // auswerten - Trivy selbst bleibt, wie es ist (LCM sperrt beim Aufruf ein).
  async function installSandbox() {
    await action(() => api.servers.installSandbox(id), t('serverDetail.lcmHost.sandboxStarted'));
    // loadLcmHost holt beides: Host-Zustand und den (neu bewerteten) Scanner.
    loadLcmHost();
  }
  async function installAptCacher() {
    await action(() => api.servers.installAptCacher(id), t('serverDetail.lcmHost.aptCacherStarted'));
    loadLcmHost();
  }
  let lapiBouncer = $state(false);
  async function installCrowdSecLapi() {
    await refreshServer(() => api.servers.installCrowdSecLapi(id, { bouncer: lapiBouncer }), t('serverDetail.lcmHost.lapiStarted'));
    loadLcmHost();
  }
  // Statistiken, Neustart und permanentes Caching des apt-cacher-ng liegen jetzt
  // auf der eigenen Seite Einstellungen → APT-Cache (/settings/apt-cache).
  let proxmoxName = $derived(proxmoxNames[server?.proxmox_type] ?? 'Proxmox');
  let proxmoxHint = $derived(t('serverDetail.proxmox.hint'));

  // Eingeschränkter Service-User: die Kernfunktionen (Updates, Repos,
  // apt-Cache, Firewall, SSH-Konfiguration/-Port, Benutzer-Sync) laufen über
  // die sudo-Whitelist + LCM-Helper weiter; gesperrt bleiben nur freie
  // Skripte/Custom-Aktionen und der Neustart.
  let restricted = $derived(!!server?.restricted_sudo);
  let restrictedHint = $derived(t('serverDetail.restricted.hint'));
  // Anleitung „volle Rechte manuell wiederherstellen" (Modal aus dem
  // Aktionen-Dropdown, nur im eingeschränkten Modus).
  let unrestrictGuideOpen = $state(false);
  let unrestrictCommands = $derived.by(() => {
    const u = server?.service_user || 'lcm-svc';
    return [
      `echo '${u} ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/${u}`,
      `sudo chmod 440 /etc/sudoers.d/${u}`,
      `sudo rm -rf /home/${u}/.lcm/sudo-bin /usr/local/sbin/lcm-helper`,
    ].join('\n');
  });

  // CVE-relevante Container (Docker-Tab): Namen aus dem Server-Feld, für die
  // Toggle-Anzeige normalisiert (Backend vergleicht case-insensitiv).
  let cveRelevantSet = $derived(
    new Set(
      (server?.cve_relevant_containers ?? '')
        .split(',')
        .map((s) => s.trim().toLowerCase())
        .filter(Boolean),
    ),
  );
  async function toggleCveRelevant(c) {
    toasts.clear();
    try {
      server = await api.servers.dockerCveRelevance(id, c.name, !cveRelevantSet.has(c.name.toLowerCase()));
      status = await api.servers.status(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  // Benutzer-Sync-Schalter: ist er gesetzt, verteilt LCM auf diesem Server
  // keine Linux-Benutzer (Zuweisungen bleiben gespeichert).
  let userSyncDisabled = $derived(!!server?.user_sync_disabled);
  let userSyncHint = $derived(t('serverDetail.userSync.hint'));

  // Docker-Schalter: Updates abschalten (Inventar läuft weiter) und CVEs aus
  // Container-Images ganz außen vor lassen.
  let dockerUpdatesDisabled = $derived(!!server?.docker_updates_disabled);
  let dockerCVEsIgnored = $derived(!!server?.docker_cves_ignored);

  // Nichterreichbarkeit unkritisch: ist der Server offline, wird das erst nach
  // Ablauf der Kulanzfrist (Tage) kritisch; vorher behält er seinen Status.
  // Offline-Kennzeichen: nicht erreichbar und mindestens zwei fehlgeschlagene
  // Kontakte in Folge (Spiegel von domain.OfflineAfterFailedChecks). Ein
  // einzelner Fehlschlag ist Alltag und noch keine Aussage.
  const OFFLINE_AFTER_FAILED_CHECKS = 2;
  let isOffline = $derived(
    !!server && !server.reachable && (server.failed_checks ?? 0) >= OFFLINE_AFTER_FAILED_CHECKS,
  );

  let unreachableUncritical = $derived(!!server?.unreachable_uncritical);
  let graceDaysInput = $state(null);
  $effect(() => {
    if (server && graceDaysInput === null) graceDaysInput = server.unreachable_grace_days || 28;
  });
  const graceValid = (v) => Number.isInteger(Number(v)) && Number(v) >= 1 && Number(v) <= 365;

  // DNS: bis zu drei Nameserver setzen (freie IP oder Auswahl aus gepflegten
  // Vorgaben) und der DNS-Verfügbarkeitstest.
  let dnsInputs = $state(['', '', '']);
  let dnsInit = false; // einmalige Vorbelegung aus server.dns_servers (kein Reaktiv-Loop)
  $effect(() => {
    if (server && !dnsInit) {
      dnsInit = true;
      const list = (server.dns_servers || '').split(',').map((s) => s.trim()).filter(Boolean);
      dnsInputs = [list[0] || '', list[1] || '', list[2] || ''];
    }
  });
  // Vorgabe-Nameserver für die Auswahl (Datalist) - nur mit Einstellungs-Recht.
  let dnsPresets = $state([]);
  let dnsPresetsLoaded = false;
  $effect(() => {
    if (dnsPresetsLoaded || !auth.can('settings:manage')) return;
    dnsPresetsLoaded = true;
    api.system
      .getSettings()
      .then((s) => {
        dnsPresets = parseDnsPresets(s.dns_server_presets);
        // Dieselbe „Label = Wert"-Schreibweise wie bei den Nameservern; die
        // Zeitserver-Vorgaben kommen aus denselben globalen Einstellungen.
        ntpPresets = parseDnsPresets(s.ntp_server_presets);
        defaultTimezone = s.default_timezone || '';
      })
      .catch(() => {});
  });
  let ntpPresets = $state([]);
  let defaultTimezone = $state('');
  // Eingabefelder der Zeit-Aktionen. Die Vorbelegung passiert EINMALIG, wenn
  // die Serverdaten da sind - nicht in einem $effect: ein Effect, der seinen
  // eigenen State schreibt und liest, läuft sich in Svelte 5 sofort tot
  // (effect_update_depth_exceeded).
  let tzInput = $state('');
  let ntpInput = $state([]);
  let timeInputsPrefilled = false;
  function prefillTimeInputs() {
    if (timeInputsPrefilled || !server) return;
    timeInputsPrefilled = true;
    tzInput = server.timezone || defaultTimezone || '';
    const current = (server.ntp_servers || '').split(',').filter(Boolean);
    ntpInput = current.length > 0 ? current : ntpPresets.slice(0, 2).map((p) => p.ip);
  }
  function parseDnsPresets(raw) {
    return (raw || '')
      .split('\n')
      .map((line) => {
        const l = line.trim();
        if (!l) return null;
        const i = l.indexOf('=');
        const ip = (i >= 0 ? l.slice(i + 1) : l).trim();
        const label = i >= 0 ? l.slice(0, i).trim() : ip;
        return ip ? { label, ip } : null;
      })
      .filter(Boolean);
  }
  // Zeit: Zeitzone, Uhrenversatz und NTP. Der Versatz enthält die SSH-Laufzeit,
  // deshalb gilt erst eine deutliche Abweichung als Problem (30 s, wie im
  // Backend). In Containern kommt die Uhr vom Host - dort ist der Versatz
  // genauso relevant, nur ist er woanders zu beheben.
  const CLOCK_WARN_SECONDS = 30;
  let isContainer = $derived(
    ['lxc', 'lxc-libvirt', 'openvz', 'docker', 'podman', 'systemd-nspawn', 'rkt', 'wsl', 'proot'].includes(
      server?.virtualization,
    ),
  );
  const clockOff = (s) => Math.abs(s?.clock_offset_seconds ?? 0);
  const clockBadgeClass = (s) =>
    clockOff(s) >= CLOCK_WARN_SECONDS ? 'text-bg-warning' : 'text-bg-success';
  const clockLabel = (s) => {
    const off = s?.clock_offset_seconds ?? 0;
    if (Math.abs(off) < CLOCK_WARN_SECONDS) return t('serverDetail.time.inSync');
    return t(off > 0 ? 'serverDetail.time.ahead' : 'serverDetail.time.behind', { secs: Math.abs(off) });
  };
  async function checkTime() {
    await action(() => api.servers.timeCheck(id), t('serverDetail.time.checkLabel'));
  }
  async function applyTimezone(tz) {
    await action(() => api.servers.setTimezone(id, tz), t('serverDetail.time.timezoneLabel'));
  }
  async function applyNTP(servers) {
    await action(() => api.servers.configureNTP(id, servers), t('serverDetail.time.ntpLabel'));
  }
  const dnsBadgeClass = (s) =>
    s === 'full' ? 'text-bg-success' : s === 'partial' ? 'text-bg-warning' : s === 'none' ? 'text-bg-danger' : 'text-bg-secondary';
  const dnsFmtTime = (s) => {
    if (!s) return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleString();
  };
  async function applyDNS() {
    const servers = dnsInputs.map((s) => s.trim()).filter(Boolean);
    await action(() => api.servers.configureDNS(id, servers), t('serverDetail.dns.applyLabel'));
  }
  async function runDnsTest() {
    await action(() => api.servers.dnsTest(id), t('serverDetail.dns.testLabel'));
  }

  // Deep Scan (Kernel-Reboot-Lücke, Kernel-CVEs, Härtungs-Audit). Läuft als Job
  // (Lynis kann ~30-60s dauern) - nach dem Anstoß wird der Report gepollt, bis
  // sich der Zeitstempel ändert.
  let deepScan = $state(null); // DeepScanReport
  let deepScanBusy = $state(false);
  const dsBadgeClass = (s) =>
    s === 'critical' ? 'text-bg-danger' : s === 'warning' ? 'text-bg-warning' : 'text-bg-secondary';

  // Aufgeklappte Läufe: Report-ID → geladene Befunde. Ein Lauf wird erst beim
  // Aufklappen nachgeladen - die Liste selbst soll leichtgewichtig bleiben,
  // auch wenn ein Server 30 Läufe mit hunderten Befunden hinter sich hat.
  // Zeitstempel des Laufs - das Datum IST hier die Identität des Berichts.
  const dsFmtTime = (iso) => {
    const d = new Date(iso);
    return isNaN(d) ? '-' : d.toLocaleString();
  };
  let dsOpen = $state({});
  let dsDetail = $state({});
  async function toggleDeepScanReport(reportId) {
    if (dsOpen[reportId]) {
      dsOpen = { ...dsOpen, [reportId]: false };
      return;
    }
    dsOpen = { ...dsOpen, [reportId]: true };
    if (dsDetail[reportId]) return;
    try {
      dsDetail = { ...dsDetail, [reportId]: await api.servers.deepScanReportDetail(id, reportId) };
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      dsOpen = { ...dsOpen, [reportId]: false };
    }
  }
  // Behobene Befunde stehen als JSON-Liste am Lauf - sie zu zeigen ist der
  // eigentliche Fortschrittsnachweis („das hier ist seit dem letzten Mal weg").
  function resolvedTitles(rep) {
    try {
      return JSON.parse(rep?.resolved_titles || '[]');
    } catch {
      return [];
    }
  }
  // Befunde eines Laufs nach Kategorie gruppiert, innerhalb der Gruppe bleibt
  // die Sortierung des Servers (schwerste zuerst). Eine durchgehende Liste
  // aus 40 Zeilen ist genau das, was die Übersicht bisher gekostet hat.
  function groupByCategory(findings) {
    const out = new Map();
    for (const f of findings ?? []) {
      if (!out.has(f.category)) out.set(f.category, []);
      out.get(f.category).push(f);
    }
    return [...out.entries()];
  }
  async function runDeepScan() {
    deepScanBusy = true;
    toasts.clear();
    const before = deepScan?.deep_scan_at ?? null;
    try {
      await api.servers.deepScan(id);
      const startedToast = toasts.info(t('serverDetail.deepScan.started'), { timeout: 180000 });
      let done = false;
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 3000));
        const rep = await api.servers.deepScanReport(id);
        deepScan = rep;
        if (rep.deep_scan_at && rep.deep_scan_at !== before) {
          done = true;
          break;
        }
      }
      status = await api.servers.status(id);
      toasts.dismiss(startedToast);
      // Auch hier gilt: Erst der neue Zeitstempel belegt den Abschluss.
      toasts[done ? 'success' : 'info'](
        done ? t('serverDetail.deepScan.done') : t('serverDetail.notices.scanStillRunning'),
      );
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      deepScanBusy = false;
    }
  }
  async function installDeepScanTools() {
    deepScanBusy = true;
    toasts.clear();
    try {
      const res = await api.servers.installDeepScanTools(id);
      // „Werkzeuge installiert" wurde früher auch nach einem fehlgeschlagenen
      // Job gemeldet. trackJob unterscheidet die Fälle.
      await trackJob(res.job_id, t('serverDetail.deepScan.installTools'), t('serverDetail.deepScan.toolsInstalled'));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      deepScanBusy = false;
    }
  }

  // Sicherheit-Tools (fail2ban / CrowdSec) - zweistufiges Modal: erst Tool
  // wählen, dann tool-spezifische Felder. Die LCM-Quell-IP wird automatisch
  // in die Allowlist vorbelegt (Aussperr-Schutz).
  let securityToolOpen = $state(false);
  let securityTool = $state(''); // '' | 'fail2ban' | 'crowdsec'
  let secAllowlist = $state('');
  let secAllowlistIds = $state([]); // gewählte benannte Allowlists
  let secBouncer = $state(true);
  let secCollections = $state('crowdsecurity/sshd');
  let secLapiMode = $state('local'); // local | remote | console
  let secSettings = $state(null); // {crowdsec_lapi_configured, crowdsec_console_configured}
  function openSecurityTool() {
    securityTool = '';
    secAllowlist = server?.lcm_source_ip || '';
    secAllowlistIds = [];
    secBouncer = true;
    secCollections = 'crowdsecurity/sshd';
    secLapiMode = 'local';
    securityToolOpen = true;
    api.servers.ipAllowlists().then((l) => (ipAllowlists = l)).catch(() => (ipAllowlists = []));
    if (auth.can('settings:manage')) {
      api.system.getSettings().then((s) => (secSettings = s)).catch(() => {});
    }
  }
  function toggleSecAllowlist(idv) {
    secAllowlistIds = secAllowlistIds.includes(idv)
      ? secAllowlistIds.filter((x) => x !== idv)
      : [...secAllowlistIds, idv];
  }
  async function applySecurityTool() {
    // SSH-2FA laeuft ueber einen eigenen Pfad: Es ist kein Einbruchschutz
    // mit Allowlist und Dienst, sondern eine sshd-Konfiguration.
    if (securityTool === 'ssh-2fa') {
      securityToolOpen = false;
      await configureSSH2FA(true);
      return;
    }
    const opts = {
      tool: securityTool,
      allowlist_ips: secAllowlist.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean),
      allowlist_ids: secAllowlistIds,
    };
    if (securityTool === 'crowdsec') {
      opts.bouncer = secBouncer;
      opts.collections = secCollections.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
      opts.lapi_mode = secLapiMode;
    }
    securityToolOpen = false;
    await refreshServer(() => api.servers.configureSecurityTool(id, opts), t('serverDetail.securityTool.label'));
  }

  // --- Benutzer-Übersicht (gescannte Linux-Konten) ---------------------------
  // Aktionen auf Konten laufen synchron (kein Job); der Backend-Aufruf erhebt
  // den Bestand danach in derselben Verbindung neu - deshalb nach jeder
  // Aktion die Liste frisch holen.

  async function refreshServerUsersNow() {
    serverUsersBusy = true;
    try {
      serverUsers = await api.servers.refreshServerUsers(id);
      toasts.success(t('serverDetail.users.refreshed'));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      serverUsersBusy = false;
    }
  }

  async function serverUserAction(fn, okMsg) {
    serverUsersBusy = true;
    toasts.clear();
    try {
      await fn();
      toasts.success(okMsg);
      serverUsers = await api.servers.serverUsers(id);
      pendingUserSyncs = await api.servers.pendingUserSyncs(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      serverUsersBusy = false;
    }
  }

  async function toggleServerUser(u) {
    if (u.disabled) {
      await serverUserAction(
        () => api.servers.enableServerUser(id, u.username),
        t('serverDetail.users.enabled', { name: u.username }),
      );
      return;
    }
    const frage = u.managed
      ? t('serverDetail.users.blockConfirm', { name: u.username })
      : t('serverDetail.users.disableConfirm', { name: u.username });
    if (!confirm(frage)) return;
    await serverUserAction(
      () => api.servers.disableServerUser(id, u.username),
      t('serverDetail.users.disabled', { name: u.username }),
    );
  }

  async function removeServerUserNow(u) {
    if (!confirm(t('serverDetail.users.removeConfirm', { name: u.username }))) return;
    await serverUserAction(
      () => api.servers.removeServerUser(id, u.username),
      t('serverDetail.users.removed', { name: u.username }),
    );
  }

  // Anmelde-Historie eines Kontos auf-/zuklappen (wird erst beim Öffnen geholt).
  async function toggleLogins(u) {
    if (openLogins === u.username) {
      openLogins = '';
      return;
    }
    openLogins = u.username;
    logins = [];
    loginsBusy = true;
    try {
      logins = await api.servers.serverUserLogins(id, u.username);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      openLogins = '';
    } finally {
      loginsBusy = false;
    }
  }

  // Sitzungsdauer für die Anzeige (leer, wenn unbekannt oder laufend).
  function loginDuration(l) {
    if (l.still_active || !l.ended_at) return '';
    const min = Math.max(0, Math.round((new Date(l.ended_at) - new Date(l.started_at)) / 60000));
    if (min < 60) return t('serverDetail.users.durMin', { n: min });
    const h = Math.floor(min / 60);
    return t('serverDetail.users.durHours', { h, m: min % 60 });
  }

  // LCM-Benutzer auf diesen Server synchronisieren (bestehender Job-Endpunkt).
  async function syncLcmUsers() {
    serverUsersBusy = true;
    toasts.clear();
    try {
      const res = await api.servers.syncUsers(id);
      toasts.success(t('serverDetail.users.synced'));
      if (res?.output) toasts.success(res.output.trim().split('\n').slice(-1)[0]);
      serverUsers = await api.servers.refreshServerUsers(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      serverUsersBusy = false;
    }
  }

  async function assignLcmUser() {
    if (!assignUserId) return;
    await serverUserAction(
      () => api.servers.assignLinuxUser(id, Number(assignUserId)),
      t('serverDetail.users.assigned'),
    );
    assignUserId = '';
  }

  // --- SSH-2FA (TOTP neben dem SSH-Key) --------------------------------------

  async function configureSSH2FA(enable) {
    const msg = enable
      ? t('serverDetail.ssh2fa.enableConfirm')
      : t('serverDetail.ssh2fa.disableConfirm');
    if (!confirm(msg)) return;
    await refreshServer(() => api.servers.configureSSH2FA(id, enable), t('serverDetail.ssh2fa.label'));
  }

  // --- Installierte Sicherheits-Tools BEDIENEN -------------------------------
  // Bis hierher ließ sich fail2ban/CrowdSec nur einrichten. Dienst steuern,
  // Allowlist nachziehen, eine Sperre aufheben oder das Werkzeug wieder
  // entfernen ging nur per SSH von Hand - und genau die Sperrliste braucht
  // man dann, wenn man die Maschine gerade NICHT mehr erreicht.
  //
  // Alle Aktionen laufen als Job; die Rückmeldung folgt deshalb trackJob
  // (erst „gestartet", dann das tatsächliche Ergebnis). Solange eine Aktion
  // läuft, sind die Knöpfe des Werkzeugs gesperrt.
  const SECURITY_TOOLS = [
    { key: 'fail2ban', label: 'fail2ban' },
    { key: 'crowdsec', label: 'CrowdSec' },
  ];
  // Dienst-Aktionen der Werkzeug-Karte (Reihenfolge = Anzeigereihenfolge).
  const SEC_SERVICE_ACTIONS = ['start', 'stop', 'restart', 'enable', 'disable'];

  let secManageBusy = $state(''); // '' | '<tool>:<action>' - laufende Aktion
  let secBans = $state({}); // tool → Sperrliste
  let secBansError = $state({}); // tool → Fehlertext der Abfrage
  let secBansBusy = $state(''); // tool, dessen Liste gerade geladen wird
  let secManageIPs = $state({ fail2ban: '', crowdsec: '' });
  let secManageIds = $state({ fail2ban: [], crowdsec: [] });

  // Auf diesem Server installierte Werkzeuge (mit Live-Zustand).
  let installedSecTools = $derived(
    SECURITY_TOOLS.filter((tl) => server?.[`${tl.key}_installed`]).map((tl) => ({
      ...tl,
      active: !!server?.[`${tl.key}_active`],
    })),
  );
  // Verwaltung ist genau dann möglich, wenn auch das Backend sie zulässt:
  // Schreibrecht, kein laufender Job, kein eingeschränkter Modus (die
  // Aktionen greifen in Dienste und Systemdateien ein).
  let canManageSecTools = $derived(auth.can('servers:write') && !restricted);

  const secBusy = (tool) => secManageBusy.startsWith(`${tool}:`);

  function toggleSecManageList(tool, listId) {
    const cur = secManageIds[tool] ?? [];
    secManageIds = {
      ...secManageIds,
      [tool]: cur.includes(listId) ? cur.filter((x) => x !== listId) : [...cur, listId],
    };
  }

  // Sperrliste holen. Synchroner Endpunkt (kein Job): Wer sich selbst
  // ausgesperrt hat, soll die Liste sofort sehen. Fehler bleiben in der Karte
  // stehen - ein unerreichbarer Server ist hier der Normalfall, kein Vorfall.
  async function loadSecBans(tool) {
    secBansBusy = tool;
    try {
      const res = await api.servers.securityToolBans(id, tool);
      secBans = { ...secBans, [tool]: res?.bans ?? [] };
      secBansError = { ...secBansError, [tool]: '' };
    } catch (e) {
      secBans = { ...secBans, [tool]: [] };
      secBansError = { ...secBansError, [tool]: e instanceof ApiError ? e.message : String(e) };
    } finally {
      secBansBusy = '';
    }
  }

  async function secManage(tool, act, extra = {}) {
    secManageBusy = `${tool}:${act}`;
    toasts.clear();
    try {
      const res = await api.servers.manageSecurityTool(id, { tool, action: act, ...extra });
      const label = t('serverDetail.securityTool.manage.jobLabel', {
        tool: SECURITY_TOOLS.find((x) => x.key === tool)?.label ?? tool,
        action: t(`serverDetail.securityTool.manage.action.${act}`),
      });
      const job = await trackJob(res.job_id, label);
      // Zustand nachziehen: Der Dienst kann jetzt laufen oder weg sein.
      await load();
      if (job?.status !== 'failed' && act !== 'uninstall') await loadSecBans(tool);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      secManageBusy = '';
    }
  }

  async function secServiceAction(tool, act) {
    await secManage(tool, act);
  }

  async function secUninstall(tool) {
    const label = SECURITY_TOOLS.find((x) => x.key === tool)?.label ?? tool;
    if (!confirm(t('serverDetail.securityTool.manage.uninstallConfirm', { tool: label }))) return;
    await secManage(tool, 'uninstall');
  }

  async function secApplyAllowlist(tool) {
    await secManage(tool, 'allowlist', {
      allowlist_ips: (secManageIPs[tool] || '').split(/[\s,]+/).map((s) => s.trim()).filter(Boolean),
      allowlist_ids: secManageIds[tool] ?? [],
    });
  }

  async function secUnban(tool, ip) {
    await secManage(tool, 'unban', { unban_ip: ip });
  }

  // Beim Öffnen des Sicherheit-Tabs einmalig vorbereiten: benannte Allowlists
  // für die Mehrfachauswahl, LCM-Quell-IP als Vorbelegung (Aussperr-Schutz)
  // und die aktuellen Sperren der installierten Werkzeuge.
  let secManagePrepared = false;
  async function loadHardenedPaths() {
    try {
      hardenedPaths = await api.servers.hardenedPaths(id);
    } catch {
      hardenedPaths = [];
    }
  }

  // ACL-Unterstützung nachinstallieren. Ohne sie bleiben die
  // Verzeichnisrechte der Berechtigungsprofile auf diesem Server wirkungslos -
  // das Paket heißt auf allen Paketverwaltungen „acl".
  async function installACL() {
    await run(() => api.servers.installACLSupport(id), t('serverDetail.harden.aclInstalling'));
  }

  // Generelle Härtung: erst suchen, dann auswählen, dann in einem Lauf
  // abschotten. Bewusst zweistufig - was hier zugemacht wird, kann einen
  // Dienst betreffen, also soll es niemand blind auslösen.
  async function findHardenSuggestions() {
    await run(async () => {
      hardenSuggestions = await api.servers.hardenSuggestions(id);
      hardenChosen = new Set(hardenSuggestions.map((s) => s.path));
    }, t('serverDetail.harden.suggestFound', { count: hardenSuggestions?.length ?? 0 }));
  }

  function toggleSuggestion(path) {
    const next = new Set(hardenChosen);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    hardenChosen = next;
  }

  async function hardenChosenPaths() {
    const targets = (hardenSuggestions ?? [])
      .filter((s) => hardenChosen.has(s.path))
      .map((s) => ({ path: s.path, group: s.service_group || '', unit: '' }));
    if (!targets.length) return;
    await run(
      () => api.servers.hardenPathsBulk(id, targets),
      t('serverDetail.harden.suggestApplied', { count: targets.length }),
    );
    hardenSuggestions = null;
    await loadHardenedPaths();
  }

  async function hardenPath() {
    const f = { ...hardenForm };
    await run(
      () => api.servers.hardenPath(id, f.path, f.group, f.unit),
      t('serverDetail.harden.notices.hardened', { path: f.path }),
    );
    hardenForm = { path: '', group: '', unit: '' };
    await loadHardenedPaths();
  }

  async function restorePath(row) {
    if (!confirm(t('serverDetail.harden.confirmRestore', { path: row.path }))) return;
    await run(
      () => api.servers.restorePath(id, row.id),
      t('serverDetail.harden.notices.restored', { path: row.path }),
    );
    await loadHardenedPaths();
  }

  async function prepareSecurityManagement() {
    if (secManagePrepared || installedSecTools.length === 0) return;
    secManagePrepared = true;
    const src = server?.lcm_source_ip || '';
    secManageIPs = { fail2ban: src, crowdsec: src };
    if (ipAllowlists.length === 0) {
      try {
        ipAllowlists = await api.servers.ipAllowlists();
      } catch {
        ipAllowlists = [];
      }
    }
    for (const tl of installedSecTools) await loadSecBans(tl.key);
  }

  // SSH-Schutz-Einstellungen (eigener Tab). Der Port ist ein Eingabefeld
  // (wird erst per Aktion übernommen), Root-Login ein Sofort-Schalter.
  let rootLoginDisabled = $derived(!!server?.ssh_root_login_disabled);
  let portInput = $state(null);
  // Beim (Neu-)Laden des Servers das Portfeld mit dem aktuellen Wert füllen.
  $effect(() => {
    if (server && portInput === null) portInput = server.ssh_port;
  });
  async function changePort() {
    const p = Number(portInput);
    if (!Number.isInteger(p) || p < 1 || p > 65535 || p === server.ssh_port) return;
    await action(() => api.servers.changeSshPort(id, p), t('serverDetail.sshProtect.portChanged'));
    portInput = null; // nach dem Reload neu aus server.ssh_port setzen
  }

  // LCM Remote: Agent-Server (Transport MQTT statt SSH). SSH-spezifische
  // Aktionen (Härtung, Key-Rotation, Reconnect) entfallen; dafür gibt es
  // die Token-Erneuerung. agent_connected ist der Live-Zustand aus dem Hub.
  let isAgent = $derived(server?.transport === 'agent');
  let agentHint = $derived(t('serverDetail.agent.hint'));
  let agentToken = $state(''); // neu erzeugtes Token (einmalige Anzeige)
  let agentTokenCopied = $state(false);

  async function regenerateAgentToken() {
    if (!confirm(t('serverDetail.agent.regenerateConfirm'))) return;
    busy = true;
    toasts.clear();
    try {
      const res = await api.servers.regenerateAgentToken(id);
      agentToken = res.token;
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function copyAgentToken() {
    try {
      await navigator.clipboard.writeText(agentToken);
      agentTokenCopied = true;
      setTimeout(() => (agentTokenCopied = false), 2000);
    } catch {
      // Ohne Clipboard-Rechte kopiert der Nutzer von Hand.
    }
  }

  // Anzahl unverschlüsselter (http-)Paketquellen.
  let insecureRepos = $derived(repos.filter((r) => r.insecure).length);
  // Anwendungen, die nicht aus der Paketverwaltung stammen (siehe Reiter).
  let apps = $state(null);
  let appUpdates = $derived(apps?.detected?.filter((a) => a.update_available).length ?? 0);
  // Quellen, die sich auf http zurückstellen lassen: beim Scan aus dem
  // Protokoll der Umstellung ermittelt, ersatzweise die Distributions-Spiegel.
  let revertUrls = $derived((server?.https_revert_urls ?? '').split(',').filter(Boolean));

  // Nur apt-Systeme kennen apt-cacher-ng (APT-Cache-Anbindung) und die
  // http→https-Umstellung der sources.list - beide sind apt-spezifisch.
  let isApt = $derived((server?.package_manager ?? 'apt') === 'apt');
  // Katalog-Quellen, die zur Paketverwaltung DIESES Servers passen: eine
  // apt-Quelle lässt sich nicht auf einem dnf-System einrichten (der Server
  // wies sie ohnehin ab). Leerer Flag-Wert (Altbestand) gilt als apt.
  let serverKnownRepos = $derived(
    knownRepos.filter((r) => (r.package_manager || 'apt') === (server?.package_manager ?? 'apt'))
  );

  // Speicher-Verlaufsdiagramm (eigenes Inline-SVG, keine Chart-Lib):
  // belegter Anteil (%) je Tag als Flächen-/Liniendiagramm. Gibt die
  // vorberechneten SVG-Pfade + Achsen-Beschriftungen zurück.
  const CHART = { w: 720, h: 260, padL: 42, padR: 14, padT: 14, padB: 30, warn: 85 };
  let storageChart = $derived.by(() => {
    const hist = storage?.history ?? [];
    if (hist.length === 0) return null;
    const plotW = CHART.w - CHART.padL - CHART.padR;
    const plotH = CHART.h - CHART.padT - CHART.padB;
    const n = hist.length;
    const x = (i) => CHART.padL + (n === 1 ? plotW / 2 : (i * plotW) / (n - 1));
    const y = (pct) => CHART.padT + plotH - (Math.max(0, Math.min(100, pct)) * plotH) / 100;
    const pts = hist.map((h, i) => {
      const pct = h.disk_total_mb > 0 ? (h.disk_used_mb * 100) / h.disk_total_mb : 0;
      return { i, x: x(i), y: y(pct), pct, h };
    });
    const line = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
    const area = `M${pts[0].x.toFixed(1)},${(CHART.padT + plotH).toFixed(1)} `
      + pts.map((p) => `L${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ')
      + ` L${pts[n - 1].x.toFixed(1)},${(CHART.padT + plotH).toFixed(1)} Z`;
    return {
      line, area, pts, lastPt: pts[n - 1],
      warnY: y(CHART.warn),
      baseY: CHART.padT + plotH,
      first: hist[0].day,
      last: hist[n - 1].day,
      peak: Math.max(...pts.map((p) => p.pct)),
    };
  });

  // Chart-Hover: der am nächsten liegende Datenpunkt unter der Maus. Beim
  // Überfahren des Diagramms erscheint dort ein Punkt + ein Tooltip mit
  // Prozentwert und Tag. Berechnung in SVG-viewBox-Koordinaten (unabhängig
  // von der gerenderten Pixelbreite).
  let hoverPt = $state(null);
  function chartHover(e) {
    const chart = storageChart;
    if (!chart) return;
    const r = e.currentTarget.getBoundingClientRect();
    const vx = ((e.clientX - r.left) / r.width) * CHART.w;
    let nearest = chart.pts[0];
    for (const p of chart.pts) {
      if (Math.abs(p.x - vx) < Math.abs(nearest.x - vx)) nearest = p;
    }
    hoverPt = nearest;
  }
  // Tooltip-Box links am Punkt ausrichten, aber im Plot-Bereich halten.
  let hoverBox = $derived.by(() => {
    if (!hoverPt) return null;
    const w = 96;
    const x = Math.max(CHART.padL, Math.min(hoverPt.x + 8, CHART.w - CHART.padR - w));
    const above = hoverPt.y > CHART.padT + 34;
    const y = above ? hoverPt.y - 34 : hoverPt.y + 10;
    return { x, y, w, label: `${hoverPt.pct.toFixed(0)}% · ${hoverPt.h.day}` };
  });

  // Ernste CVEs (kritisch + hoch) für das Tab-Badge.
  let vulnSevereCount = $derived(
    (vulnReport?.summary?.critical ?? 0) + (vulnReport?.summary?.high ?? 0),
  );

  // Seitenweise Anzeige der CVE-Tabelle (clientseitig; die Liste eines einzelnen
  // Servers ist begrenzt, aber bei vielen Funden würde sie sonst die Seite lähmen).
  const VULN_PAGE_SIZE = PAGE_SIZE;
  let vulnPage = $state(1);
  // Quellen-Filter der CVE-Liste: '' = alle, 'os' = nur Betriebssystem,
  // 'docker' = nur Container-Images. Dieselbe Auswahl wie auf der globalen
  // Sicherheitsseite - wer dort filtert, sucht sie hier auch.
  let vulnSource = $state('');
  let vulnAll = $derived(vulnReport?.vulnerabilities ?? []);
  let vulnList = $derived(
    vulnSource === 'os'
      ? vulnAll.filter((v) => (v.source ?? 'os') !== 'docker')
      : vulnSource === 'docker'
        ? vulnAll.filter((v) => v.source === 'docker')
        : vulnAll,
  );
  // Funde aus ungenutzten Images: angezeigt, aber nicht bewertet.
  let unusedCount = $derived(
    Object.values(vulnReport?.unused_summary ?? {}).reduce((a, b) => a + b, 0),
  );
  let vulnPageCount = $derived(pageCount(vulnList.length, VULN_PAGE_SIZE));
  let vulnPageRows = $derived(pageSlice(vulnList, vulnPage, VULN_PAGE_SIZE));
  // Bei neuem Report/Scan zurück auf Seite 1.
  $effect(() => {
    vulnList;
    vulnPage = 1;
  });

  // CVEs je Paket (OS-Quelle) für Inline-Badges direkt in der Paketliste -
  // der Fund soll am betroffenen Paket sichtbar sein, nicht nur gesammelt
  // im Sicherheit-Tab. Map: Paketname → {count, worst}.
  const SEV_RANK = { critical: 0, high: 1, medium: 2, low: 3, unknown: 4 };
  let pkgVulnMap = $derived.by(() => {
    const m = new Map();
    for (const v of vulnReport?.vulnerabilities ?? []) {
      if (v.source === 'docker') continue; // Image-Funde stehen am Image
      const e = m.get(v.package_name) ?? { count: 0, worst: 'unknown' };
      e.count += 1;
      if ((SEV_RANK[v.severity] ?? 4) < (SEV_RANK[e.worst] ?? 4)) e.worst = v.severity;
      m.set(v.package_name, e);
    }
    return m;
  });

  // Paketnamen-Suche im Pakete-Tab (clientseitig über die geladene Liste).
  let pkgSearch = $state('');
  let filteredPackages = $derived.by(() => {
    const q = pkgSearch.trim().toLowerCase();
    if (!q) return packages;
    return packages.filter((p) => p.name.toLowerCase().includes(q));
  });

  // Sortierung der Paketliste über die Tabellenköpfe. Der häufigste Griff ist
  // „was hat ein Update" - deshalb kippt die Update-Spalte beim ersten Klick
  // gleich auf absteigend, statt die Pakete ohne Update nach oben zu holen.
  const hasPkgUpdate = (p) => !!p.candidate_version && p.candidate_version !== p.version;
  let pkgSort = $state({ key: 'name', dir: 'asc' });

  function togglePkgSort(key) {
    pkgSort =
      pkgSort.key === key
        ? { key, dir: pkgSort.dir === 'asc' ? 'desc' : 'asc' }
        : { key, dir: key === 'name' ? 'asc' : 'desc' };
    pkgPage = 1;
  }

  let sortedPackages = $derived.by(() => {
    const dir = pkgSort.dir === 'asc' ? 1 : -1;
    // Der Name ist überall der Zweitschlüssel: Ohne ihn stünden gleichwertige
    // Zeilen bei jedem Neuzeichnen woanders.
    const byName = (a, b) => a.name.localeCompare(b.name);
    return [...filteredPackages].sort((a, b) => {
      if (pkgSort.key === 'update') {
        return ((hasPkgUpdate(a) ? 1 : 0) - (hasPkgUpdate(b) ? 1 : 0)) * dir || byName(a, b);
      }
      if (pkgSort.key === 'version') {
        return (a.version ?? '').localeCompare(b.version ?? '') * dir || byName(a, b);
      }
      return byName(a, b) * dir;
    });
  });

  // Seitennummern der übrigen langen Tabellen. Bewusst je Tabelle eine
  // eigene: Ein gemeinsamer Zähler spränge beim Reiterwechsel auf eine Seite,
  // die es in der anderen Tabelle gar nicht gibt.
  let snapPage = $state(1);
  let imagePage = $state(1);
  let userPage = $state(1);
  let jobPage = $state(1);
  let appPage = $state(1);
  let unknownAppPage = $state(1);
  let kernelVulnPage = $state(1);

  let pkgPage = $state(1);
  let pagedPackages = $derived(pageSlice(sortedPackages, pkgPage));
  // Ein Filter, der die Liste kürzt, macht die aktuelle Seite oft
  // gegenstandslos - dann gehört man auf Seite 1, nicht auf die letzte.
  $effect(() => {
    pkgSearch;
    pkgPage = 1;
  });

  // CVE-Zähler je Image-Referenz - für Badges an den Container-Zeilen
  // (der Container erbt die Lücken seines Images).
  let imageVulnMap = $derived.by(() => {
    const m = new Map();
    for (const i of docker?.images ?? []) {
      if (i.critical_vulns > 0 || i.high_vulns > 0) {
        m.set(i.tag ? `${i.repository}:${i.tag}` : i.repository, i);
      }
    }
    return m;
  });

  // Von laufenden Docker-Containern VERÖFFENTLICHTE Host-Ports (aus dem
  // Ports-Feld, z.B. "0.0.0.0:8081->80/tcp") - als Freigabe-Vorschläge im
  // Firewall-Popup. Nur durchgereichte Ports zählen; interne wie "5432/tcp"
  // ohne Host-Mapping sind von außen ohnehin nicht erreichbar.
  let dockerPublishedPorts = $derived.by(() => {
    const ports = new Set();
    for (const c of docker?.containers ?? []) {
      if (c.state !== 'running') continue;
      for (const m of (c.ports || '').matchAll(/:(\d+)->/g)) ports.add(Number(m[1]));
    }
    return [...ports].sort((a, b) => a - b);
  });
  // Lauschende Sockets aus dem Scan (JSON) - Vorschläge im Firewall-Dialog.
  let listeningPorts = $derived.by(() => {
    try {
      return JSON.parse(server?.listening_ports || '[]');
    } catch {
      return [];
    }
  });
  // Wirksames Firewall-Backend: erkanntes Werkzeug, sonst das für die
  // Distribution vorgesehene (kommt berechnet vom Server, firewall_backend).
  let firewallBackend = $derived(server?.firewall_tool || server?.firewall_backend || 'ufw');

  // Docker: Container nach Compose-Projekt gruppiert ('' = Standalone).
  let dockerGroups = $derived.by(() => {
    const groups = new Map();
    for (const c of docker?.containers ?? []) {
      const key = c.compose_project || '';
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(c);
    }
    // Compose-Projekte zuerst (alphabetisch), Standalone am Ende.
    return [...groups.entries()].sort((a, b) =>
      (a[0] === '') - (b[0] === '') || a[0].localeCompare(b[0]));
  });
  // Genutzte Images mit verfügbarem Update (Tab-Warn-Badge).
  let dockerUpdates = $derived(
    (docker?.images ?? []).filter((i) => i.update_available && i.in_use).length,
  );

  // Gruppen, in denen der Server noch NICHT ist (Dedup).
  let availableGroups = $derived(
    server ? allGroups.filter((g) => !(server.groups ?? []).some((m) => m.id === g.id)) : [],
  );

  // Reines Nachladen der Ansicht. Räumt bewusst KEINE Meldungen weg: load()
  // läuft am Ende fast jeder Aktion und würde sonst genau deren Ergebnis
  // wieder ausblenden. Aufgeräumt wird nur zu Beginn einer neuen Aktion.
  async function load() {
    try {
      server = await api.servers.get(id);
      status = await api.servers.status(id);
      prefillTimeInputs();
      // Snaps früh laden: entscheidet, ob der Snaps-Tab überhaupt erscheint.
      snaps = await api.servers.snaps(id);
      // Docker-Inventar früh laden (nur bei Docker-Hosts): speist das
      // Tab-Badge (Container-Anzahl, verfügbare Updates).
      docker = server.has_docker ? await api.servers.docker(id) : null;
      if (auth.can('groups:read')) allGroups = await api.groups.list();
      // LCM-Host-Status (Trivy/apt-cacher-ng) nur für den localhost-Server.
      if (isLcmHost) loadLcmHost();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  // Gemeinsame, EHRLICHE Rückmeldung zu einem asynchronen Job: Nach dem POST
  // steht nur fest, DASS er läuft - nicht, dass er geklappt hat. Also erst
  // „gestartet" melden, auf den Abschluss warten und dann das tatsächliche
  // Ergebnis zeigen. Ein fehlgeschlagener Job ist kein Erfolg, und ein nach
  // dem Zeitfenster noch laufender Job ist nicht „abgeschlossen".
  // Rückgabe: der fertige Job, oder null wenn er noch läuft.
  async function trackJob(jobId, label, okText) {
    // Die Start-Meldung bleibt sichtbar, solange gewartet wird (waitForJob
    // wartet bis ~20 s) - deshalb ein langer Timeout und ein gezieltes
    // Ausblenden, sobald das Ergebnis feststeht.
    const startedToast = toasts.info(t('serverDetail.notices.jobStarted', { label, jobId }), { timeout: 120000 });
    const job = await waitForJob(id, jobId);
    toasts.dismiss(startedToast);
    if (job?.status === 'failed') {
      toasts.error(t('serverDetail.notices.jobFailed', { label }));
    } else if (job) {
      toasts.success(okText || t('serverDetail.notices.jobDone', { label }));
    } else {
      toasts.info(t('serverDetail.notices.jobStillRunning', { label }));
    }
    return job;
  }

  // CVE-Scan für diesen Server anstoßen. Der Endpunkt stößt den Scheduler an
  // und liefert KEINE Job-ID - der Abschluss ist deshalb nur am Zeitstempel
  // des Berichts erkennbar. Vorher wurde pauschal 2,5 s gewartet und danach
  // der meist noch alte Bericht angezeigt; jetzt wird gepollt, bis sich
  // last_scan_at ändert (gleiches Muster wie beim Deep Scan).
  async function scanVulnerabilities() {
    vulnScanning = true;
    toasts.clear();
    const before = vulnReport?.last_scan_at ?? null;
    try {
      await api.servers.scanVulnerabilities(id);
      let done = false;
      for (let i = 0; i < 40; i++) {
        await new Promise((r) => setTimeout(r, 2000));
        vulnReport = await api.servers.vulnerabilities(id);
        if (vulnReport?.last_scan_at && vulnReport.last_scan_at !== before) {
          done = true;
          break;
        }
      }
      status = await api.servers.status(id);
      toasts[done ? 'success' : 'info'](
        done ? t('serverDetail.notices.scanDone') : t('serverDetail.notices.scanStillRunning'),
      );
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      vulnScanning = false;
    }
  }

  // Job-basierte Server-Aktion (Refresh): Job starten, auf Abschluss warten,
  // dann die komplette Detailansicht neu laden - so erscheinen die frisch
  // ausgelesenen Werte (Hardware, Docker, Firewall/SSH …) sofort.
  async function refreshServer(fn, label) {
    busy = true;
    toasts.clear();
    try {
      const res = await fn();
      await trackJob(res.job_id, label);
      // Faul geladene Tab-Daten verwerfen, damit sie beim Öffnen neu kommen.
      // Alles, was „Alles aktualisieren" auf der Gegenseite neu schreibt,
      // gehört hierher - sonst zeigt der Tab weiter den Stand von vorhin,
      // und die Aktualisierung sieht wirkungslos aus.
      packages = [];
      outdated = [];
      repos = [];
      storage = null;
      vulnReport = null;
      serverUsers = null;
      docker = server?.has_docker ? await api.servers.docker(id) : null;
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function addToGroup() {
    if (!addGroupId) return;
    await action(() => api.groups.assignServer(Number(addGroupId), id), t('serverDetail.notices.addedToGroup'));
    addGroupId = '';
  }

  async function loadTab(name) {
    tab = name;
    try {
      if (name === 'packages' && packages.length === 0) {
        packages = await api.servers.packages(id);
        outdated = await api.servers.outdatedPackages(id);
        // CVE-Bericht mitladen: speist die Inline-Badges je Paket.
        if (!vulnReport) vulnReport = await api.servers.vulnerabilities(id);
      }
      if (name === 'packages' && !pinLoaded) {
        pinLoaded = true;
        await loadPins();
      } else if (name === 'repos') {
        if (repos.length === 0) repos = await api.servers.repositories(id);
        if (knownRepos.length === 0 && auth.can('servers:write')) {
          knownRepos = await api.servers.knownRepos();
        }
      } else if (name === 'security') {
        if (!vulnReport) vulnReport = await api.servers.vulnerabilities(id);
        prepareSecurityManagement();
        await loadHardenedPaths();
      } else if (name === 'deep-scan') {
        deepScan = await api.servers.deepScanReport(id);
      } else if (name === 'users') {
        if (!serverUsers) serverUsers = await api.servers.serverUsers(id);
        pendingUserSyncs = await api.servers.pendingUserSyncs(id);
        if (lcmUsers.length === 0 && auth.can('linuxusers:read')) {
          lcmUsers = await api.linuxUsers.list();
        }
      } else if (name === 'apps') {
        apps = await api.servers.apps(id);
      } else if (name === 'storage') {
        if (!storage) storage = await api.servers.storageHistory(id);
      } else if (name === 'jobs') {
        jobs = (await api.jobs.history(id, { page_size: 100 })).items;
      } else if (name === 'logs') {
        sessions = await api.servers.sshSessions(id, 200);
      }
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  // Server-Aktion. WICHTIG: Viele „Aktionen" starten in Wahrheit nur einen
  // Job - nach dem POST ist dann noch nichts passiert. Liefert die Antwort
  // eine Job-ID, wird deshalb auf den Abschluss gewartet und das tatsächliche
  // Ergebnis gemeldet; nur echte Sofort-Aktionen melden direkt Erfolg.
  async function action(fn, label) {
    busy = true;
    toasts.clear();
    try {
      const res = await fn();
      if (res?.job_id) {
        await trackJob(res.job_id, label);
      } else {
        toasts.success(t('serverDetail.notices.actionOk', { label }));
      }
      await load();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  // Repo-Aktion: ausführen, Output zeigen, Quellen-Liste neu laden.
  async function repoAction(fn, label) {
    busy = true;
    toasts.clear();
    repoOutput = '';
    try {
      const res = await fn();
      repoOutput = res.output ?? '';
      toasts.success(t('serverDetail.notices.actionOk', { label }));
      repos = await api.servers.repositories(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      busy = false;
    }
  }

  async function secureRepos() {
    await repoAction(() => api.servers.secureRepositories(id), t('serverDetail.actions.httpsSwitch'));
  }

  // Sicherung/Update einer erkannten Anwendung. Läuft als Job - deshalb über
  // action(), das auf den Abschluss wartet und das echte Ergebnis meldet.
  async function runAppAction(app, kind) {
    const frage =
      kind === 'backup'
        ? t('serverDetail.apps.backupConfirm', { name: i18n.field(app, 'name') })
        : app.can_backup
          ? t('serverDetail.apps.updateConfirmWithBackup', { name: i18n.field(app, 'name') })
          : t('serverDetail.apps.updateConfirm', { name: i18n.field(app, 'name') });
    if (!confirm(frage)) return;
    await action(
      () => api.servers.runAppAction(id, app.slug, kind),
      t('serverDetail.apps.jobLabel', { name: i18n.field(app, 'name') })
    );
    apps = await api.servers.apps(id);
  }

  async function revertRepos() {
    if (!confirm(t('serverDetail.repos.revertConfirm', { list: revertUrls.join('\n') }))) return;
    await repoAction(() => api.servers.revertRepositoriesHTTPS(id), t('serverDetail.actions.httpsRevert'));
  }

  async function addRepo() {
    if (!addRepoKey) return;
    const repo = knownRepos.find((r) => r.key === addRepoKey);
    await repoAction(() => api.servers.addRepository(id, addRepoKey), t('serverDetail.actions.setupRepo', { name: repo?.name ?? addRepoKey }));
    addRepoKey = '';
  }

  // APT-Cache-Anbindung umschalten; danach den Server-Status neu laden
  // (apt_proxy_active hat sich geändert). Der Reload läuft mit im
  // Fehler-Handling - sonst gäbe es eine stumme unbehandelte Rejection.
  async function toggleAptProxy(enable) {
    await repoAction(async () => {
      const res = await api.servers.configureAptProxy(id, enable);
      server = await api.servers.get(id);
      return res;
    }, enable ? t('serverDetail.actions.aptProxyConnect') : t('serverDetail.actions.aptProxyDisconnect'));
  }

  // Versionsvergleich in Komponenten (numerisch, wo möglich) - ein reiner
  // String-Vergleich hielte „1.10" fälschlich für kleiner als „1.9".
  function versionLess(a, b) {
    const pa = String(a).split(/[.\-+~:]/);
    const pb = String(b).split(/[.\-+~:]/);
    for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
      const x = pa[i] ?? '';
      const y = pb[i] ?? '';
      if (x === y) continue;
      const nx = parseInt(x, 10);
      const ny = parseInt(y, 10);
      if (!Number.isNaN(nx) && !Number.isNaN(ny) && nx !== ny) return nx < ny;
      return x < y;
    }
    return false;
  }

  // Paketbestand neu laden (nach Update-Jobs).
  async function reloadPackages() {
    packages = await api.servers.packages(id);
    outdated = await api.servers.outdatedPackages(id);
  }

  // Ein Paket-Update-Job starten. Läuft asynchron; wir warten auf den
  // Abschluss und laden dann den Bestand neu.
  async function pkgAction(fn, label) {
    pkgBusy = true;
    toasts.clear();
    try {
      const res = await fn();
      await trackJob(res.job_id, label);
      await reloadPackages();
      status = await api.servers.status(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      pkgBusy = false;
      pkgVersionsFor = null;
    }
  }

  async function refreshPackages() {
    await pkgAction(() => api.servers.refreshPackages(id), t('serverDetail.actions.refreshPackages'));
  }

  async function upgradeAll() {
    await pkgAction(() => api.servers.upgradeAllPackages(id), t('serverDetail.actions.upgradeAll'));
  }

  async function upgradeSecurity() {
    await pkgAction(() => api.servers.upgradeSecurityPackages(id), t('serverDetail.actions.securityUpdates'));
  }

  async function updateOne(name) {
    await pkgAction(() => api.servers.updatePackages(id, { names: [name] }), t('serverDetail.actions.updatePackage', { name }));
  }

  // Nicht mehr benötigte Pakete entfernen (apt autoremove und Pendants).
  async function autoremove() {
    if (!confirm(t('serverDetail.packages.autoremoveConfirm'))) return;
    await pkgAction(() => api.servers.autoremovePackages(id), t('serverDetail.actions.autoremove'));
  }

  // --- Snaps ----------------------------------------------------------------
  // Die zweite Paketverwaltung bekommt dieselben Aktionen wie apt: alle
  // aktualisieren, eines aktualisieren, eines entfernen. Eine Versionsauswahl
  // gibt es bewusst nicht - bei Snaps ist das ein „revert" auf eine Revision
  // und damit etwas anderes als bei apt.
  let snapUpdates = $derived(
    snaps.filter((s) => s.candidate_version && s.candidate_version !== s.version).length,
  );

  // snapd und die core-Basen sind die Grundlage aller übrigen Snaps.
  // Die Basis-Snaps heißen core, core18, core22 … - bewusst mit Zeilenende:
  // „coreutils-snap" ist ein gewöhnliches Snap. Gleiche Liste wie im Server.
  const snapGeschuetzt = (name) =>
    ['snapd', 'bare', 'lxd', 'snap-store'].includes(name) || /^core[0-9]*$/.test(name);

  async function refreshAllSnaps() {
    await pkgAction(() => api.servers.refreshAllSnaps(id), t('serverDetail.snaps.refreshAll'));
    snaps = await api.servers.snaps(id);
  }

  async function refreshSnap(name) {
    await pkgAction(() => api.servers.refreshSnaps(id, [name]), t('serverDetail.snaps.refreshedOne', { name }));
    snaps = await api.servers.snaps(id);
  }

  async function removeSnap(name) {
    if (!confirm(t('serverDetail.snaps.removeConfirm', { name }))) return;
    await pkgAction(() => api.servers.removeSnaps(id, [name]), t('serverDetail.snaps.removedOne', { name }));
    snaps = await api.servers.snaps(id);
  }

  // Alte Kernel entfernen. Was stehen bleibt, entscheidet der Server:
  // laufender Kernel, alles Neuere und eine Rückfallebene.
  async function cleanupKernels() {
    const list = (kernel?.removable ?? []).map((k) => k.release).join(', ');
    if (!confirm(t('serverDetail.kernel.cleanupConfirm', { list }))) return;
    await pkgAction(() => api.servers.removeOldKernels(id), t('serverDetail.kernel.cleanup'));
  }

  // --- Paket-Pins ------------------------------------------------------------
  // Ein Pin schützt ein Paket vor dem Aufräumen (Autoremove) und friert
  // optional seine Version ein. Der Hauptzweck ist der Kernel: Ohne Pin räumt
  // `apt autoremove` ältere Kernel weg - die Rückfallebene, wenn ein neuer
  // Kernel nicht bootet (Vorbild Proxmox).
  //
  // Pins gibt es global (alle Server) und je Server; auf dem Ziel wird die
  // Vereinigung beider Mengen angewendet.
  let pins = $state(null); // {global, server, available, reason, kernel_preset}
  let pinBusy = $state(false);
  let pinLoaded = false;
  let newPin = $state({ name: '', no_remove: true, hold: false, note: '', global: false });
  // Namen aller wirksamen Pins - speist das Schild-Symbol in der Paketliste.
  let pinnedNames = $derived([...(pins?.global ?? []), ...(pins?.server ?? [])]);
  function pinFor(name) {
    const n = (name || '').toLowerCase();
    return pinnedNames.find((p) =>
      p.name.endsWith('*') ? n.startsWith(p.name.slice(0, -1)) : n === p.name,
    );
  }

  async function loadPins() {
    try {
      pins = await api.servers.packagePins(id);
    } catch (e) {
      pins = null;
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  // Nach jeder Pin-Änderung wird die Liste neu geladen. Die Änderung wirkt auf
  // dem Ziel aber erst mit „Auf dem Server anwenden" (bzw. beim nächsten
  // Aufräum-Lauf, der die Pins selbst mitschreibt) - das sagt die Karte auch.
  async function pinAction(fn) {
    if (pinBusy) return false;
    pinBusy = true;
    toasts.clear();
    try {
      await fn();
      await loadPins();
      return true;
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      return false;
    } finally {
      pinBusy = false;
    }
  }

  async function addPin() {
    if (!newPin.name.trim()) return;
    const ok = await pinAction(() => api.servers.createPackagePin(id, { ...newPin, name: newPin.name.trim() }));
    if (ok) {
      toasts.success(t('serverDetail.pins.added', { name: newPin.name.trim() }));
      newPin = { name: '', no_remove: true, hold: false, note: '', global: false };
    }
  }

  async function removePin(pin) {
    if (!confirm(t('serverDetail.pins.removeConfirm', { name: pin.name }))) return;
    if (await pinAction(() => api.servers.deletePackagePin(id, pin.id))) {
      toasts.success(t('serverDetail.pins.removed', { name: pin.name }));
    }
  }

  // Ein-Klick: ein Paket aus der Liste vor dem Entfernen schützen.
  async function pinPackage(name) {
    if (await pinAction(() => api.servers.createPackagePin(id, { name, no_remove: true, hold: false }))) {
      toasts.success(t('serverDetail.pins.added', { name }));
    }
  }

  async function pinKernel(global) {
    if (await pinAction(() => api.servers.pinKernelPreset(id, global))) {
      toasts.success(t('serverDetail.pins.kernelAdded'));
    }
  }

  // Pins auf dem Server festschreiben (Schutzdateien + Holds) - Job.
  async function applyPins() {
    pinBusy = true;
    toasts.clear();
    try {
      const res = await api.servers.applyPackagePins(id);
      await trackJob(res.job_id, t('serverDetail.pins.applyLabel'));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      pinBusy = false;
    }
  }

  // Ein einzelnes Paket gezielt entfernen (mit Rückfrage - irreversibel).
  async function removeOne(name) {
    if (!confirm(t('serverDetail.packages.removeConfirm', { name }))) return;
    await pkgAction(() => api.servers.removePackages(id, [name]), t('serverDetail.actions.removePackage', { name }));
  }

  // Docker-Aktion starten (Job) und danach Inventar + Ampel neu laden.
  async function dockerAction(fn, label) {
    dockerBusy = true;
    toasts.clear();
    try {
      const res = await fn();
      await trackJob(res.job_id, label);
      docker = await api.servers.docker(id);
      status = await api.servers.status(id);
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      dockerBusy = false;
    }
  }

  async function refreshDocker() {
    await dockerAction(() => api.servers.refreshDocker(id), t('serverDetail.actions.refreshDocker'));
  }

  async function composeUpdate(project, service = '') {
    await dockerAction(
      () => api.servers.composeUpdate(id, project, service),
      t('serverDetail.actions.composeUpdate', { target: `${project}${service ? '/' + service : ''}` }),
    );
  }

  async function dockerPull(image) {
    await dockerAction(() => api.servers.dockerPull(id, image), t('serverDetail.actions.pullImage', { image }));
    toasts.info(t('serverDetail.notices.pullHint'));
  }

  async function dockerPullAll() {
    await dockerAction(() => api.servers.dockerPullAll(id), t('serverDetail.actions.pullAllImages'));
    toasts.info(t('serverDetail.notices.pullHint'));
  }

  async function dockerRemoveImage(image) {
    await dockerAction(() => api.servers.dockerRemoveImage(id, image), t('serverDetail.actions.removeImage', { image }));
  }

  async function dockerPrune() {
    if (!confirm(t('serverDetail.confirms.dockerPrune'))) return;
    await dockerAction(() => api.servers.dockerPrune(id), t('serverDetail.actions.dockerPrune'));
  }

  // Anzeige-Helfer: Digest kompakt ("sha256:ab12cd…").
  function shortDigest(d) {
    if (!d) return '-';
    const hex = d.startsWith('sha256:') ? d.slice(7) : d;
    return `sha256:${hex.slice(0, 12)}…`;
  }
  let CHECK_LABELS = $derived({
    ok: null, // ok braucht kein Badge - Update-Spalte sagt alles
    unauthorized: { text: t('serverDetail.checkLabels.unauthorized'), cls: 'text-bg-secondary' },
    not_found: { text: t('serverDetail.checkLabels.notFound'), cls: 'text-bg-secondary' },
    local: { text: t('serverDetail.checkLabels.local'), cls: 'border text-body-secondary' },
    error: { text: t('serverDetail.checkLabels.error'), cls: 'text-bg-danger' },
  });

  // Versions-Auswahl für ein Paket öffnen und die verfügbaren Versionen laden.
  async function openVersions(name) {
    if (pkgVersionsFor === name) {
      pkgVersionsFor = null;
      return;
    }
    pkgVersionsFor = name;
    pkgVersions = [];
    pkgSelectedVersion = '';
    try {
      pkgVersions = (await api.servers.packageVersions(id, name)) ?? [];
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function installVersion(name) {
    if (!pkgSelectedVersion) return;
    await pkgAction(
      () => api.servers.updatePackages(id, { names: [name], version: pkgSelectedVersion }),
      `${name} → ${pkgSelectedVersion}`,
    );
  }

  // Neustart: erst bestätigen (disruptiv - der Server ist danach kurz
  // nicht erreichbar), dann als Job auslösen und auf Abschluss warten.
  async function reboot() {
    if (!confirm(t('serverDetail.confirms.reboot'))) return;
    await refreshServer(() => api.servers.reboot(id), t('serverDetail.actions.reboot'));
  }

  // Rechte einschränken: aus dem Bestätigungs-Modal ausgelöst (Einweg-Aktion).
  async function restrictSudo() {
    await action(() => api.servers.restrictSudo(id), t('serverDetail.actions.restrictSudo'));
    restrictOpen = false;
  }

  function openFirewall() {
    // Regel-Konfiguration laden: JSON (maßgeblich), sonst Legacy-CSV → TCP-Regeln.
    try {
      firewallRules = JSON.parse(server.firewall_rules || '[]');
    } catch {
      firewallRules = [];
    }
    if (firewallRules.length === 0 && server.firewall_allowed_ports) {
      firewallRules = server.firewall_allowed_ports
        .split(',')
        .map((s) => Number(s.trim()))
        .filter((p) => Number.isInteger(p) && p > 0)
        .map((p) => ({ port: p, proto: 'tcp', ip_version: 'any', allowlist_ids: [], source_ips: [], comment: '' }));
    }
    try {
      const src = JSON.parse(server.firewall_ssh_sources || '{}');
      firewallSSHSources = { allowlist_ids: src.allowlist_ids ?? [], source_ips: src.source_ips ?? [] };
    } catch {
      firewallSSHSources = { allowlist_ids: [], source_ips: [] };
    }
    // Verfügbare Allowlists für die Quell-Auswahl laden (best effort).
    api.servers.ipAllowlists().then((l) => (ipAllowlists = l)).catch(() => (ipAllowlists = []));
    firewallOpen = true;
  }

  // Lauschende Dienste sofort vom Server holen. Ohne das zeigte der Dialog
  // nur, was der letzte Server-Scan zufällig erfasst hatte - ein seither
  // installierter Dienst tauchte in den Vorschlägen nie auf.
  async function rescanListeningPorts() {
    listeningScanBusy = true;
    toasts.clear();
    try {
      const res = await api.servers.scanListeningPorts(id);
      listeningPorts = res.listening_ports ?? [];
      toasts.success(t('serverDetail.firewall.portsScanned', { count: listeningPorts.length }));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      listeningScanBusy = false;
    }
  }

  async function applyFirewall(enable) {
    if (enable && firewallEditor?.invalidRows()) return;
    // Leere Felder normalisieren, Ports als Zahlen senden.
    const rules = firewallRules.map((r) => ({
      port: Number(r.port),
      proto: r.proto || 'tcp',
      ip_version: r.ip_version || 'any',
      allowlist_ids: (r.allowlist_ids ?? []).length ? r.allowlist_ids : undefined,
      source_ips: (r.source_ips ?? []).length ? r.source_ips : undefined,
      comment: r.comment?.trim() || undefined,
    }));
    const sshSources = {
      allowlist_ids: firewallSSHSources.allowlist_ids ?? [],
      source_ips: firewallSSHSources.source_ips ?? [],
    };
    firewallOpen = false;
    // Asynchroner Job (je nach Distribution wird das Werkzeug erst
    // installiert) - starten, auf Abschluss warten, neu laden.
    await refreshServer(
      () => api.servers.configureFirewall(id, enable, enable ? rules : [], enable ? sshSources : null),
      enable ? t('serverDetail.actions.firewallEnabled') : t('serverDetail.actions.firewallDisabled'),
    );
  }

  function openRemove() {
    // Bereinigung nur sinnvoll, wenn erreichbar; auf Agent-Servern gibt es
    // keine LCM-Spuren (kein Service-User) - dort per `lcm-agent uninstall`.
    removePurge = server.reachable && server.transport !== 'agent';
    removeOpen = true;
  }

  async function confirmRemove() {
    busy = true;
    toasts.clear();
    try {
      await api.servers.decommission(id, removePurge);
      removeOpen = false;
      push('/');
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
      busy = false;
    }
  }

  $effect(() => {
    if (!id) return;
    // Serverwechsel (svelte-spa-router recycelt die Komponente): Tab und
    // die faul nachgeladenen Bestände zurücksetzen, damit nicht die Daten
    // des vorigen Servers stehen bleiben.
    tab = 'overview';
    packages = [];
    outdated = [];
    repos = [];
    jobs = [];
    sessions = [];
    vulnReport = null;
    storage = null;
    docker = null;
    serverUsers = null;
    assignUserId = '';
    activeJob = null;
    pins = null;
    pinLoaded = false;
    secManagePrepared = false;
    secBans = {};
    secBansError = {};
    load();
    // Job-Sperre live halten: sofort und dann alle 5 s prüfen.
    pollActiveJob();
    const iv = setInterval(() => {
      nowTick = Date.now();
      pollActiveJob();
    }, 5000);
    return () => clearInterval(iv);
  });
</script>

<div class="container">
  {#if server}
    <div class="d-flex justify-content-between align-items-start mb-3">
      <div class="d-flex align-items-center gap-3">
        <OsLogo os={server.os_name} proxmox={server.proxmox_type} host={server.host} port={server.ssh_port} size={40} />
        <div>
          <h1 class="h3 mb-1">
            {server.name}
            <!-- Offline: nicht erreichbar UND mindestens zwei fehlgeschlagene
                 Kontakte in Folge. Gilt für jeden Server - ob die
                 Nichterreichbarkeit toleriert wird, steuert nur die
                 Ampelfarbe, nicht diese Tatsache. -->
            {#if isOffline}
              <span class="badge text-bg-secondary align-middle ms-1" data-testid="offline-badge"
                title={t('serverDetail.offline.title', { count: server.failed_checks })}>
                {t('serverDetail.offline.badge')}
              </span>
            {/if}
            {#if isProxmox}
              <span class="badge align-middle ms-1" style="background: #E57000; color: #fff" data-testid="proxmox-badge">
                {proxmoxName} {server.proxmox_version}
              </span>
            {/if}
            {#if restricted}
              <span class="badge text-bg-secondary align-middle ms-1" title={restrictedHint} data-testid="restricted-badge">
                {@html icons.lock} {t('serverDetail.restricted.badge')}
              </span>
            {/if}
            {#if isAgent}
              <span class="badge text-bg-info align-middle ms-1" title={agentHint} data-testid="agent-badge">
                {@html icons.link} {t('serverDetail.agent.badge')}
              </span>
              <span class="badge align-middle ms-1 {server.agent_connected ? 'text-bg-success' : 'text-bg-danger'}" data-testid="agent-conn-badge">
                {server.agent_connected ? t('serverDetail.agent.online') : t('serverDetail.agent.offline')}
              </span>
            {/if}
          </h1>
          <div class="text-body-secondary">
            {#if isRouterOS}
              {server.os_name} {server.os_version}{#if server.routeros_channel} ({server.routeros_channel}){/if}
              {#if server.routerboard_model} · {server.routerboard_model}{/if} · {server.host}:{server.ssh_port}
            {:else if isDSM}
              {server.os_version}{#if server.dsm_model} · {server.dsm_model}{/if} · {server.host}:{server.dsm_port || server.ssh_port} · {t('serverDetail.dsm.transport')}
            {:else if isAgent}
              {server.os_name} {server.os_version} · {t('serverDetail.agent.transport')}{server.agent_version ? ` · lcm-agent ${server.agent_version}` : ''}
            {:else}
              {server.os_name} {server.os_version} · {server.host}:{server.ssh_port} · {server.service_user}
            {/if}
          </div>
          {#if isRouterOS}
            {#if server.routeros_update_available}
              <div class="small"><span class="badge text-bg-warning">{t('serverDetail.routeros.updateBadge', { version: server.routeros_latest_version })}</span></div>
            {:else}
              <div class="small"><span class="badge text-bg-success">{t('serverDetail.routeros.currentBadge')}</span></div>
            {/if}
          {/if}
          {#if isDSM}
            <div class="small d-flex flex-wrap gap-1" data-testid="dsm-badges">
              {#if server.dsm_update_available}
                <span class="badge text-bg-warning">{t('serverDetail.dsm.updateBadge', { version: server.dsm_latest_version || '' })}</span>
              {:else}
                <span class="badge text-bg-success">{t('serverDetail.dsm.currentBadge')}</span>
              {/if}
              {#if server.dsm_security_risks > 0}
                <span class="badge text-bg-warning" title={server.dsm_security_summary}>
                  {t('serverDetail.dsm.advisorRisks', { count: server.dsm_security_risks })}
                </span>
              {:else}
                <span class="badge text-bg-success">{t('serverDetail.dsm.advisorClean')}</span>
              {/if}
            </div>
          {/if}
          <div class="small text-body-secondary">{t('serverDetail.header.lastConnected')}: {lastSeen(server.last_seen_at)}</div>
        </div>
      </div>
      <div class="d-flex align-items-center gap-2">
        <!-- Offline deutlich machen: große Pille direkt neben dem Status. -->
        {#if server.reachable === false}
          <span class="badge rounded-pill text-bg-secondary fs-6" data-testid="offline-pill"
            title={server.last_error || t('serverDetail.header.offlineTitle')}>
            {@html icons.warning} {t('serverDetail.header.offline')}
          </span>
        {/if}
        {#if status}<StatusBadge status={status.status} insights={status.insights} />{/if}
      </div>
    </div>

    {#if activeJob}
      <!-- Job-Sperre sichtbar machen: laufender Job + Laufzeit + Abbruch. -->
      <div class="alert alert-warning d-flex flex-wrap align-items-center gap-2 py-2" data-testid="active-job-banner">
        <span>{@html icons.clock} {t('serverDetail.activeJob.running', { name: activeJob.name, minutes: activeJobMinutes })}</span>
        {#if auth.can('servers:write')}
          <button class="btn btn-sm btn-outline-danger ms-auto" disabled={aborting} data-testid="active-job-abort" onclick={abortActiveJob}>
            {aborting ? t('serverDetail.activeJob.aborting') : t('serverDetail.activeJob.abort')}
          </button>
        {/if}
      </div>
    {/if}

    {#if auth.can('servers:write')}
      <div class="d-flex flex-wrap gap-2 mb-4">
        <div class="btn-group btn-group-sm flex-wrap" role="group">
          <button class="btn btn-outline-secondary" disabled={busy || jobLocked} title={t('serverDetail.actions.refreshHardwareTitle')} onclick={() => refreshServer(() => api.servers.refreshHardware(id), t('serverDetail.actions.refreshHardware'))}>{@html icons.refresh} {t('serverDetail.actions.refreshHardware')}</button>
          <button class="btn btn-outline-secondary" disabled={busy || jobLocked} title={t('serverDetail.actions.refreshAllTitle')} onclick={() => refreshServer(() => api.servers.refreshAll(id), t('serverDetail.actions.refreshAll'))}>{@html icons.refresh} {t('serverDetail.actions.refreshAll')}</button>
          {#if !isAgent && !isAPIDevice}
            {#if server.ssh_hardened}
              <button class="btn btn-outline-primary" disabled={busy || jobLocked} title={t('serverDetail.actions.sshHardenedTitle')} onclick={() => action(() => api.servers.unhardenSsh(id), t('serverDetail.actions.sshUnhardened'))}>
                {t('serverDetail.actions.sshHardenedBtn')}
              </button>
            {:else}
              <button class="btn btn-outline-primary" disabled={busy || jobLocked} onclick={() => action(() => api.servers.hardenSsh(id), t('serverDetail.actions.sshHarden'))}>
                {t('serverDetail.actions.sshHardenBtn')}
              </button>
            {/if}
          {/if}
          {#if !isAPIDevice}
            <button class="btn btn-outline-primary" disabled={busy || jobLocked || isProxmox}
              title={isProxmox ? proxmoxHint + ' (pve-firewall)' : undefined} onclick={openFirewall}>
              {t('serverDetail.actions.firewall')} {#if isProxmox}{@html icons.lock}{:else if server.firewall_active}✓{/if}
            </button>
          {/if}
          <button class="btn btn-outline-secondary" disabled={busy || jobLocked} title={t('serverDetail.actions.settings')} aria-label={t('serverDetail.actions.settings')} data-testid="open-settings" onclick={() => (settingsOpen = true)}>{@html icons.gear}</button>
        </div>

        <!-- Aktionen-Dropdown: seltener genutzte/disruptive Aktionen gebündelt. -->
        <div class="dropdown" bind:this={actionsMenuEl}>
          <button
            class="btn btn-outline-secondary btn-sm dropdown-toggle"
            disabled={busy || jobLocked}
            aria-haspopup="menu"
            aria-expanded={actionsMenu}
            data-testid="server-actions-toggle"
            onclick={() => (actionsMenu = !actionsMenu)}
          >{t('serverDetail.actions.menu')}</button>
          <div class="dropdown-menu {actionsMenu ? 'show' : ''}" style="min-width: 240px">
            {#if !isAPIDevice}
            <button class="dropdown-item d-flex align-items-center gap-2" disabled={isProxmox || userSyncDisabled}
              title={isProxmox ? proxmoxHint : userSyncDisabled ? userSyncHint : undefined}
              onclick={() => runMenuAction(() => action(() => api.servers.syncUsers(id), t('serverDetail.actions.userSync')))}>
              {@html icons.users} {t('serverDetail.actions.syncUsers')}{#if isProxmox || userSyncDisabled} {@html icons.lock}{/if}
            </button>
            {#if isAgent}
              <!-- Agent-Server: kein SSH-Zertifikat/Reconnect, stattdessen Token neu erzeugen. -->
              <button class="dropdown-item d-flex align-items-center gap-2"
                data-testid="agent-token-regenerate"
                onclick={() => runMenuAction(regenerateAgentToken)}>
                {@html icons.key} {t('serverDetail.agent.regenerate')}
              </button>
            {:else}
              <button class="dropdown-item d-flex align-items-center gap-2"
                onclick={() => runMenuAction(() => action(() => api.servers.rotateKey(id), t('serverDetail.actions.keyRotation')))}>
                {@html icons.key} {t('serverDetail.actions.rotateCert')}
              </button>
              <button class="dropdown-item d-flex align-items-center gap-2" onclick={() => runMenuAction(() => (reconnectOpen = true))}>
                {@html icons.link} {t('serverDetail.actions.reconnect')}
              </button>
            {/if}
            <button class="dropdown-item d-flex align-items-center gap-2"
              data-testid="server-action-dns-test"
              onclick={() => runMenuAction(runDnsTest)}>
              {@html icons.search} {t('serverDetail.dns.testLabel')}
            </button>
            <button class="dropdown-item d-flex align-items-center gap-2" disabled={restricted}
              title={restricted ? restrictedHint : undefined}
              data-testid="server-action-security-tools"
              onclick={() => runMenuAction(openSecurityTool)}>
              {@html icons.shield} {t('serverDetail.securityTool.menu')}{#if restricted} {@html icons.lock}{/if}
            </button>
            <button class="dropdown-item d-flex align-items-center gap-2" disabled={restricted}
              title={restricted ? restrictedHint : undefined}
              data-testid="server-action-reboot"
              onclick={() => runMenuAction(reboot)}>
              {@html icons.power} {t('serverDetail.actions.reboot')}{#if restricted} {@html icons.lock}{/if}
            </button>
            {#if !restricted}
              <button class="dropdown-item d-flex align-items-center gap-2 text-warning-emphasis"
                data-testid="server-action-restrict"
                onclick={() => runMenuAction(() => (restrictOpen = true))}>
                {@html icons.lock} {t('serverDetail.actions.restrictSudo')}
              </button>
            {:else}
              <button class="dropdown-item d-flex align-items-center gap-2"
                data-testid="server-action-unrestrict-guide"
                onclick={() => runMenuAction(() => (unrestrictGuideOpen = true))}>
                {@html icons.key} {t('serverDetail.actions.unrestrictGuide')}
              </button>
            {/if}
            {/if}
            <hr class="dropdown-divider" />
            <button class="dropdown-item d-flex align-items-center gap-2 text-danger" onclick={() => runMenuAction(openRemove)}>
              {@html icons.trash} {t('serverDetail.actions.remove')}
            </button>
          </div>
        </div>
      </div>
    {/if}

    {#if agentToken}
      <!-- Neu erzeugtes Agent-Token: einmalige Anzeige, danach nur der Hash. -->
      <div class="alert alert-warning" data-testid="agent-token-alert">
        <div class="mb-2"><strong>{t('serverDetail.agent.newTokenBold')}</strong>{t('serverDetail.agent.newTokenText')}</div>
        <div class="input-group input-group-sm">
          <input class="form-control font-monospace" readonly value={agentToken} />
          <button class="btn btn-outline-secondary" type="button" onclick={copyAgentToken}>
            {agentTokenCopied ? t('serverDetail.agent.copied') : t('common.copy')}
          </button>
          <button class="btn btn-outline-secondary" type="button" onclick={() => (agentToken = '')}>{t('modal.close')}</button>
        </div>
        <div class="form-text">{t('serverDetail.agent.newTokenHint')}</div>
      </div>
    {/if}

    <ul class="nav nav-tabs mb-3">
      <li class="nav-item"><button class="nav-link {tab === 'overview' ? 'active' : ''}" onclick={() => loadTab('overview')}>{t('serverDetail.tabs.overview')}</button></li>
      <!-- Reine API-Geräte: Paketquellen, CVE-Sicht und Deep Scan setzen eine
           Shell/Paketverwaltung voraus - leere Reiter anzubieten hieße, eine
           Funktion zu versprechen, die es dort nicht gibt. -->
      {#if !isAPIDevice}
        <li class="nav-item"><button class="nav-link {tab === 'repos' ? 'active' : ''}" onclick={() => loadTab('repos')}>{t('serverDetail.tabs.repos')}</button></li>
      {/if}
      <li class="nav-item"><button class="nav-link {tab === 'packages' ? 'active' : ''}" onclick={() => loadTab('packages')}>{t('serverDetail.tabs.packages')}</button></li>
      {#if server.has_snap || snaps.length > 0}
        <li class="nav-item"><button class="nav-link {tab === 'snaps' ? 'active' : ''}" onclick={() => loadTab('snaps')}>{t('serverDetail.tabs.snaps')} <span class="badge border text-body-secondary ms-1">{snaps.length}</span></button></li>
      {/if}
      {#if server.has_docker || (docker?.containers?.length ?? 0) > 0}
        <li class="nav-item"><button class="nav-link {tab === 'docker' ? 'active' : ''}" onclick={() => loadTab('docker')}>
          {t('serverDetail.tabs.docker')} <span class="badge border text-body-secondary ms-1">{docker?.containers?.length ?? 0}</span>
          {#if dockerUpdates > 0}<span class="badge bg-warning text-dark ms-1">{dockerUpdates === 1 ? t('serverDetail.tabs.updateOne', { count: dockerUpdates }) : t('serverDetail.tabs.updateMany', { count: dockerUpdates })}</span>{/if}
        </button></li>
      {/if}
      <li class="nav-item"><button class="nav-link {tab === 'apps' ? 'active' : ''}" data-testid="tab-apps" onclick={() => loadTab('apps')}>
        {t('serverDetail.tabs.apps')}
        {#if appUpdates > 0}<span class="badge bg-warning text-dark ms-1">{appUpdates === 1 ? t('serverDetail.tabs.updateOne', { count: appUpdates }) : t('serverDetail.tabs.updateMany', { count: appUpdates })}</span>{/if}
      </button></li>
      {#if !isAPIDevice}
        <li class="nav-item"><button class="nav-link {tab === 'users' ? 'active' : ''}" data-testid="tab-users" onclick={() => loadTab('users')}>{t('serverDetail.tabs.users')}</button></li>
        <li class="nav-item"><button class="nav-link {tab === 'security' ? 'active' : ''}" onclick={() => loadTab('security')}>
          {t('serverDetail.tabs.security')}
          {#if vulnSevereCount > 0}<span class="badge bg-danger ms-1">{vulnSevereCount}</span>{/if}
        </button></li>
        <li class="nav-item"><button class="nav-link {tab === 'deep-scan' ? 'active' : ''}" data-testid="tab-deep-scan" onclick={() => loadTab('deep-scan')}>
          {t('serverDetail.tabs.deepScan')}
          {#if server.deep_scan_warnings > 0}<span class="badge bg-warning text-dark ms-1">{server.deep_scan_warnings}</span>{/if}
        </button></li>
      {/if}
      <li class="nav-item"><button class="nav-link {tab === 'storage' ? 'active' : ''}" onclick={() => loadTab('storage')}>{t('serverDetail.tabs.storage')}</button></li>
      <li class="nav-item"><button class="nav-link {tab === 'jobs' ? 'active' : ''}" onclick={() => loadTab('jobs')}>{t('serverDetail.tabs.jobs')}</button></li>
      <li class="nav-item"><button class="nav-link {tab === 'logs' ? 'active' : ''}" onclick={() => loadTab('logs')}>{t('serverDetail.tabs.logs')}</button></li>
    </ul>

    {#if tab === 'overview'}
      {#if isLcmHost}
        <div class="card border-primary mb-3" data-testid="lcm-host-card">
          <div class="card-body">
            <h2 class="h6 d-flex align-items-center gap-2">{@html icons.shield} {t('serverDetail.lcmHost.title')}</h2>
            <p class="small text-body-secondary mb-3">{t('serverDetail.lcmHost.intro')}</p>
            {#if lcmHost?.in_container}
              <!-- Im Container richtet sich hier nichts ein: Es gibt kein apt
                   und keinen Dienst, der den Neustart überlebt. Erklären statt
                   Schaltflächen anbieten, die scheitern müssen. -->
              <div class="alert alert-info py-2 small mb-0" data-testid="lcm-host-container">
                {t('serverDetail.lcmHost.inContainer')}
              </div>
            {:else if lcmHost && lcmHost.package_manager !== 'apt'}
              <div class="alert alert-warning py-2 small mb-0">{t('serverDetail.lcmHost.notApt')}</div>
            {:else}
              <div class="row g-3">
                <!-- Trivy -->
                <div class="col-md-6">
                  <div class="d-flex align-items-center justify-content-between gap-2">
                    <div>
                      <div class="fw-semibold">Trivy <span class="text-body-secondary small">({t('serverDetail.lcmHost.trivyWhat')})</span></div>
                      {#if lcmHost?.trivy_installed}
                        <span class="badge text-bg-success" data-testid="trivy-status">{t('serverDetail.lcmHost.installed')}</span>
                        <!-- Version und Datenbank-Stand gehören direkt hierher:
                             Ein installiertes Trivy sagt für sich genommen noch
                             nichts darüber, ob seine Daten aktuell sind. -->
                        {#if scannerInfo?.available}
                          <div class="small text-body-secondary mt-1" data-testid="trivy-db-info">
                            Trivy {scannerInfo.version}
                            {#if scannerInfo.updated_at}
                              · {t('serverDetail.lcmHost.dbFrom', { when: lastSeen(scannerInfo.updated_at) })}
                              {#if scannerInfo.freshness !== 'fresh'}
                                <span class="badge {scannerInfo.freshness === 'critical' ? 'text-bg-danger' : 'text-bg-warning'} ms-1"
                                  data-testid="trivy-db-stale">{t('serverDetail.lcmHost.dbStale')}</span>
                              {/if}
                            {:else}
                              · <span class="badge text-bg-danger" data-testid="trivy-db-missing">{t('serverDetail.lcmHost.dbNever')}</span>
                            {/if}
                          </div>
                          <!-- Abschottung samt Nachrüst-Schaltfläche. Der
                               Zustand selbst steht auch auf der
                               Sicherheits-Seite (SandboxBadge) - die
                               Schaltfläche gehört hierher, weil sie ein Paket
                               auf dem LCM-Host installiert. -->
                          <div class="mt-1">
                            <SandboxBadge info={scannerInfo}>
                              {#snippet actions()}
                                {#if lcmHost?.sandbox_retrofit && auth.can('servers:write')}
                                  <button class="btn btn-sm btn-outline-primary mt-2" data-testid="install-sandbox"
                                    disabled={busy || jobLocked} onclick={installSandbox}>{t('serverDetail.lcmHost.sandboxSetup')}</button>
                                {/if}
                              {/snippet}
                            </SandboxBadge>
                          </div>
                        {/if}
                      {:else}
                        <span class="badge text-bg-secondary" data-testid="trivy-status">{t('serverDetail.lcmHost.notInstalled')}</span>
                      {/if}
                    </div>
                    {#if !lcmHost?.trivy_installed && auth.can('servers:write')}
                      <button class="btn btn-sm btn-primary" data-testid="install-trivy"
                        disabled={busy || jobLocked} onclick={installTrivy}>{t('serverDetail.lcmHost.setup')}</button>
                    {/if}
                  </div>
                </div>
                <!-- apt-cacher-ng -->
                <div class="col-md-6">
                  <div class="d-flex align-items-center justify-content-between gap-2">
                    <div>
                      <div class="fw-semibold">apt-cacher-ng <span class="text-body-secondary small">({t('serverDetail.lcmHost.aptCacherWhat')})</span></div>
                      {#if lcmHost?.apt_cacher_installed}
                        <span class="badge text-bg-success" data-testid="apt-cacher-status">{t('serverDetail.lcmHost.installed')}</span>
                      {:else}
                        <span class="badge text-bg-secondary" data-testid="apt-cacher-status">{t('serverDetail.lcmHost.notInstalled')}</span>
                      {/if}
                    </div>
                    {#if !lcmHost?.apt_cacher_installed && auth.can('servers:write')}
                      <button class="btn btn-sm btn-primary" data-testid="install-apt-cacher"
                        disabled={busy || jobLocked} onclick={installAptCacher}>{t('serverDetail.lcmHost.setup')}</button>
                    {/if}
                  </div>
                </div>
                <!-- CrowdSec LAPI -->
                <div class="col-12">
                  <hr class="my-1" />
                  <div class="d-flex align-items-center justify-content-between gap-2 flex-wrap">
                    <div>
                      <div class="fw-semibold">CrowdSec LAPI <span class="text-body-secondary small">({t('serverDetail.lcmHost.lapiWhat')})</span></div>
                      {#if lcmHost?.crowdsec_lapi_installed}
                        <span class="badge text-bg-success" data-testid="crowdsec-lapi-status">{t('serverDetail.lcmHost.installed')}</span>
                      {:else}
                        <span class="badge text-bg-secondary" data-testid="crowdsec-lapi-status">{t('serverDetail.lcmHost.notInstalled')}</span>
                      {/if}
                    </div>
                    {#if !lcmHost?.crowdsec_lapi_installed && auth.can('servers:write')}
                      <div class="d-flex align-items-center gap-3">
                        <label class="form-check form-switch small mb-0">
                          <input class="form-check-input" type="checkbox" role="switch" bind:checked={lapiBouncer} />
                          {t('serverDetail.lcmHost.lapiBouncer')}
                        </label>
                        <button class="btn btn-sm btn-primary" data-testid="install-crowdsec-lapi"
                          disabled={busy || jobLocked} onclick={installCrowdSecLapi}>{t('serverDetail.lcmHost.setup')}</button>
                      </div>
                    {/if}
                  </div>
                  <p class="small text-body-secondary mb-0 mt-1">{t('serverDetail.lcmHost.lapiHint')}</p>
                </div>
              </div>
            {/if}
          </div>
        </div>
      {/if}
      <div class="row g-3">
        <div class="col-md-6">
          <div class="card"><div class="card-body">
            <h2 class="h6">{t('serverDetail.overview.hardware')}</h2>
            <dl class="row mb-0 small">
              <!-- Reine API-Geräte (RouterOS, DSM): Plattform/Paketverwaltung/
                   DNS erhebt LCM dort nicht - leere Zeilen mit „unbekannt"
                   behaupteten eine Lücke, wo schlicht keine Erhebung
                   vorgesehen ist. -->
              {#if !isAPIDevice}
                <dt class="col-5">{t('serverDetail.overview.platform')}</dt>
                <dd class="col-7">{@html virtInfo(server.virtualization).icon} {virtInfo(server.virtualization).label}</dd>
              {/if}
              {#if isDSM && server.dsm_model}
                <dt class="col-5">{t('serverDetail.dsm.model')}</dt><dd class="col-7">{server.dsm_model}</dd>
              {/if}
              <dt class="col-5">{t('serverDetail.overview.cpu')}</dt><dd class="col-7">{server.cpu_model || '-'} ({t('serverDetail.overview.cores', { count: server.cpu_cores })})</dd>
              <dt class="col-5">{t('serverDetail.overview.ram')}</dt><dd class="col-7">{fmtSize(server.mem_total_mb)}</dd>
              <dt class="col-5">{t('serverDetail.overview.disk')}</dt><dd class="col-7">{fmtSize(server.disk_used_mb)} / {fmtSize(server.disk_total_mb)}</dd>
              {#if !isAPIDevice}
                <dt class="col-5">{t('serverDetail.overview.packageManager')}</dt>
                <dd class="col-7">
                  <span class="badge border text-body-secondary">{pkgMgrLabel(server.package_manager)}</span>
                  {#if server.has_snap || snaps.length > 0}<span class="badge border text-body-secondary ms-1">Snap ({snaps.length})</span>{/if}
                </dd>
                <dt class="col-5">{t('serverDetail.overview.ipAddresses')}</dt><dd class="col-7">{server.ip_addresses || '-'}</dd>
                <dt class="col-5">{t('serverDetail.dns.currentLabel')}</dt><dd class="col-7">{server.dns_current || t('serverDetail.dns.currentUnknown')}</dd>
                <dt class="col-5">{t('serverDetail.dns.testLabel')}</dt><dd class="col-7">
                  {#if server.dns_test_status}
                    <span class="badge {dnsBadgeClass(server.dns_test_status)}" title={server.dns_test_detail || ''} data-testid="dns-test-badge">{t('serverDetail.dns.status.' + server.dns_test_status)}</span>
                    {#if server.dns_test_at}<span class="text-body-secondary small ms-1">({dnsFmtTime(server.dns_test_at)})</span>{/if}
                  {:else}
                    <span class="text-body-secondary">{t('serverDetail.dns.testNever')}</span>
                  {/if}
                </dd>
              {/if}
              <dt class="col-5">{t('serverDetail.time.timezone')}</dt>
              <dd class="col-7" data-testid="time-timezone">{server.timezone || '-'}</dd>
              <dt class="col-5">{t('serverDetail.time.clock')}</dt>
              <dd class="col-7">
                {#if server.time_checked_at}
                  <span class="badge {clockBadgeClass(server)}" data-testid="time-clock-badge">{clockLabel(server)}</span>
                  {#if server.ntp_service}
                    <span class="badge border text-body-secondary ms-1" title={server.ntp_servers || ''}>
                      {server.ntp_service}{server.ntp_synchronized ? ' ✓' : ''}
                    </span>
                  {:else if !isContainer}
                    <span class="badge border text-body-secondary ms-1">{t('serverDetail.time.noNtp')}</span>
                  {/if}
                {:else}
                  <span class="text-body-secondary">{t('serverDetail.time.never')}</span>
                {/if}
              </dd>
              {#if isContainer}
                <dd class="col-12 small text-body-secondary mt-1">{t('serverDetail.time.containerNote', { type: server.virtualization })}</dd>
              {/if}
            </dl>
          </div></div>
        </div>
        <div class="col-md-6">
          <div class="card"><div class="card-body">
            <h2 class="h6">{t('serverDetail.overview.systemSecurity')}</h2>
            <dl class="row mb-0 small">
              <dt class="col-5">{t('serverDetail.overview.os')}</dt><dd class="col-7">{server.os_name} {server.os_version}</dd>
            {#if isProxmox}
              <dt class="col-5">{t('serverDetail.overview.product')}</dt><dd class="col-7">{proxmoxName} <span class="text-body-secondary">{server.proxmox_version}</span></dd>
            {/if}
              <dt class="col-5">{t('serverDetail.overview.support')}</dt>
              <dd class="col-7">
                {#if status?.os_support?.known}
                  <span class="badge {supportBadge(status.os_support)}">{supportLabel(status.os_support)}</span>
                  {#if status.os_support.is_lts}<span class="badge border text-body-secondary ms-1">LTS</span>{/if}
                  <div class="text-body-secondary mt-1">{supportSummary(status.os_support)}</div>
                {:else}
                  <span class="text-body-secondary">{t('serverDetail.support.unknown')}</span>
                {/if}
              </dd>
              <!-- Laufender Kernel. Bewusst NICHT „installierter Kernel": Nach
                   einem Kernel-Update weichen beide bis zum Neustart
                   voneinander ab, und was zählt, ist der gebootete. Die volle
                   Liste steht in der Kernel-Karte darunter. -->
              {#if !isAPIDevice}
              <dt class="col-5">{t('serverDetail.overview.kernel')}</dt>
              <dd class="col-7">
                <span class="font-monospace" data-testid="kernel-running">{server.kernel_version || '-'}</span>
                {#if kernel?.reboot_pending}
                  <span class="badge text-bg-warning ms-1" data-testid="kernel-reboot-badge"
                    title={t('serverDetail.kernel.rebootHint')}>{t('serverDetail.kernel.newerInstalled')}</span>
                {/if}
                {#if kernel && !kernel.managed}
                  <div class="text-body-secondary small mt-1" data-testid="kernel-container-note">
                    {t('serverDetail.kernel.fromHost', { type: kernel.container })}
                  </div>
                {/if}
              </dd>
              <dt class="col-5">{t('serverDetail.overview.rebootRequired')}</dt>
              <dd class="col-7">
                {#if server.reboot_required}
                  <span class="badge text-bg-warning" data-testid="reboot-required-badge">{t('serverDetail.overview.rebootYes')}</span>
                {:else}
                  {t('serverDetail.overview.no')}
                {/if}
              </dd>
              <dt class="col-5">{t('serverDetail.overview.sshHardened')}</dt><dd class="col-7">{server.ssh_hardened ? t('serverDetail.overview.yes') : t('serverDetail.overview.no')}</dd>
              <dt class="col-5">{t('serverDetail.overview.firewall')}</dt><dd class="col-7">{server.firewall_active ? t('serverDetail.overview.firewallActive') : t('serverDetail.overview.firewallInactive')}{#if server.firewall_tool} <span class="badge text-bg-secondary" data-testid="firewall-tool-badge">{server.firewall_tool}</span>{/if}{#if server.firewall_active} <span class="text-body-secondary">({t('serverDetail.overview.ports')}: {server.ssh_port}{#if server.firewall_allowed_ports},{server.firewall_allowed_ports}{/if})</span>{/if}
                <!-- Docker veröffentlicht Ports vor der ufw-Kette. Ohne diesen
                     Zusatz behauptet die Zeile darüber etwas Falsches über die
                     Erreichbarkeit des Systems. LCM greift bewusst nicht ein. -->
                {#if server.firewall_active && dockerExposures.length > 0}
                  <div class="alert alert-warning py-2 px-2 small mt-2 mb-0" data-testid="docker-firewall-bypass">
                    <div class="fw-semibold">{t('serverDetail.overview.dockerBypassTitle', { count: dockerExposures.length })}</div>
                    <ul class="mb-1 ps-3">
                      {#each dockerExposures as e (e.container + e.host_ip + e.host_port)}
                        <li><code>{e.host_port}/{e.proto}</code> - {e.container} <span class="text-body-secondary">({e.host_ip})</span></li>
                      {/each}
                    </ul>
                    <div class="text-body-secondary">
                      {t('serverDetail.overview.dockerBypassHint')}
                      <a href={t('serverDetail.overview.dockerBypassDocsUrl')} target="_blank" rel="noopener">{t('serverDetail.overview.dockerBypassDocsLink')} ↗</a>
                    </div>
                  </div>
                {/if}</dd>
              <dt class="col-5">{t('serverDetail.securityTool.property')}</dt><dd class="col-7" data-testid="security-tool-prop">
                {#if server.crowdsec_installed}
                  CrowdSec <span class="badge {server.crowdsec_active ? 'text-bg-success' : 'text-bg-secondary'}">{server.crowdsec_active ? t('serverDetail.securityTool.active') : t('serverDetail.securityTool.inactive')}</span>
                {/if}
                {#if server.fail2ban_installed}
                  fail2ban <span class="badge {server.fail2ban_active ? 'text-bg-success' : 'text-bg-secondary'}">{server.fail2ban_active ? t('serverDetail.securityTool.active') : t('serverDetail.securityTool.inactive')}</span>
                {/if}
                {#if !server.crowdsec_installed && !server.fail2ban_installed}
                  <span class="text-body-secondary">{t('serverDetail.securityTool.none')}</span>
                {/if}
              </dd>
              <dt class="col-5">{t('serverDetail.overview.fingerprint')}</dt><dd class="col-7 text-truncate"><code class="small">{server.host_key_fingerprint}</code></dd>
              {/if}
              <!-- Synology DSM: statt der Linux-Zeilen die Angaben, die es
                   dort wirklich gibt - Update-Stand, Advisor, Zertifikat. -->
              {#if isDSM}
                <dt class="col-5">{t('serverDetail.dsm.updateState')}</dt>
                <dd class="col-7" data-testid="dsm-update-state">
                  {#if server.dsm_update_available}
                    <span class="badge text-bg-warning">{server.dsm_latest_version || t('serverDetail.dsm.updateAvailable')}</span>
                  {:else}
                    <span class="badge text-bg-success">{t('serverDetail.dsm.currentBadge')}</span>
                  {/if}
                </dd>
                <dt class="col-5">{t('serverDetail.dsm.advisor')}</dt>
                <dd class="col-7" data-testid="dsm-advisor">
                  {#if server.dsm_security_risks > 0}
                    <span class="badge text-bg-warning">{server.dsm_security_risks}</span>
                    <span class="text-body-secondary ms-1">{server.dsm_security_summary}</span>
                  {:else}
                    <span class="text-body-secondary">{t('serverDetail.dsm.advisorClean')}</span>
                  {/if}
                </dd>
                <dt class="col-5">{t('serverDetail.dsm.certFingerprint')}</dt>
                <dd class="col-7 text-truncate"><code class="small">{server.dsm_cert_fingerprint}</code></dd>
              {/if}
            </dl>
          </div></div>
        </div>
        <!-- Kernel-Karte: welcher Kernel LÄUFT und welche sind installiert.
             Die zusätzlich installierten (älteren) Kernel sind die
             Rückfallebene, wenn ein neuer nicht bootet - sie sollen sichtbar
             sein, damit man weiß, ob es sie überhaupt noch gibt.
             In Containern entfällt die Liste: dort kommt der Kernel vom Host
             (siehe Hinweis in der Übersicht). -->
        {#if kernel?.managed && (kernel.installed?.length ?? 0) > 0}
          <div class="col-12">
            <div class="card"><div class="card-body">
              <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
                <h2 class="h6 mb-0">{t('serverDetail.kernel.title')}</h2>
                <span class="badge text-bg-secondary" data-testid="kernel-count">
                  {t('serverDetail.kernel.count', { count: kernel.installed.length })}
                </span>
                {#if kernel.reboot_pending}
                  <span class="badge text-bg-warning" data-testid="kernel-card-reboot">
                    {t('serverDetail.kernel.rebootPending')}
                  </span>
                {/if}
                {#if auth.can('servers:write') && (kernel.removable?.length ?? 0) > 0}
                  <button
                    class="btn btn-sm btn-outline-danger ms-auto"
                    onclick={cleanupKernels}
                    disabled={busy}
                    data-testid="kernel-cleanup"
                  >
                    {t('serverDetail.kernel.cleanupCount', { count: kernel.removable.length })}
                  </button>
                {/if}
              </div>
              <p class="small text-body-secondary mb-2">{t('serverDetail.kernel.intro')}</p>
              <div class="table-responsive">
                <table class="table table-sm align-middle mb-0" data-testid="kernel-table">
                  <thead><tr>
                    <th>{t('serverDetail.kernel.colRelease')}</th>
                    <th>{t('serverDetail.kernel.colPackage')}</th>
                    <th>{t('serverDetail.kernel.colState')}</th>
                  </tr></thead>
                  <tbody>
                    {#each kernel.installed as k, i (k.name)}
                      <tr class={k.running ? 'table-success' : ''}>
                        <td class="small font-monospace">{k.release || '-'}</td>
                        <td class="small font-monospace text-body-secondary">{k.name}</td>
                        <td class="small">
                          {#if k.running}
                            <span class="badge text-bg-success">{t('serverDetail.kernel.running')}</span>
                          {:else if runningIndex >= 0 && i < runningIndex}
                            <!-- Steht in der (nach Fassung sortierten) Liste ÜBER
                                 dem laufenden: also neuer und noch nicht gebootet.
                                 „Rückfallebene" wäre hier schlicht falsch. -->
                            <span class="badge text-bg-warning">{t('serverDetail.kernel.awaitingReboot')}</span>
                          {:else if (kernel.removable ?? []).some((r) => r.name === k.name)}
                            <!-- Weder laufend noch Rückfallebene: reiner
                                 Ballast in /boot. -->
                            <span class="badge border text-body-secondary">{t('serverDetail.kernel.removable')}</span>
                          {:else}
                            <span class="badge border text-body-secondary">{t('serverDetail.kernel.fallback')}</span>
                          {/if}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
              <!-- Läuft ein Kernel, der in keinem Paket steckt (Eigenbau,
                   Fremdquelle), sagt LCM das offen, statt ihn stillschweigend
                   aus der Liste zu verlieren. -->
              {#if !kernel.installed.some((k) => k.running)}
                <div class="alert alert-warning py-2 small mt-2 mb-0" data-testid="kernel-unknown-running">
                  {t('serverDetail.kernel.runningUnknown', { release: kernel.running })}
                </div>
              {/if}
            </div></div>
          </div>
        {/if}
        <div class="col-12">
          <div class="card"><div class="card-body">
            <h2 class="h6">{t('serverDetail.overview.serverGroups')}</h2>
            <div class="d-flex flex-wrap gap-2 mb-2">
              {#each server.groups ?? [] as g (g.id)}
                <span class="badge border text-body-secondary d-inline-flex align-items-center gap-1">
                  {g.name}
                  {#if auth.can('groups:write')}
                    <button class="btn-close btn-close-sm" style="font-size:.6rem" aria-label={t('serverDetail.actions.remove')}
                      onclick={() => action(() => api.groups.removeServer(g.id, id), t('serverDetail.notices.removedFromGroup'))}></button>
                  {/if}
                </span>
              {:else}
                <span class="text-body-secondary small">{t('serverDetail.overview.noGroups')}</span>
              {/each}
            </div>
            {#if auth.can('groups:write') && availableGroups.length > 0}
              <div class="input-group input-group-sm" style="max-width: 360px">
                <select class="form-select" bind:value={addGroupId}>
                  <option value="">{t('serverDetail.overview.addToGroupOption')}</option>
                  {#each availableGroups as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
                </select>
                <button class="btn btn-primary" onclick={addToGroup} disabled={!addGroupId || busy}>{t('serverDetail.actions.add')}</button>
              </div>
            {/if}
          </div></div>
        </div>
      </div>
    {:else if tab === 'packages'}
      {#if auth.can('servers:write')}
        <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
          <button class="btn btn-sm btn-outline-secondary" disabled={pkgBusy || jobLocked} onclick={refreshPackages} title={t('serverDetail.packages.refreshTitle')}>
            {@html icons.refresh} {t('serverDetail.actions.refreshPackages')}
          </button>
          <button class="btn btn-sm btn-primary" disabled={pkgBusy || jobLocked} onclick={upgradeAll}>
            {pkgBusy ? t('serverDetail.packages.running') : t('serverDetail.packages.upgradeAll')}
          </button>
          <button class="btn btn-sm btn-outline-danger" disabled={pkgBusy || jobLocked} onclick={upgradeSecurity}>
            {t('serverDetail.packages.securityOnly')}
          </button>
          <button class="btn btn-sm btn-outline-secondary" disabled={pkgBusy || jobLocked} onclick={autoremove}
            title={t('serverDetail.packages.autoremoveTitle')}>
            {@html icons.trash} {t('serverDetail.packages.autoremove')}
          </button>
          {#if outdated.length > 0}
            <span class="badge text-bg-warning">{t('serverDetail.packages.updatesAvailable', { count: outdated.length })}</span>
          {:else}
            <span class="badge text-bg-success">{t('serverDetail.packages.allCurrent')}</span>
          {/if}
          {#if pkgBusy}<span class="small text-body-secondary">{t('serverDetail.packages.updateRunning')}</span>{/if}
        </div>
      {/if}

      <!-- Paket-Pins: Schutz vor dem Aufräumen (und optional Versions-Stopp).
           Ohne sie entfernt der Autoremove-Lauf regelmäßig ältere Kernel - die
           Rückfallebene, wenn ein neuer Kernel nicht bootet. -->
      {#if auth.can('servers:write') && pins}
        <CollapsibleCard title={t('serverDetail.pins.title')} testid="pin-card">
          {#snippet badge()}
            <!-- Die Anzahl gehört in die Kopfzeile: Sie ist das, was man im
                 Vorbeigehen wissen will, ohne aufzuklappen. -->
            <span class="badge {pinnedNames.length ? 'text-bg-info' : 'text-bg-secondary'}" data-testid="pin-count">
              {pinnedNames.length}
            </span>
          {/snippet}
          <p class="small text-body-secondary mb-2">{t('serverDetail.pins.intro')}</p>
          {#if !pins.available}
            <div class="alert alert-secondary py-2 small mb-0" data-testid="pin-unavailable">
              {isProxmox ? t('serverDetail.pins.proxmoxHint', { name: proxmoxName }) : pins.reason}
            </div>
          {:else}
            <div class="d-flex flex-wrap gap-2 mb-2">
              <button class="btn btn-sm btn-outline-primary" data-testid="pin-kernel"
                disabled={pinBusy || (pins.kernel_preset ?? []).length === 0}
                title={t('serverDetail.pins.kernelTitle')}
                onclick={() => pinKernel(false)}>
                {t('serverDetail.pins.kernelBtn')}
              </button>
              <button class="btn btn-sm btn-outline-secondary" data-testid="pin-apply"
                disabled={pinBusy || jobLocked} onclick={applyPins}>
                {t('serverDetail.pins.applyBtn')}
              </button>
            </div>
            <div class="table-responsive">
              <table class="table table-sm align-middle mb-2">
                <thead><tr>
                  <th>{t('serverDetail.pins.colName')}</th>
                  <th>{t('serverDetail.pins.colScope')}</th>
                  <th>{t('serverDetail.pins.colEffect')}</th>
                  <th>{t('serverDetail.pins.colNote')}</th>
                  <th></th>
                </tr></thead>
                <tbody>
                  {#each [...pins.global, ...pins.server] as pin (pin.id)}
                    <tr>
                      <td class="small font-monospace">{pin.name}</td>
                      <td class="small">
                        <span class="badge {pin.server_id ? 'text-bg-secondary' : 'text-bg-info'}">
                          {pin.server_id ? t('serverDetail.pins.scopeServer') : t('serverDetail.pins.scopeGlobal')}
                        </span>
                      </td>
                      <td class="small">
                        {#if pin.no_remove}<span class="badge text-bg-success me-1">{t('serverDetail.pins.effectNoRemove')}</span>{/if}
                        {#if pin.hold}<span class="badge text-bg-warning">{t('serverDetail.pins.effectHold')}</span>{/if}
                      </td>
                      <td class="small text-body-secondary">{pin.note || '-'}</td>
                      <td class="text-end">
                        <button class="btn btn-sm btn-outline-danger py-0" disabled={pinBusy}
                          aria-label={t('serverDetail.pins.removeAria', { name: pin.name })}
                          onclick={() => removePin(pin)}>×</button>
                      </td>
                    </tr>
                  {:else}
                    <tr><td colspan="5" class="text-body-secondary small" data-testid="pin-empty">
                      {t('serverDetail.pins.empty')}
                    </td></tr>
                  {/each}
                </tbody>
              </table>
            </div>

            <div class="row g-2 align-items-end">
              <div class="col-12 col-md-4">
                <label class="form-label small mb-1" for="pin-name">{t('serverDetail.pins.newName')}</label>
                <input id="pin-name" class="form-control form-control-sm font-monospace" data-testid="pin-name"
                  bind:value={newPin.name} placeholder="linux-image-*" />
              </div>
              <div class="col-12 col-md-3">
                <label class="form-label small mb-1" for="pin-note">{t('serverDetail.pins.newNote')}</label>
                <input id="pin-note" class="form-control form-control-sm" bind:value={newPin.note}
                  placeholder={t('serverDetail.pins.notePlaceholder')} />
              </div>
              <div class="col-12 col-md-auto d-flex flex-wrap gap-3">
                <label class="form-check-label small d-flex align-items-center gap-1">
                  <input type="checkbox" class="form-check-input mt-0" bind:checked={newPin.no_remove} />
                  {t('serverDetail.pins.effectNoRemove')}
                </label>
                <label class="form-check-label small d-flex align-items-center gap-1">
                  <input type="checkbox" class="form-check-input mt-0" bind:checked={newPin.hold} />
                  {t('serverDetail.pins.effectHold')}
                </label>
                <label class="form-check-label small d-flex align-items-center gap-1">
                  <input type="checkbox" class="form-check-input mt-0" bind:checked={newPin.global} data-testid="pin-global" />
                  {t('serverDetail.pins.forAllServers')}
                </label>
              </div>
              <div class="col-12 col-md-auto">
                <button class="btn btn-sm btn-primary" data-testid="pin-add"
                  disabled={pinBusy || !newPin.name.trim() || (!newPin.no_remove && !newPin.hold)}
                  onclick={addPin}>{t('serverDetail.pins.addBtn')}</button>
              </div>
            </div>
            <div class="form-text">{t('serverDetail.pins.hint')}</div>
          {/if}
        </CollapsibleCard>
      {/if}
      <div class="input-group input-group-sm mb-2" style="max-width: 320px">
        <span class="input-group-text">{@html icons.search}</span>
        <input class="form-control" type="search" data-testid="pkg-search"
          placeholder={t('serverDetail.packages.searchPlaceholder')}
          aria-label={t('serverDetail.packages.searchPlaceholder')}
          bind:value={pkgSearch} />
        {#if pkgSearch}
          <button class="btn btn-outline-secondary" onclick={() => (pkgSearch = '')} aria-label={t('common.reset')}>×</button>
        {/if}
      </div>
      <div class="table-responsive">
        <table class="table table-sm align-middle" data-testid="pkg-table">
          <thead>
            <tr>
              {@render sortHead('name', t('serverDetail.packages.colPackage'))}
              {@render sortHead('version', t('serverDetail.packages.colVersion'))}
              {@render sortHead('update', t('serverDetail.packages.colUpdate'))}
              {#if auth.can('servers:write')}<th class="text-end">{t('serverDetail.packages.colAction')}</th>{/if}
            </tr>
          </thead>
          <tbody>
            {#each pagedPackages as p (p.id)}
              {@const hasUpdate = p.candidate_version && p.candidate_version !== p.version}
              {@const pv = pkgVulnMap.get(p.name)}
              {@const pin = pinFor(p.name)}
              <tr class={hasUpdate ? 'table-warning' : ''}>
                <td>
                  {p.name}{#if p.security} <span class="badge bg-danger">security</span>{/if}
                  {#if pin}
                    <span class="badge text-bg-success ms-1" title={t('serverDetail.pins.pinnedBy', { name: pin.name })}>
                      {@html icons.lock} {t('serverDetail.pins.pinnedBadge')}
                    </span>
                  {/if}
                  {#if pv}
                    <button class="badge {severityBadge(pv.worst)} border-0 ms-1" style="cursor: pointer"
                      title={t('serverDetail.packages.cveTitle')}
                      onclick={() => loadTab('security')}>
                      {@html icons.shield} {pv.count === 1 ? t('serverDetail.packages.cveOne', { count: pv.count }) : t('serverDetail.packages.cveMany', { count: pv.count })}
                    </button>
                  {/if}
                </td>
                <td class="small">{p.version}</td>
                <td class="small">{hasUpdate ? p.candidate_version : '-'}</td>
                {#if auth.can('servers:write')}
                  <td class="text-end text-nowrap">
                    {#if hasUpdate}
                      <button class="btn btn-sm btn-outline-primary py-0" disabled={pkgBusy || jobLocked} onclick={() => updateOne(p.name)}>{t('serverDetail.actions.update')}</button>
                    {/if}
                    <button class="btn btn-sm btn-outline-secondary py-0" disabled={pkgBusy || jobLocked} onclick={() => openVersions(p.name)}>{t('serverDetail.packages.versionBtn')}</button>
                    {#if pins?.available && !pin}
                      <button class="btn btn-sm btn-outline-secondary py-0" disabled={pinBusy}
                        title={t('serverDetail.pins.pinPackageTitle')}
                        aria-label={t('serverDetail.pins.pinPackageAria', { name: p.name })}
                        onclick={() => pinPackage(p.name)}>{@html icons.lock}</button>
                    {/if}
                    <!-- Gepinnte Pakete lassen sich nicht entfernen - das
                         Backend weist es ab, also hier gar nicht erst anbieten. -->
                    <button class="btn btn-sm btn-outline-danger py-0" disabled={pkgBusy || jobLocked || !!pin?.no_remove} onclick={() => removeOne(p.name)}
                      title={pin?.no_remove ? t('serverDetail.pins.removeBlocked') : t('serverDetail.packages.removeTitle')} aria-label={t('serverDetail.actions.removePackage', { name: p.name })}>{t('serverDetail.packages.removeBtn')}</button>
                  </td>
                {/if}
              </tr>
              {#if pkgVersionsFor === p.name}
                <tr>
                  <td colspan={auth.can('servers:write') ? 4 : 3} class="bg-body-tertiary">
                    {#if pkgVersions.length === 0}
                      <span class="small text-body-secondary">{t('serverDetail.packages.noVersions')}</span>
                    {:else}
                      <div class="d-flex flex-wrap align-items-center gap-2">
                        <label class="small mb-0" for="pkg-ver-{p.id}">{t('serverDetail.packages.chooseVersion')}</label>
                        <select id="pkg-ver-{p.id}" class="form-select form-select-sm" style="max-width: 320px" bind:value={pkgSelectedVersion}>
                          <option value="">{t('serverDetail.packages.selectPlaceholder')}</option>
                          {#each pkgVersions as v (v)}
                            <option value={v}>{v}{v === p.version ? t('serverDetail.packages.installedSuffix') : ''}</option>
                          {/each}
                        </select>
                        <button class="btn btn-sm btn-primary" disabled={pkgBusy || !pkgSelectedVersion} onclick={() => installVersion(p.name)}>{t('serverDetail.packages.install')}</button>
                        {#if pkgSelectedVersion && pkgSelectedVersion !== p.version && versionLess(pkgSelectedVersion, p.version)}
                          <span class="badge text-bg-warning">{@html icons.warning} {t('serverDetail.packages.downgrade')}</span>
                        {/if}
                      </div>
                    {/if}
                  </td>
                </tr>
              {/if}
            {:else}
              <tr><td colspan={auth.can('servers:write') ? 4 : 3} class="text-body-secondary small">
                {pkgSearch ? t('serverDetail.packages.searchEmpty', { q: pkgSearch }) : t('serverDetail.packages.empty')}
              </td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={pkgPage}
        pageCount={pageCount(sortedPackages.length)}
        total={sortedPackages.length}
        pageSize={PAGE_SIZE}
        testid="pkg-pagination"
        onchange={(p) => (pkgPage = p)}
      />
    {:else if tab === 'snaps'}
      <p class="small text-body-secondary">
        {t('serverDetail.snaps.introA')}<strong>{t('serverDetail.snaps.channelBold')}</strong>{t('serverDetail.snaps.introB')}<code>latest/stable</code>{t('serverDetail.snaps.introC')}
      </p>
      {#if auth.can('servers:write') && snaps.length > 0}
        <div class="d-flex flex-wrap gap-2 mb-3">
          <button class="btn btn-sm btn-primary" onclick={refreshAllSnaps} disabled={busy} data-testid="snap-refresh-all">
            {t('serverDetail.snaps.refreshAll')}
          </button>
          {#if snapUpdates > 0}
            <span class="align-self-center small text-body-secondary" data-testid="snap-update-count">
              {t('serverDetail.snaps.updatesPending', { count: snapUpdates })}
            </span>
          {/if}
        </div>
      {/if}
      <div class="table-responsive">
        <table class="table table-sm align-middle">
          <thead><tr><th>{t('serverDetail.snaps.colSnap')}</th><th>{t('serverDetail.snaps.colVersion')}</th><th>{t('serverDetail.snaps.colChannel')}</th><th>{t('serverDetail.snaps.colPublisher')}</th><th>{t('serverDetail.snaps.colUpdate')}</th><th class="text-end"></th></tr></thead>
          <tbody>
            {#each pageSlice(snaps, snapPage) as s (s.id)}
              {@const hasUpdate = s.candidate_version && s.candidate_version !== s.version}
              <tr class={hasUpdate ? 'table-warning' : ''}>
                <td>{s.name}</td>
                <td class="small">{s.version}{#if s.revision} <span class="text-body-secondary">(rev {s.revision})</span>{/if}</td>
                <td class="small">{s.channel || '-'}</td>
                <td class="small">{s.publisher || '-'}</td>
                <td class="small">{hasUpdate ? s.candidate_version : '-'}</td>
                <td class="text-end text-nowrap">
                  {#if auth.can('servers:write')}
                    {#if hasUpdate}
                      <button class="btn btn-sm btn-outline-primary py-0" onclick={() => refreshSnap(s.name)} disabled={busy}>
                        {t('serverDetail.snaps.refreshOne')}
                      </button>
                    {/if}
                    <!-- snapd und die core-Basen tragen alle übrigen Snaps -
                         sie zu entfernen legte die Snap-Verwaltung still. Der
                         Server weist das ohnehin ab; hier gibt es die
                         Schaltfläche gar nicht erst. -->
                    {#if !snapGeschuetzt(s.name)}
                      <button class="btn btn-sm btn-outline-danger py-0" onclick={() => removeSnap(s.name)} disabled={busy}>
                        {t('common.delete')}
                      </button>
                    {/if}
                  {/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="6" class="text-body-secondary small">
                {t('serverDetail.snaps.empty')}
              </td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={snapPage}
        pageCount={pageCount(snaps.length)}
        total={snaps.length}
        pageSize={PAGE_SIZE}
        testid="snap-pagination"
        onchange={(p) => (snapPage = p)}
      />
    {:else if tab === 'docker'}
      <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
        <h2 class="h6 mb-0 me-2">{t('serverDetail.docker.containers')}</h2>
        {#if !docker?.has_compose}
          <span class="badge text-bg-secondary" title={t('serverDetail.docker.noComposeTitle')}>{t('serverDetail.docker.noComposeBadge')}</span>
        {/if}
        {#if auth.can('servers:write')}
          <button class="btn btn-sm btn-primary ms-auto" disabled={dockerBusy || jobLocked} onclick={dockerPullAll}
            data-testid="docker-pull-all" title={t('serverDetail.docker.pullAllTitle')}>
            {@html icons.refresh} {t('serverDetail.docker.pullAll')}
          </button>
          <button class="btn btn-sm btn-outline-secondary" disabled={dockerBusy || jobLocked} onclick={refreshDocker} title={t('serverDetail.docker.refreshTitle')}>
            {@html icons.refresh} {t('serverDetail.docker.refreshInventory')}
          </button>
        {/if}
        {#if dockerBusy}<span class="small text-body-secondary">{t('serverDetail.docker.actionRunning')}</span>{/if}
      </div>
      {#each dockerGroups as [project, containers] (project)}
        <div class="card mb-3"><div class="card-body">
          <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
            {#if project}
              <h3 class="h6 mb-0">{@html icons.box} {t('serverDetail.docker.composeProject')} <code>{project}</code></h3>
              {#if auth.can('servers:write') && docker?.has_compose}
                <button class="btn btn-sm btn-primary ms-auto" disabled={dockerBusy || jobLocked}
                  onclick={() => composeUpdate(project)}
                  title={t('serverDetail.docker.projectUpdateTitle')}>
                  {t('serverDetail.docker.projectUpdate')}
                </button>
              {/if}
            {:else}
              <h3 class="h6 mb-0">{t('serverDetail.docker.standalone')}</h3>
              <span class="small text-body-secondary ms-auto" title={t('serverDetail.docker.standaloneTitle')}>
                {t('serverDetail.docker.standaloneHint')}
              </span>
            {/if}
          </div>
          <div class="table-responsive">
            <table class="table table-sm align-middle mb-0">
              <thead><tr><th>{t('serverDetail.docker.colContainer')}</th>{#if project}<th>{t('serverDetail.docker.colService')}</th>{/if}<th>{t('serverDetail.docker.colImage')}</th><th>{t('serverDetail.docker.colStatus')}</th><th>{t('serverDetail.docker.colPorts')}</th>{#if project && auth.can('servers:write') && docker?.has_compose}<th class="text-end">{t('serverDetail.docker.colAction')}</th>{/if}</tr></thead>
              <tbody>
                {#each containers as c (c.id)}
                  {@const iv = imageVulnMap.get(c.image)}
                  <tr>
                    <td>{c.name}</td>
                    {#if project}<td class="small">{c.compose_service || '-'}</td>{/if}
                    <td class="small">
                      <code>{c.image}</code>
                      {#if iv}
                        <span class="badge {iv.critical_vulns > 0 ? 'bg-danger' : 'bg-warning text-dark'} ms-1"
                          title={t('serverDetail.docker.containerVulnTitle')}>
                          {@html icons.shield} {iv.critical_vulns > 0 ? t('serverDetail.docker.criticalN', { count: iv.critical_vulns }) : t('serverDetail.docker.highN', { count: iv.high_vulns })}
                        </span>
                      {/if}
                      {#if auth.can('servers:write')}
                        <button
                          class="badge border ms-1 {cveRelevantSet.has(c.name.toLowerCase()) ? 'text-bg-danger border-danger' : 'bg-transparent text-body-secondary'}"
                          style="cursor: pointer"
                          data-testid="cve-relevance-toggle"
                          title={cveRelevantSet.has(c.name.toLowerCase()) ? t('serverDetail.docker.cveRelevantOnTitle') : t('serverDetail.docker.cveRelevantOffTitle')}
                          onclick={() => toggleCveRelevant(c)}>
                          {@html icons.shield} {t('serverDetail.docker.cveRelevant')}
                        </button>
                      {:else if cveRelevantSet.has(c.name.toLowerCase())}
                        <span class="badge text-bg-danger ms-1" title={t('serverDetail.docker.cveRelevantOnTitle')}>{@html icons.shield} {t('serverDetail.docker.cveRelevant')}</span>
                      {/if}
                    </td>
                    <td>
                      <span class="badge {c.state === 'running' ? 'bg-success' : c.state === 'restarting' ? 'bg-warning text-dark' : 'text-bg-secondary'}">{c.state}</span>
                      <span class="small text-body-secondary ms-1">{c.status}</span>
                    </td>
                    <td class="small text-body-secondary">{c.ports || '-'}</td>
                    {#if project && auth.can('servers:write') && docker?.has_compose}
                      <td class="text-end">
                        {#if c.compose_service}
                          <button class="btn btn-sm btn-outline-primary py-0" disabled={dockerBusy || jobLocked}
                            onclick={() => composeUpdate(project, c.compose_service)}>{t('serverDetail.docker.serviceUpdate')}</button>
                        {/if}
                      </td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div></div>
      {:else}
        <div class="alert alert-info small">{t('serverDetail.docker.noContainers')}</div>
      {/each}

      <div class="d-flex flex-wrap align-items-center gap-2">
        <h2 class="h6 mb-0 me-auto">{t('serverDetail.docker.images')}</h2>
        {#if auth.can('servers:write') && docker?.has_docker}
          <button class="btn btn-sm btn-outline-danger" disabled={dockerBusy || jobLocked} onclick={dockerPrune}
            title={t('serverDetail.docker.pruneTitle')}>
            {@html icons.trash} {t('serverDetail.docker.pruneBtn')}
          </button>
        {/if}
      </div>
      <p class="small text-body-secondary">
        {t('serverDetail.docker.imagesIntroA')}<strong>Trivy</strong>{t('serverDetail.docker.imagesIntroB')}
      </p>
      <div class="table-responsive">
        <table class="table table-sm align-middle">
          <thead><tr><th>{t('serverDetail.docker.colImage')}</th><th>{t('serverDetail.docker.colDigest')}</th><th>{t('serverDetail.docker.colSize')}</th><th>{t('serverDetail.docker.colStatus')}</th><th>{t('serverDetail.docker.colCves')}</th>{#if auth.can('servers:write')}<th class="text-end">{t('serverDetail.docker.colAction')}</th>{/if}</tr></thead>
          <tbody>
            {#each pageSlice(docker?.images ?? [], imagePage) as img (img.id)}
              {@const ref = img.tag ? `${img.repository}:${img.tag}` : img.repository}
              {@const check = CHECK_LABELS[img.check_status]}
              <tr class={img.update_available && img.in_use ? 'table-warning' : ''}>
                <td>
                  <code class="small">{ref}</code>
                  {#if !img.in_use}<span class="badge border text-body-secondary ms-1" title={t('serverDetail.docker.unusedTitle')}>{t('serverDetail.docker.unused')}</span>{/if}
                </td>
                <td class="small text-body-secondary">{shortDigest(img.repo_digest)}</td>
                <td class="small">{img.size_text || '-'}</td>
                <td>
                  {#if img.update_available}
                    <span class="badge bg-warning text-dark">{t('serverDetail.docker.updateAvailable')}</span>
                  {:else if check}
                    <span class="badge {check.cls}">{check.text}</span>
                  {:else if img.check_status === 'ok'}
                    <span class="badge text-bg-success">{t('serverDetail.docker.current')}</span>
                  {:else}
                    <span class="small text-body-secondary">{t('serverDetail.docker.notChecked')}</span>
                  {/if}
                </td>
                <td class="small">
                  {#if img.critical_vulns > 0}<span class="badge bg-danger">{t('serverDetail.docker.criticalN', { count: img.critical_vulns })}</span>{/if}
                  {#if img.high_vulns > 0}<span class="badge bg-warning text-dark ms-1">{t('serverDetail.docker.highN', { count: img.high_vulns })}</span>{/if}
                  {#if !img.critical_vulns && !img.high_vulns}-{/if}
                </td>
                {#if auth.can('servers:write')}
                  <td class="text-end text-nowrap">
                    {#if img.update_available && img.tag}
                      <button class="btn btn-sm btn-outline-primary py-0" disabled={dockerBusy || jobLocked}
                        onclick={() => dockerPull(ref)} title={t('serverDetail.docker.pullTitle')}>Pull</button>
                    {/if}
                    {#if !img.in_use}
                      <button class="btn btn-sm btn-outline-danger py-0" disabled={dockerBusy || jobLocked}
                        onclick={() => dockerRemoveImage(ref)} title={t('serverDetail.docker.removeTitle')}>{t('common.delete')}</button>
                    {/if}
                  </td>
                {/if}
              </tr>
            {:else}
              <tr><td colspan={auth.can('servers:write') ? 6 : 5} class="text-body-secondary small">{t('serverDetail.docker.noImages')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={imagePage}
        pageCount={pageCount((docker?.images ?? []).length)}
        total={(docker?.images ?? []).length}
        pageSize={PAGE_SIZE}
        testid="image-pagination"
        onchange={(p) => (imagePage = p)}
      />
    {:else if tab === 'users'}
      <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
        <h2 class="h6 mb-0 me-2">{t('serverDetail.users.title')}</h2>
        {#if auth.can('servers:write')}
          <div class="ms-auto d-flex gap-2">
            <button class="btn btn-sm btn-outline-secondary" data-testid="users-sync"
              disabled={serverUsersBusy || jobLocked || isProxmox || userSyncDisabled}
              title={isProxmox || userSyncDisabled ? t('serverDetail.users.syncLocked') : undefined}
              onclick={syncLcmUsers}>
              {@html icons.users} {t('serverDetail.users.sync')}
            </button>
            <button class="btn btn-sm btn-outline-primary" data-testid="users-refresh"
              disabled={serverUsersBusy || jobLocked} onclick={refreshServerUsersNow}>
              {#if serverUsersBusy}{t('common.loading')}{:else}{@html icons.refresh} {t('serverDetail.users.refresh')}{/if}
            </button>
          </div>
        {/if}
      </div>
      <p class="small text-body-secondary">
        {t('serverDetail.users.intro')}
        {#if server.ssh_2fa_enabled}
          <span class="d-block mt-1">{@html icons.shield} {t('serverDetail.users.tfaActiveHint')}</span>
        {/if}
      </p>
      {#if pendingUserSyncs.length > 0}
        <!-- Der Rückstand gehört sichtbar: Solange er offen ist, weicht der
             Server von dem ab, was LCM anzeigt - und ein entzogener Zugang
             besteht dort noch. -->
        <div class="alert alert-warning py-2 small" data-testid="user-sync-pending">
          <strong>{t('serverDetail.users.pendingTitle', { count: pendingUserSyncs.length })}</strong>
          <span class="d-block">{t('serverDetail.users.pendingHint')}</span>
          <ul class="mb-0 mt-1">
            {#each pendingUserSyncs as p (p.id)}
              <li>
                {p.username ? t('serverDetail.users.pendingRemove', { name: p.username }) : t('serverDetail.users.pendingDistribute')}
                {#if p.last_error}<span class="text-body-secondary">- {p.last_error}</span>{/if}
              </li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if auth.can('servers:write') && lcmUsers.length > 0}
        {@const assignable = lcmUsers.filter((lu) => lu.active && !(serverUsers ?? []).some((u) => u.username === lu.username))}
        {#if assignable.length > 0}
          <div class="card mb-3">
            <div class="card-body">
              <h3 class="h6">{@html icons.users} {t('serverDetail.users.assignTitle')}</h3>
              <p class="small text-body-secondary mb-2">{t('serverDetail.users.assignIntro')}</p>
              <div class="d-flex flex-wrap gap-2">
                <select class="form-select" style="max-width: 20rem" bind:value={assignUserId} data-testid="users-assign-select">
                  <option value="">{t('serverDetail.users.assignPlaceholder')}</option>
                  {#each assignable as lu (lu.id)}
                    <option value={lu.id}>{lu.username}{lu.full_name ? ` - ${lu.full_name}` : ''}</option>
                  {/each}
                </select>
                <button class="btn btn-primary" data-testid="users-assign"
                  disabled={!assignUserId || serverUsersBusy || jobLocked} onclick={assignLcmUser}>
                  {t('serverDetail.users.assign')}
                </button>
              </div>
            </div>
          </div>
        {/if}
      {/if}
      <div class="table-responsive">
        <table class="table table-sm align-middle" data-testid="users-table">
          <thead><tr>
            <th>{t('serverDetail.users.colName')}</th>
            <th>{t('serverDetail.users.colLogin')}</th>
            <th>{t('serverDetail.users.colKeys')}</th>
            <th>2FA</th>
            <th>{t('serverDetail.users.colLastLogin')}</th>
            <th></th>
          </tr></thead>
          <tbody>
            {#each pageSlice(serverUsers ?? [], userPage) as u (u.username)}
              <tr class={u.disabled ? 'table-secondary' : ''}>
                <td>
                  <span class="font-monospace">{u.username}</span>
                  <span class="text-body-secondary small ms-1">({u.uid})</span>
                  {#if u.managed}<span class="badge text-bg-info ms-1" title={t('serverDetail.users.managedHint')}>LCM</span>{/if}
                  {#if u.username === server.service_user}<span class="badge border text-body-secondary ms-1">{t('serverDetail.users.serviceUser')}</span>{/if}
                  {#if u.disabled}<span class="badge text-bg-danger ms-1" data-testid={`user-disabled-${u.username}`}>{t('serverDetail.users.stateDisabled')}</span>{/if}
                  {#if u.blocked}<span class="badge text-bg-warning text-dark ms-1" title={t('serverDetail.users.blockedHint')} data-testid={`user-blocked-${u.username}`}>{t('serverDetail.users.stateBlocked')}</span>{/if}
                </td>
                <td>
                  {#if u.password_status === 'set'}
                    <span class="badge text-bg-warning" title={t('serverDetail.users.pwHint')}>{t('serverDetail.users.pwPassword')}</span>
                  {:else if u.ssh_key_count > 0}
                    <span class="badge text-bg-success">{t('serverDetail.users.pwKeyOnly')}</span>
                  {:else if u.password_status === 'unknown'}
                    <span class="badge text-bg-secondary">{t('serverDetail.users.pwUnknown')}</span>
                  {:else}
                    <span class="badge text-bg-secondary" title={t('serverDetail.users.pwNoneHint')}>{t('serverDetail.users.pwNone')}</span>
                  {/if}
                </td>
                <td class="small">{u.ssh_key_count}</td>
                <td>
                  {#if u.two_factor_enrolled}
                    <span class="badge text-bg-success" data-testid={`user-2fa-${u.username}`}>{t('serverDetail.users.tfaYes')}</span>
                  {:else}
                    <span class="text-body-secondary small">-</span>
                  {/if}
                </td>
                <td class="small">
                  {u.last_login_at ? lastSeen(u.last_login_at) : t('serverDetail.users.neverLoggedIn')}
                  {#if u.logins_from_lcm}
                    <!-- Herkunft benennen: Diese Zahlen stammen aus LCMs
                         eigenem Protokoll, nicht aus wtmp - sonst wirkt der
                         Unterschied zu `last` auf dem Server wie ein Fehler. -->
                    <span class="badge border text-body-secondary ms-1"
                      title={t('serverDetail.users.fromLcmLogHint')}>{t('serverDetail.users.fromLcmLog')}</span>
                  {/if}
                  {#if u.login_count > 0}
                    <button class="btn btn-link btn-sm p-0 ms-1 align-baseline"
                      data-testid={`user-logins-${u.username}`}
                      onclick={() => toggleLogins(u)}>
                      {openLogins === u.username
                        ? t('serverDetail.users.hideLogins')
                        : t('serverDetail.users.showLogins', { n: u.login_count })}
                    </button>
                  {/if}
                </td>
                <td class="text-end text-nowrap">
                  {#if auth.can('servers:write') && u.username !== 'root' && u.username !== server.service_user}
                    <button class="btn btn-sm btn-outline-secondary"
                      data-testid={`user-toggle-${u.username}`}
                      disabled={serverUsersBusy || jobLocked}
                      onclick={() => toggleServerUser(u)}>
                      {u.disabled || u.blocked ? t('serverDetail.users.enable') : t('serverDetail.users.disable')}
                    </button>
                    <!-- Endgültig entfernen nur bei unverwalteten Konten: ein
                         verteiltes Konto legte der nächste Sync ohnehin wieder
                         an - dafür ist die Linux-Benutzer-Verwaltung da. -->
                    {#if !u.managed}
                      <button class="btn btn-sm btn-outline-danger"
                        data-testid={`user-remove-${u.username}`}
                        disabled={serverUsersBusy || jobLocked}
                        onclick={() => removeServerUserNow(u)}>
                        {@html icons.trash}
                      </button>
                    {/if}
                  {/if}
                </td>
              </tr>
              {#if openLogins === u.username}
                <tr data-testid={`user-logins-row-${u.username}`}>
                  <td colspan="6" class="bg-body-tertiary">
                    {#if loginsBusy}
                      <span class="small text-body-secondary">{t('common.loading')}</span>
                    {:else}
                      <div class="small fw-semibold mb-1">{t('serverDetail.users.loginsTitle', { name: u.username })}</div>
                      <div class="table-responsive">
                        <table class="table table-sm mb-1">
                          <thead><tr>
                            <th>{t('serverDetail.users.colWhen')}</th>
                            <th>{t('serverDetail.users.colFrom')}</th>
                            <th>{t('serverDetail.users.colTty')}</th>
                            <th>{t('serverDetail.users.colDuration')}</th>
                          </tr></thead>
                          <tbody>
                            {#each logins as l (l.id)}
                              <tr>
                                <td class="small">{new Date(l.started_at).toLocaleString()}</td>
                                <td class="small font-monospace">{l.from_host || t('serverDetail.users.localConsole')}</td>
                                <td class="small font-monospace">{l.tty}</td>
                                <td class="small">
                                  {#if l.still_active}
                                    <span class="badge text-bg-success">{t('serverDetail.users.stillActive')}</span>
                                  {:else}{loginDuration(l) || '-'}{/if}
                                </td>
                              </tr>
                            {/each}
                          </tbody>
                        </table>
                      </div>
                      <div class="form-text">{t('serverDetail.users.loginsHint')}</div>
                    {/if}
                  </td>
                </tr>
              {/if}
            {:else}
              <tr><td colspan="6" class="text-body-secondary small" data-testid="users-empty">
                {t('serverDetail.users.empty')}
              </td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={userPage}
        pageCount={pageCount((serverUsers ?? []).length)}
        total={(serverUsers ?? []).length}
        pageSize={PAGE_SIZE}
        testid="user-pagination"
        onchange={(p) => (userPage = p)}
      />
    {:else if tab === 'security'}
      <!-- Grundzustand härten: Ein Berechtigungsprofil GIBT Rechte, es nimmt
           keine weg. Wer will, dass ein Datenbereich nur den Berechtigten
           offensteht, entfernt hier zuerst den Welt-Zugriff. -->
      <!-- ACL-Fähigkeit: Voraussetzung für die Verzeichnisrechte aus den
           Berechtigungsprofilen. LCM rät sie nicht, sondern prüft sie im Scan. -->
      <div class="card mb-3">
        <div class="card-body d-flex flex-wrap align-items-center gap-2">
          <h3 class="h6 mb-0 me-2">{t('serverDetail.harden.aclTitle')}</h3>
          {#if server?.acl_usable}
            <span class="badge text-bg-success">{t('serverDetail.harden.aclOk')}</span>
          {:else if server?.has_acl}
            <span class="badge text-bg-warning">{t('serverDetail.harden.aclFsMissing')}</span>
          {:else}
            <span class="badge text-bg-secondary">{t('serverDetail.harden.aclMissing')}</span>
          {/if}
          <span class="small text-body-secondary">{t('serverDetail.harden.aclHint')}</span>
          {#if !server?.acl_usable && !server?.has_acl && auth.can('servers:write')}
            <button class="btn btn-sm btn-outline-primary ms-auto" onclick={installACL} disabled={busy}>
              {t('serverDetail.harden.aclInstall')}
            </button>
          {/if}
        </div>
      </div>

      <CollapsibleCard title={t('serverDetail.harden.title')} testid="harden-card">
        {#snippet badge()}
          <!-- Wie viele Verzeichnisse abgeschottet sind, steht in der
               Kopfzeile - sonst müsste man zum Nachsehen aufklappen. -->
          <span class="badge {hardenedPaths.length ? 'text-bg-info' : 'text-bg-secondary'}" data-testid="harden-count">
            {hardenedPaths.length}
          </span>
        {/snippet}
        <p class="small text-body-secondary">{t('serverDetail.harden.intro')}</p>
        {#if hardenedPaths.length}
          <ul class="list-group list-group-flush mb-2">
            {#each hardenedPaths as row (row.id)}
              <li class="list-group-item d-flex justify-content-between align-items-center py-1 px-0">
                <span class="small font-monospace">{row.path}</span>
                <span class="small text-body-secondary">
                  {row.prev_mode} {row.prev_group} → {row.mode} {row.group}
                </span>
                {#if auth.can('servers:write')}
                  <button class="btn btn-sm btn-outline-secondary py-0" onclick={() => restorePath(row)}>
                    {t('serverDetail.harden.restore')}
                  </button>
                {/if}
              </li>
            {/each}
          </ul>
        {:else}
          <p class="small text-body-secondary">{t('serverDetail.harden.none')}</p>
        {/if}
        {#if auth.can('servers:write')}
          <!-- Generelle Härtung: LCM sucht die Standardverzeichnisse ab,
               statt dass man sie einzeln von Hand einträgt. -->
          <div class="border rounded p-2 mb-3">
            <div class="d-flex flex-wrap align-items-center gap-2">
              <strong class="small">{t('serverDetail.harden.suggestTitle')}</strong>
              <button class="btn btn-sm btn-outline-primary ms-auto" onclick={findHardenSuggestions} disabled={busy}>
                {t('serverDetail.harden.suggestSearch')}
              </button>
            </div>
            <p class="small text-body-secondary mb-0 mt-1">{t('serverDetail.harden.suggestHint')}</p>
            {#if hardenSuggestions}
              {#if hardenSuggestions.length}
                <table class="table table-sm align-middle mt-2 mb-2">
                  <thead>
                    <tr>
                      <th style="width:2rem"></th>
                      <th>{t('serverDetail.harden.path')}</th>
                      <th>{t('serverDetail.harden.suggestMode')}</th>
                      <th>{t('serverDetail.harden.suggestGroup')}</th>
                      <th>{t('serverDetail.harden.suggestKind')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each hardenSuggestions as s (s.path)}
                      <tr>
                        <td>
                          <input class="form-check-input" type="checkbox" aria-label={s.path}
                            checked={hardenChosen.has(s.path)} onchange={() => toggleSuggestion(s.path)} />
                        </td>
                        <td class="small font-monospace">{s.path}</td>
                        <td class="small">{s.mode} {s.group}</td>
                        <td class="small">{s.service_group}</td>
                        <td class="small text-body-secondary">
                          {s.kind === 'konfig' ? t('serverDetail.harden.kindConfig') : t('serverDetail.harden.kindData')}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
                <button class="btn btn-sm btn-primary" onclick={hardenChosenPaths} disabled={busy || hardenChosen.size === 0}>
                  {t('serverDetail.harden.suggestApply', { count: hardenChosen.size })}
                </button>
              {:else}
                <p class="small text-body-secondary mt-2 mb-0">{t('serverDetail.harden.suggestNone')}</p>
              {/if}
            {/if}
          </div>

          <div class="row g-2 align-items-end">
            <div class="col-md-5">
              <label class="form-label small mb-1" for="hd-path">{t('serverDetail.harden.path')}</label>
              <input id="hd-path" class="form-control form-control-sm font-monospace" placeholder="/srv/kundendaten" bind:value={hardenForm.path} />
            </div>
            <div class="col-md-3">
              <label class="form-label small mb-1" for="hd-group">{t('serverDetail.harden.group')}</label>
              <input id="hd-group" class="form-control form-control-sm font-monospace" placeholder="www-data" bind:value={hardenForm.group} />
            </div>
            <div class="col-md-3">
              <label class="form-label small mb-1" for="hd-unit">{t('serverDetail.harden.unit')}</label>
              <input id="hd-unit" class="form-control form-control-sm font-monospace" placeholder="nginx" bind:value={hardenForm.unit} />
            </div>
            <div class="col-md-1">
              <button class="btn btn-sm btn-primary w-100" onclick={hardenPath} disabled={busy || !hardenForm.path.trim()}>
                {t('serverDetail.harden.apply')}
              </button>
            </div>
          </div>
        <p class="small text-body-secondary mt-1 mb-0">{t('serverDetail.harden.hint')}</p>
      {/if}
      </CollapsibleCard>

      <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
        <h2 class="h6 mb-0 me-2">{t('serverDetail.security.title')}</h2>
        {#if vulnReport?.summary}
          {#each ['critical', 'high', 'medium', 'low'] as sev}
            {#if vulnReport.summary[sev]}
              <span class="badge {severityBadge(sev)}">{severityLabel(sev)}: {vulnReport.summary[sev]}</span>
            {/if}
          {/each}
        {/if}
        {#if unusedCount > 0}
          <!-- Getrennt ausgewiesen, nicht mitgezaehlt: Ein Image, das kein
               Container nutzt, hat keine Angriffsflaeche. -->
          <span class="badge border text-body-secondary" data-testid="vuln-unused-badge"
            title={t('serverDetail.security.unusedHint')}>
            {t('serverDetail.security.unusedN', { count: unusedCount })}
          </span>
        {/if}
        {#if auth.can('servers:write') && vulnReport?.scanner_available}
          <button class="btn btn-sm btn-outline-primary ms-auto" disabled={vulnScanning || jobLocked} onclick={scanVulnerabilities}>
            {#if vulnScanning}{t('serverDetail.security.scanning')}{:else}{@html icons.search} {t('serverDetail.security.scanNow')}{/if}
          </button>
        {/if}
      </div>
      <p class="small text-body-secondary">
        {t('serverDetail.security.introA')}<strong>Trivy</strong>{t('serverDetail.security.introB')}
        {#if vulnReport?.last_scan_at}
          {t('serverDetail.security.lastScan', { when: lastSeen(vulnReport.last_scan_at) })}
        {:else}
          {t('serverDetail.security.noScan')}
        {/if}
        <span class="d-block">{t('serverDetail.security.snapNote')}</span>
        {#if server.listening_packages}
          <span class="d-block mt-1" data-testid="listening-packages">
            {t('serverDetail.security.listeningHint')}
            <code class="small">{server.listening_packages}</code>
          </span>
        {/if}
      </p>
      {#if vulnReport && !vulnReport.scanner_available}
        <div class="alert alert-warning small">
          {t('serverDetail.security.noScanner')}
        </div>
      {/if}

      <!-- SSH-2FA: TOTP als zweiter Faktor neben dem SSH-Key
           (google-authenticator-libpam). Benutzer enrollen sich selbst auf
           dem Server; die Benutzer-Übersicht zeigt, wer schon so weit ist.
           Die Karte erscheint erst, wenn 2FA eingerichtet IST - vorher steht
           sie als Auswahl unter „Sicherheits-Tools einrichten". Eine Karte,
           die nur „nicht aktiv" meldet, kostet Platz und sagt nichts. -->
      {#if server.ssh_2fa_enabled}
        <CollapsibleCard title={t('serverDetail.ssh2fa.title')} testid="ssh-2fa-card">
          {#snippet badge()}
            <span class="badge text-bg-success" data-testid="ssh-2fa-state">
              {t('serverDetail.securityTool.active')}
            </span>
          {/snippet}
          <p class="small text-body-secondary mb-2">{t('serverDetail.ssh2fa.intro')}</p>
          <p class="small mb-2">
            {t('serverDetail.ssh2fa.enrollHintA')}<code>google-authenticator</code>{t('serverDetail.ssh2fa.enrollHintB')}
            <button class="btn btn-link btn-sm p-0 align-baseline" onclick={() => loadTab('users')}>{t('serverDetail.tabs.users')}</button>.
          </p>
          {#if auth.can('servers:write')}
            {#if restricted}
              <div class="alert alert-secondary py-2 small mb-0">{restrictedHint}</div>
            {:else}
              <button class="btn btn-sm btn-outline-danger" data-testid="ssh-2fa-disable"
                disabled={busy || jobLocked} onclick={() => configureSSH2FA(false)}>
                {t('serverDetail.ssh2fa.remove')}
              </button>
            {/if}
          {/if}
        </CollapsibleCard>
      {/if}

      <!-- Verwaltung der installierten Sicherheits-Tools (fail2ban/CrowdSec):
           Dienst steuern, Allowlist nachziehen, Sperren aufheben, entfernen.
           Eine Karte je installiertem Werkzeug - beide können parallel
           installiert sein. -->
      {#each installedSecTools as tl (tl.key)}
        <CollapsibleCard
          title={t('serverDetail.securityTool.manage.title', { tool: tl.label })}
          testid={`sec-manage-${tl.key}`}
        >
          {#snippet badge()}
            <span class="badge {tl.active ? 'text-bg-success' : 'text-bg-secondary'}" data-testid={`sec-state-${tl.key}`}>
              {tl.active ? t('serverDetail.securityTool.active') : t('serverDetail.securityTool.inactive')}
            </span>
            {#if secBusy(tl.key)}
              <!-- Der Fortschritt gehört in die Kopfzeile: Er muss auch dann
                   sichtbar sein, wenn die Karte zugeklappt ist. -->
              <span class="d-flex align-items-center gap-2 small text-body-secondary" data-testid={`sec-progress-${tl.key}`}>
                <span class="spinner-border spinner-border-sm" role="status" aria-hidden="true"></span>
                {t('serverDetail.securityTool.manage.running')}
              </span>
            {/if}
          {/snippet}

          <p class="small text-body-secondary mb-3">{t('serverDetail.securityTool.manage.intro')}</p>

          {#if !canManageSecTools}
            <div class="alert alert-secondary py-2 small mb-0" data-testid={`sec-manage-locked-${tl.key}`}>
              {restricted ? restrictedHint : t('serverDetail.securityTool.manage.noPermission')}
            </div>
          {:else}
            <!-- Dienst steuern -->
            <div class="mb-3">
              <div class="form-label small mb-1">{t('serverDetail.securityTool.manage.serviceTitle')}</div>
              <div class="d-flex flex-wrap gap-2">
                {#each SEC_SERVICE_ACTIONS as act}
                  <button class="btn btn-sm btn-outline-secondary"
                    data-testid={`sec-${act}-${tl.key}`}
                    disabled={secBusy(tl.key) || jobLocked}
                    onclick={() => secServiceAction(tl.key, act)}>
                    {t(`serverDetail.securityTool.manage.action.${act}`)}
                  </button>
                {/each}
                <button class="btn btn-sm btn-outline-danger ms-auto"
                  data-testid={`sec-uninstall-${tl.key}`}
                  disabled={secBusy(tl.key) || jobLocked}
                  onclick={() => secUninstall(tl.key)}>
                  {@html icons.trash} {t('serverDetail.securityTool.manage.action.uninstall')}
                </button>
              </div>
            </div>

            <!-- Allowlist nachziehen (freie IPs + benannte Allowlists) -->
            <div class="mb-3">
              <label class="form-label small mb-1" for={`sec-mg-allow-${tl.key}`}>
                {t('serverDetail.securityTool.manage.allowlistTitle')}
              </label>
              <div class="d-flex flex-wrap gap-2">
                <input id={`sec-mg-allow-${tl.key}`} class="form-control font-monospace flex-grow-1" style="min-width: 16rem"
                  bind:value={secManageIPs[tl.key]} placeholder="1.2.3.4 5.6.7.8"
                  data-testid={`sec-allowlist-input-${tl.key}`} />
                <button class="btn btn-sm btn-primary"
                  data-testid={`sec-allowlist-apply-${tl.key}`}
                  disabled={secBusy(tl.key) || jobLocked}
                  onclick={() => secApplyAllowlist(tl.key)}>
                  {t('serverDetail.securityTool.manage.allowlistApply')}
                </button>
              </div>
              <div class="form-text">{t('serverDetail.securityTool.manage.allowlistHint')}</div>
              {#if ipAllowlists.length > 0}
                <div class="mt-2 small" data-testid={`sec-allowlist-lists-${tl.key}`}>
                  <span class="text-body-secondary">{t('serverDetail.securityTool.allowlistLists')}</span>
                  <div class="d-flex flex-wrap gap-3 mt-1">
                    {#each ipAllowlists as a (a.id)}
                      <label class="d-flex align-items-center gap-1">
                        <input type="checkbox" class="form-check-input mt-0"
                          checked={(secManageIds[tl.key] ?? []).includes(a.id)}
                          onchange={() => toggleSecManageList(tl.key, a.id)} />
                        <span>{a.name}</span>
                      </label>
                    {/each}
                  </div>
                </div>
              {/if}
            </div>

            <!-- Sperrliste: die Ansicht, die man braucht, wenn man sich
                 selbst ausgesperrt hat. -->
            <div>
              <div class="d-flex flex-wrap align-items-center gap-2 mb-1">
                <div class="form-label small mb-0">{t('serverDetail.securityTool.manage.bansTitle')}</div>
                <button class="btn btn-sm btn-outline-secondary"
                  data-testid={`sec-bans-refresh-${tl.key}`}
                  disabled={secBansBusy === tl.key || secBusy(tl.key)}
                  onclick={() => loadSecBans(tl.key)}>
                  {secBansBusy === tl.key ? t('common.loading') : t('serverDetail.securityTool.manage.bansRefresh')}
                </button>
              </div>
              {#if secBansError[tl.key]}
                <div class="alert alert-warning py-2 small mb-0" data-testid={`sec-bans-error-${tl.key}`}>
                  {t('serverDetail.securityTool.manage.bansError', { err: secBansError[tl.key] })}
                </div>
              {:else}
                <div class="table-responsive">
                  <table class="table table-sm align-middle mb-0">
                    <thead><tr>
                      <th>{t('serverDetail.securityTool.manage.colIp')}</th>
                      <th>{t('serverDetail.securityTool.manage.colScope')}</th>
                      <th>{t('serverDetail.securityTool.manage.colDuration')}</th>
                      <th></th>
                    </tr></thead>
                    <tbody>
                      {#each secBans[tl.key] ?? [] as ban (ban.tool + ban.scope + ban.ip)}
                        <tr>
                          <td class="small font-monospace">{ban.ip}</td>
                          <td class="small">{ban.scope || '-'}{#if ban.cause}<span class="text-body-secondary"> · {ban.cause}</span>{/if}</td>
                          <td class="small">{ban.since || '-'}</td>
                          <td class="text-end">
                            <button class="btn btn-sm btn-outline-primary"
                              disabled={secBusy(tl.key) || jobLocked}
                              aria-label={t('serverDetail.securityTool.manage.unbanAria', { ip: ban.ip })}
                              onclick={() => secUnban(tl.key, ban.ip)}>
                              {t('serverDetail.securityTool.manage.unban')}
                            </button>
                          </td>
                        </tr>
                      {:else}
                        <tr><td colspan="4" class="text-body-secondary small" data-testid={`sec-bans-empty-${tl.key}`}>
                          {t('serverDetail.securityTool.manage.noBans')}
                        </td></tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              {/if}
            </div>
          {/if}
        </CollapsibleCard>
      {:else}
        {#if !isAPIDevice}
          <p class="small text-body-secondary" data-testid="sec-manage-none">
            {t('serverDetail.securityTool.manage.notInstalled')}
          </p>
        {/if}
      {/each}

      <div class="d-flex flex-wrap align-items-center gap-3 mt-4 mb-2">
        <h3 class="h6 mb-0">{t('serverDetail.security.findingsTitle')}</h3>
        <div class="d-flex align-items-center gap-2">
          <label class="form-label small mb-0" for="vuln-source">{t('serverDetail.security.filterSource')}</label>
          <select id="vuln-source" class="form-select form-select-sm" style="width: auto"
            data-testid="vuln-source-filter" bind:value={vulnSource}>
            <option value="">{t('serverDetail.security.sourceAll')}</option>
            <option value="os">{t('serverDetail.security.sourceOs')}</option>
            <option value="docker">Docker</option>
          </select>
        </div>
      </div>
      <div class="table-responsive">
        <table class="table table-sm align-middle" data-testid="vuln-table">
          <thead><tr><th>{t('serverDetail.security.colSeverity')}</th><th>{t('serverDetail.security.colSource')}</th><th>{t('serverDetail.security.colCve')}</th><th>{t('serverDetail.security.colPackage')}</th><th>{t('serverDetail.security.colInstalled')}</th><th>{t('serverDetail.security.colFixed')}</th><th>{t('serverDetail.security.colTitle')}</th></tr></thead>
          <tbody>
            {#each vulnPageRows as v (v.id)}
              <!-- Funde aus ungenutzten Images treten zurueck: Sie zaehlen
                   nicht, sollen aber auffindbar bleiben. -->
              <tr class={v.image_unused
                ? 'text-body-secondary'
                : v.severity === 'critical'
                  ? 'table-danger'
                  : v.severity === 'high'
                    ? 'table-warning'
                    : ''}>
                <td><span class="badge {severityBadge(v.severity)}">{severityLabel(v.severity)}</span></td>
                <td class="small">
                  {#if v.source === 'docker'}
                    <span class="badge text-bg-info" title={v.image_ref}>Docker</span>
                    {#if v.image_ref}<div class="text-body-secondary"><code class="small">{v.image_ref}</code></div>{/if}
                    {#if v.image_unused}
                      <div><span class="badge border text-body-secondary" data-testid="vuln-unused"
                        title={t('serverDetail.security.unusedHint')}>{t('serverDetail.security.unusedTag')}</span></div>
                    {/if}
                  {:else}
                    <span class="badge border text-body-secondary">{t('serverDetail.security.sourceOs')}</span>
                  {/if}
                </td>
                <td class="small">
                  {#if v.primary_url}
                    <a href={v.primary_url} target="_blank" rel="noopener noreferrer">{v.cve_id}</a>
                  {:else}{v.cve_id}{/if}
                </td>
                <td class="small">{v.package_name}</td>
                <td class="small">{v.installed_version || '-'}</td>
                <td class="small">{v.fixed_version || '-'}</td>
                <td class="small text-body-secondary">{v.title || '-'}</td>
              </tr>
            {:else}
              <tr><td colspan="7" class="text-body-secondary small">
                {#if vulnReport?.scanner_available}
                  {t('serverDetail.security.noVulns')}
                {:else}
                  {t('serverDetail.security.noData')}
                {/if}
              </td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={vulnPage}
        pageCount={vulnPageCount}
        total={vulnList.length}
        pageSize={VULN_PAGE_SIZE}
        onchange={(p) => (vulnPage = p)}
        label={t('serverDetail.security.pagination')}
      />
    {:else if tab === 'deep-scan'}
      <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
        <h2 class="h6 mb-0 me-2">{t('serverDetail.deepScan.title')}</h2>
        {#if deepScan?.hardening_index != null}
          <span class="badge {deepScan.hardening_index >= 70 ? 'text-bg-success' : deepScan.hardening_index >= 50 ? 'text-bg-warning' : 'text-bg-danger'}" data-testid="hardening-index">
            {t('serverDetail.deepScan.hardening')}: {deepScan.hardening_index}/100
          </span>
        {/if}
        {#if auth.can('servers:write')}
          <button class="btn btn-sm btn-outline-primary ms-auto" disabled={deepScanBusy || jobLocked} onclick={runDeepScan} data-testid="deep-scan-run">
            {#if deepScanBusy}{t('serverDetail.deepScan.running')}{:else}{@html icons.search} {t('serverDetail.deepScan.run')}{/if}
          </button>
          <button class="btn btn-sm btn-outline-secondary" disabled={deepScanBusy || jobLocked} onclick={installDeepScanTools} data-testid="deep-scan-install">
            {@html icons.box} {t('serverDetail.deepScan.installTools')}
          </button>
        {/if}
      </div>
      <p class="small text-body-secondary">{t('serverDetail.deepScan.intro')}
        {#if deepScan?.deep_scan_at}
          <span class="d-block">{t('serverDetail.deepScan.lastScan', { when: lastSeen(deepScan.deep_scan_at) })}</span>
        {:else}
          <span class="d-block">{t('serverDetail.deepScan.noScan')}</span>
        {/if}
      </p>

      {#if deepScan?.kernel_reboot_pending}
        <div class="alert alert-warning small" data-testid="deep-scan-kernel-reboot">{t('serverDetail.deepScan.kernelReboot')}</div>
      {/if}

      <!-- Kernel-CVEs (aus dem zentralen Trivy-Scan, kernel-fokussiert) -->
      <div class="card mb-3"><div class="card-body">
        <h3 class="h6">{t('serverDetail.deepScan.kernelCves')}</h3>
        {#if (deepScan?.kernel_vulns?.length ?? 0) > 0}
          <div class="table-responsive">
            <table class="table table-sm align-middle mb-0">
              <thead><tr><th>{t('serverDetail.security.colSeverity')}</th><th>{t('serverDetail.security.colCve')}</th><th>{t('serverDetail.security.colPackage')}</th><th>{t('serverDetail.security.colFixed')}</th></tr></thead>
              <tbody>
                {#each pageSlice(deepScan.kernel_vulns, kernelVulnPage) as v (v.id)}
                  <tr>
                    <td><span class="badge {severityBadge(v.severity)}">{severityLabel(v.severity)}</span></td>
                    <td class="small">{#if v.primary_url}<a href={v.primary_url} target="_blank" rel="noopener">{v.cve_id} ↗</a>{:else}{v.cve_id}{/if}</td>
                    <td class="small font-monospace">{v.package_name}</td>
                    <td class="small">{v.fixed_version || '-'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <Pagination
            page={kernelVulnPage}
            pageCount={pageCount(deepScan.kernel_vulns.length)}
            total={deepScan.kernel_vulns.length}
            pageSize={PAGE_SIZE}
            testid="kernel-vuln-pagination"
            onchange={(p) => (kernelVulnPage = p)}
          />
        {:else}
          <p class="small text-body-secondary mb-0">{t('serverDetail.deepScan.noKernelCves')}</p>
        {/if}
      </div></div>

      <!-- Berichte: ein datierter Eintrag je Lauf, aufklappbar. Eine einzige
           lange Befundliste ließ nicht erkennen, was neu ist und was man
           bereits gelöst hat - der Vergleich zweier Läufe beantwortet das. -->
      <div class="card"><div class="card-body">
        <h3 class="h6">{t('serverDetail.deepScan.reports')}</h3>
        {#if (deepScan?.reports?.length ?? 0) > 0}
          <p class="small text-body-secondary">{t('serverDetail.deepScan.reportsIntro')}</p>
          <div class="list-group list-group-flush" data-testid="deep-scan-reports">
            {#each deepScan.reports as rep, i (rep.id)}
              <div class="list-group-item px-0">
                <button type="button" class="btn btn-link p-0 text-start text-decoration-none w-100 d-flex flex-wrap gap-2 align-items-center"
                  onclick={() => toggleDeepScanReport(rep.id)} data-testid="deep-scan-report-toggle">
                  <span class="text-body-secondary" aria-hidden="true">{dsOpen[rep.id] ? '▾' : '▸'}</span>
                  <span class="fw-medium text-body">{dsFmtTime(rep.created_at)}</span>
                  {#if i === 0}<span class="badge text-bg-primary">{t('serverDetail.deepScan.latest')}</span>{/if}
                  {#if rep.critical > 0}<span class="badge text-bg-danger">{t('serverDetail.deepScan.sev.critical')}: {rep.critical}</span>{/if}
                  {#if rep.warnings > 0}<span class="badge text-bg-warning">{t('serverDetail.deepScan.sev.warning')}: {rep.warnings}</span>{/if}
                  {#if rep.infos > 0}<span class="badge text-bg-secondary">{t('serverDetail.deepScan.sev.info')}: {rep.infos}</span>{/if}
                  {#if rep.critical + rep.warnings + rep.infos === 0}
                    <span class="badge text-bg-success">{t('serverDetail.deepScan.noFindings')}</span>
                  {/if}
                  {#if rep.new_findings > 0}
                    <span class="badge text-bg-danger-subtle text-danger-emphasis border border-danger-subtle" data-testid="deep-scan-new">
                      +{rep.new_findings} {t('serverDetail.deepScan.newShort')}
                    </span>
                  {/if}
                  {#if rep.resolved_findings > 0}
                    <span class="badge text-bg-success-subtle text-success-emphasis border border-success-subtle" data-testid="deep-scan-resolved">
                      −{rep.resolved_findings} {t('serverDetail.deepScan.resolvedShort')}
                    </span>
                  {/if}
                  {#if rep.hardening_index != null}
                    <span class="badge border text-body-secondary">{t('serverDetail.deepScan.hardening')}: {rep.hardening_index}/100</span>
                  {/if}
                </button>

                {#if dsOpen[rep.id]}
                  <div class="mt-2 ms-3">
                    <p class="small text-body-secondary mb-2">
                      {t('serverDetail.deepScan.tools', { tools: rep.tools || '-' })}
                    </p>
                    {#if resolvedTitles(rep).length > 0}
                      <div class="alert alert-success py-2 px-3 small" data-testid="deep-scan-resolved-list">
                        <strong>{t('serverDetail.deepScan.resolvedSince')}</strong>
                        <ul class="mb-0 mt-1">
                          {#each resolvedTitles(rep) as title (title)}<li>{title}</li>{/each}
                        </ul>
                      </div>
                    {/if}
                    {#if dsDetail[rep.id]}
                      {#each groupByCategory(dsDetail[rep.id].findings) as [cat, list] (cat)}
                        <h4 class="h6 small text-body-secondary mt-3 mb-1">
                          {t('serverDetail.deepScan.cat.' + cat)} <span class="fw-normal">({list.length})</span>
                        </h4>
                        <ul class="list-group list-group-flush">
                          {#each list as f (f.id)}
                            <li class="list-group-item px-0 py-1 border-0">
                              <span class="badge {dsBadgeClass(f.severity)} me-2">{t('serverDetail.deepScan.sev.' + f.severity)}</span>
                              {#if f.is_new}
                                <span class="badge text-bg-danger-subtle text-danger-emphasis border border-danger-subtle me-2">{t('serverDetail.deepScan.new')}</span>
                              {/if}
                              {f.title}
                              {#if f.detail}<div class="small text-body-secondary ms-1">{f.detail}</div>{/if}
                            </li>
                          {/each}
                        </ul>
                      {:else}
                        <p class="small text-body-secondary mb-0">{t('serverDetail.deepScan.noFindings')}</p>
                      {/each}
                    {:else}
                      <p class="small text-body-secondary mb-0">{t('common.loading')}</p>
                    {/if}
                  </div>
                {/if}
              </div>
            {/each}
          </div>
        {:else}
          <p class="small text-body-secondary mb-0">{t('serverDetail.deepScan.noReports')}</p>
        {/if}
      </div></div>
    {:else if tab === 'apps'}
      <p class="small text-body-secondary">
        {t('serverDetail.apps.intro')}
        {#if auth.can('settings:manage')}<a href="/settings/apps" use:link>{t('serverDetail.apps.catalogLink')}</a>{/if}
      </p>
      {#if !apps}
        <div class="alert alert-info small mb-0">{t('common.loading')}</div>
      {:else}
        <h3 class="h6">{t('serverDetail.apps.detectedTitle')}</h3>
        {#if apps.detected.length === 0}
          <div class="alert alert-secondary small" data-testid="apps-none">{t('serverDetail.apps.detectedEmpty')}</div>
        {:else}
          <div class="table-responsive mb-4">
            <table class="table table-sm align-middle" data-testid="apps-table">
              <thead>
                <tr>
                  <th>{t('serverDetail.apps.colName')}</th>
                  <th>{t('serverDetail.apps.colPath')}</th>
                  <th>{t('serverDetail.apps.colVersion')}</th>
                  <th>{t('serverDetail.apps.colLatest')}</th>
                  {#if auth.can('servers:write')}<th class="text-end">{t('serverDetail.apps.colActions')}</th>{/if}
                </tr>
              </thead>
              <tbody>
                {#each pageSlice(apps.detected, appPage) as a (a.id)}
                  <tr class={a.update_available ? 'table-warning' : ''}>
                    <td>
                      <strong>{i18n.field(a, 'name')}</strong>
                      {#if i18n.field(a, 'description')}<div class="small text-body-secondary">{i18n.field(a, 'description')}</div>{/if}
                    </td>
                    <td><code class="small">{a.path}</code></td>
                    <td>{a.version || '-'}</td>
                    <td>
                      {#if a.update_available}
                        <span class="badge bg-warning text-dark">{a.latest_version}</span>
                      {:else if a.latest_version}
                        <span class="text-body-secondary small">{a.latest_version}</span>
                      {:else}
                        <span class="text-body-secondary small" title={t('serverDetail.apps.noSourceHint')}>-</span>
                      {/if}
                    </td>
                    {#if auth.can('servers:write')}
                      <td class="text-end text-nowrap">
                        {#if a.can_backup}
                          <button class="btn btn-sm btn-outline-secondary py-0" disabled={busy || jobLocked}
                            onclick={() => runAppAction(a, 'backup')}>{t('serverDetail.apps.backup')}</button>
                        {/if}
                        {#if a.can_update}
                          <button class="btn btn-sm {a.update_available ? 'btn-warning' : 'btn-outline-secondary'} py-0"
                            disabled={busy || jobLocked} onclick={() => runAppAction(a, 'update')}>{t('serverDetail.apps.update')}</button>
                        {:else}
                          <span class="small text-body-secondary" title={t('serverDetail.apps.noActionHint')}>-</span>
                        {/if}
                      </td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <Pagination
            page={appPage}
            pageCount={pageCount(apps.detected.length)}
            total={apps.detected.length}
            pageSize={PAGE_SIZE}
            testid="app-pagination"
            onchange={(p) => (appPage = p)}
          />
        {/if}

        <h3 class="h6">{t('serverDetail.apps.unknownTitle')}</h3>
        <p class="small text-body-secondary">{t('serverDetail.apps.unknownIntro')}</p>
        {#if apps.unknown.length === 0}
          <div class="alert alert-secondary small mb-0">{t('serverDetail.apps.unknownEmpty')}</div>
        {:else}
          <div class="table-responsive">
            <table class="table table-sm align-middle" data-testid="apps-unknown-table">
              <thead>
                <tr>
                  <th>{t('serverDetail.apps.colUnit')}</th>
                  <th>{t('serverDetail.apps.colExec')}</th>
                  <th>{t('serverDetail.apps.colFragment')}</th>
                </tr>
              </thead>
              <tbody>
                {#each pageSlice(apps.unknown, unknownAppPage) as u (u.id)}
                  <tr>
                    <td><code class="small">{u.unit}</code></td>
                    <td><code class="small">{u.exec_path || '-'}</code></td>
                    <td class="small text-body-secondary">{u.fragment_path}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <Pagination
            page={unknownAppPage}
            pageCount={pageCount(apps.unknown.length)}
            total={apps.unknown.length}
            pageSize={PAGE_SIZE}
            testid="unknown-app-pagination"
            onchange={(p) => (unknownAppPage = p)}
          />
        {/if}
      {/if}
    {:else if tab === 'storage'}
      <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
        <h2 class="h6 mb-0 me-2">{t('serverDetail.storage.title')}</h2>
        {#if storage}
          <span class="badge {storage.current_percent >= 85 ? 'bg-warning text-dark' : 'border text-body-secondary'}">
            {t('serverDetail.storage.current', { pct: storage.current_percent, used: fmtSize(storage.current_used_mb), total: fmtSize(storage.current_total_mb) })}
          </span>
          {#if storage.forecast?.insufficient_data}
            <span class="badge text-bg-secondary">{t('serverDetail.storage.forecastInsufficient')}</span>
          {:else if storage.forecast?.unlimited}
            <span class="badge text-bg-success">{t('serverDetail.storage.forecastUnlimited')}</span>
          {:else if storage.forecast}
            <span class="badge {storage.forecast.days_remaining <= 30 ? 'bg-danger' : storage.forecast.days_remaining <= 90 ? 'bg-warning text-dark' : 'text-bg-info'}">
              {t('serverDetail.storage.forecastDays', { days: storage.forecast.days_remaining })}
            </span>
          {/if}
        {/if}
      </div>
      <p class="small text-body-secondary">
        {t('serverDetail.storage.introA')}<strong>{t('serverDetail.storage.dailyAvgBold')}</strong>{t('serverDetail.storage.introB')}
      </p>
      {#if storageChart}
        <div class="card"><div class="card-body">
          <svg viewBox="0 0 {CHART.w} {CHART.h}" class="w-100" style="max-height: 300px" role="img"
            aria-label={t('serverDetail.storage.chartAria')}
            onmousemove={chartHover} onmouseleave={() => (hoverPt = null)}>
            <!-- Gitternetz + Y-Achsen-Beschriftung (0/25/50/75/100 %) -->
            {#each [0, 25, 50, 75, 100] as g}
              {@const gy = CHART.padT + (CHART.h - CHART.padT - CHART.padB) * (1 - g / 100)}
              <line x1={CHART.padL} y1={gy} x2={CHART.w - CHART.padR} y2={gy} stroke="currentColor" stroke-opacity="0.12" />
              <text x={CHART.padL - 6} y={gy + 3} text-anchor="end" font-size="11" fill="currentColor" fill-opacity="0.6">{g}%</text>
            {/each}
            <!-- Warnschwelle 85% -->
            <line x1={CHART.padL} y1={storageChart.warnY} x2={CHART.w - CHART.padR} y2={storageChart.warnY}
              stroke="var(--bs-warning, #ffc107)" stroke-width="1.5" stroke-dasharray="5 4" />
            <!-- Fläche + Linie -->
            <path d={storageChart.area} fill="var(--bs-primary, #0d6efd)" fill-opacity="0.14" />
            <path d={storageChart.line} fill="none" stroke="var(--bs-primary, #0d6efd)" stroke-width="2" />
            <!-- Endpunkt hervorheben -->
            <circle cx={storageChart.lastPt.x} cy={storageChart.lastPt.y} r="3.5" fill="var(--bs-primary, #0d6efd)" />
            <!-- X-Achse (erster/letzter Tag) -->
            <text x={CHART.padL} y={CHART.h - 8} font-size="11" fill="currentColor" fill-opacity="0.6">{storageChart.first}</text>
            <text x={CHART.w - CHART.padR} y={CHART.h - 8} text-anchor="end" font-size="11" fill="currentColor" fill-opacity="0.6">{storageChart.last}</text>
            <!-- Hover: Führungslinie + Punkt + Tooltip mit Prozent/Tag -->
            {#if hoverPt && hoverBox}
              <line x1={hoverPt.x} y1={CHART.padT} x2={hoverPt.x} y2={storageChart.baseY} stroke="currentColor" stroke-opacity="0.25" stroke-dasharray="3 3" />
              <circle cx={hoverPt.x} cy={hoverPt.y} r="4.5" fill="var(--bs-primary, #0d6efd)" stroke="var(--bs-body-bg, #fff)" stroke-width="1.5" />
              <g transform="translate({hoverBox.x}, {hoverBox.y})">
                <rect width={hoverBox.w} height="24" rx="4" fill="var(--bs-dark, #212529)" fill-opacity="0.9" />
                <text x={hoverBox.w / 2} y="16" text-anchor="middle" font-size="12" fill="#fff" data-testid="chart-hover-label">{hoverBox.label}</text>
              </g>
            {/if}
          </svg>
          <div class="small text-body-secondary mt-2">
            {t('serverDetail.storage.captured', { days: storage.history.length, peak: storageChart.peak.toFixed(0) })}
            {#if storage.forecast && !storage.forecast.insufficient_data}
              ·
              {#if storage.forecast.unlimited}
                {t('serverDetail.storage.trendFlat')}
              {:else}
                {t('serverDetail.storage.trendLinearA')}<strong>{t('serverDetail.storage.trendLinearDays', { days: storage.forecast.days_remaining })}</strong>
              {/if}
            {/if}
          </div>
        </div></div>
      {:else}
        <div class="alert alert-info small">
          {t('serverDetail.storage.empty')}
        </div>
      {/if}

      <!-- Eingehängte Dateisysteme (Volumes): aktuelle Belegung je Volume. -->
      <h2 class="h6 mt-4 mb-1">{t('serverDetail.storage.volumesTitle')}</h2>
      <p class="small text-body-secondary">{t('serverDetail.storage.volumesHint')}</p>
      {#if (storage?.volumes?.length ?? 0) > 0}
        <div class="table-responsive">
          <table class="table table-sm align-middle" data-testid="volumes-table">
            <thead><tr>
              <th>{t('serverDetail.storage.colMount')}</th>
              <th>{t('serverDetail.storage.colDevice')}</th>
              <th>{t('serverDetail.storage.colFstype')}</th>
              <th style="min-width: 220px">{t('serverDetail.storage.colUsage')}</th>
            </tr></thead>
            <tbody>
              {#each storage.volumes as v (v.id)}
                {@const pct = v.total_mb > 0 ? Math.round((v.used_mb * 100) / v.total_mb) : 0}
                <tr>
                  <td>
                    <code class="small">{v.mountpoint}</code>
                    {#if v.mountpoint === '/'}<span class="badge text-bg-secondary ms-1">{t('serverDetail.storage.rootBadge')}</span>{/if}
                  </td>
                  <td class="small text-body-secondary">{v.device}</td>
                  <td class="small text-body-secondary">{v.fstype}</td>
                  <td>
                    <div class="d-flex align-items-center gap-2">
                      <div class="progress flex-grow-1" style="height: 8px; min-width: 90px">
                        <div class="progress-bar {pct >= 90 ? 'bg-danger' : pct >= 85 ? 'bg-warning' : 'bg-success'}" style="width: {pct}%"></div>
                      </div>
                      <span class="small text-nowrap">{fmtSize(v.used_mb)} / {fmtSize(v.total_mb)} ({pct}%)</span>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <div class="alert alert-info small">{t('serverDetail.storage.volumesEmpty')}</div>
      {/if}
    {:else if tab === 'repos'}
      {#if auth.can('servers:write')}
        <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
          <span class="badge border text-body-secondary" title={t('serverDetail.repos.pkgMgrHint')}>{pkgMgrLabel(server.package_manager)}</span>
          {#if insecureRepos > 0}
            <button class="btn btn-sm btn-warning" disabled={busy || jobLocked} onclick={secureRepos}>
              {@html icons.lock} {insecureRepos === 1 ? t('serverDetail.repos.secureOne', { count: insecureRepos }) : t('serverDetail.repos.secureMany', { count: insecureRepos })}
            </button>
          {:else}
            <span class="badge text-bg-success">{t('serverDetail.repos.allSecure')}</span>
          {/if}
          {#if revertUrls.length > 0}
            <button class="btn btn-sm btn-outline-secondary" disabled={busy || jobLocked}
              data-testid="revert-https" title={t('serverDetail.repos.revertTitle')} onclick={revertRepos}>
              {revertUrls.length === 1
                ? t('serverDetail.repos.revertOne', { count: revertUrls.length })
                : t('serverDetail.repos.revertMany', { count: revertUrls.length })}
            </button>
          {/if}
          <!-- APT-Cache (apt-cacher-ng) ist apt-spezifisch → nur auf apt-Systemen. -->
          {#if isApt}
            {#if server.apt_proxy_active}
              <span class="badge text-bg-info">{t('serverDetail.repos.aptCacheActive')}</span>
              <button class="btn btn-sm btn-outline-danger" disabled={busy || jobLocked}
                title={t('serverDetail.repos.aptDisconnectTitle')}
                onclick={() => toggleAptProxy(false)}>{t('serverDetail.repos.aptDisconnect')}</button>
            {:else}
              <button class="btn btn-sm btn-outline-primary" disabled={busy || jobLocked}
                title={t('serverDetail.repos.aptConnectTitle')}
                onclick={() => toggleAptProxy(true)}>{@html icons.box} {t('serverDetail.repos.aptConnect')}</button>
            {/if}
          {/if}
          {#if isProxmox}
            <span class="badge border text-body-secondary ms-auto" title={proxmoxHint}>
              {@html icons.lock} {t('serverDetail.repos.proxmoxManaged', { name: proxmoxName })}
            </span>
          {:else if serverKnownRepos.length > 0}
            <div class="input-group input-group-sm ms-auto" style="max-width: 420px">
              <select class="form-select" bind:value={addRepoKey} disabled={busy}>
                <option value="">{t('serverDetail.repos.addKnownOption')}</option>
                {#each serverKnownRepos as kr (kr.key)}<option value={kr.key}>{kr.name}</option>{/each}
              </select>
              <button class="btn btn-primary" onclick={addRepo} disabled={!addRepoKey || busy || jobLocked}>{t('serverDetail.actions.add')}</button>
            </div>
          {:else}
            <span class="small text-body-secondary ms-auto" title={t('serverDetail.repos.noCatalogHint')}>
              {t('serverDetail.repos.noCatalogForPkgMgr', { mgr: pkgMgrLabel(server.package_manager) })}
            </span>
          {/if}
        </div>
        {#if addRepoKey}
          {@const kr = serverKnownRepos.find((r) => r.key === addRepoKey)}
          {#if kr}<p class="small text-body-secondary">{kr.description}{t('serverDetail.repos.repoDescA')}<code>{kr.key_url}</code>{t('serverDetail.repos.repoDescB')}</p>{/if}
        {/if}
      {/if}
      <ul class="list-group">
        {#each repos as r (r.id)}
          <li class="list-group-item d-flex justify-content-between align-items-center">
            <code class="small">{r.line}</code>
            {#if r.insecure}<span class="badge bg-danger">{t('serverDetail.repos.insecure')}</span>{/if}
          </li>
        {:else}
          <li class="list-group-item text-body-secondary small">{t('serverDetail.repos.noRepos')}</li>
        {/each}
      </ul>
      {#if repoOutput}
        <div class="card mt-3"><div class="card-body py-2">
          <h3 class="h6 mb-1">{t('serverDetail.repos.consoleOutput')}</h3>
          <pre class="small mb-0" style="max-height: 260px; overflow: auto">{repoOutput}</pre>
        </div></div>
      {/if}
    {:else if tab === 'jobs'}
      <div class="d-flex justify-content-end mb-2">
        <div class="form-check form-switch text-nowrap">
          <input class="form-check-input" type="checkbox" id="hide-health-jobs" bind:checked={hideHealth} />
          <label class="form-check-label small" for="hide-health-jobs">{t('serverDetail.jobs.hideHealth')}</label>
        </div>
      </div>
      <div class="table-responsive">
        <table class="table table-sm">
          <thead><tr><th>{t('serverDetail.jobs.colJob')}</th><th>{t('common.status')}</th><th>{t('serverDetail.jobs.colTime')}</th></tr></thead>
          <tbody>
            {#each pageSlice(visibleJobs, jobPage) as j (j.id)}
              <tr>
                <td>{j.name}</td>
                <td><span class="badge {j.status === 'success' ? 'bg-success' : j.status === 'failed' ? 'bg-danger' : 'bg-secondary'}">{j.status}</span></td>
                <td class="small text-body-secondary">{j.started_at ? new Date(j.started_at).toLocaleString() : '-'}</td>
              </tr>
            {:else}
              <tr><td colspan="3" class="text-body-secondary small">{t('serverDetail.jobs.noJobs')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
      <Pagination
        page={jobPage}
        pageCount={pageCount(visibleJobs.length)}
        total={visibleJobs.length}
        pageSize={PAGE_SIZE}
        testid="job-pagination"
        onchange={(p) => (jobPage = p)}
      />
      <a href="/jobs" use:link class="small">{t('serverDetail.jobs.fullHistory')}</a>
    {:else if tab === 'logs'}
      <div class="d-flex justify-content-between align-items-center mb-2">
        <p class="small text-body-secondary mb-0">
          {t('serverDetail.logs.intro')}
        </p>
        <div class="form-check form-switch text-nowrap ms-3">
          <input class="form-check-input" type="checkbox" id="hide-health" bind:checked={hideHealth} />
          <label class="form-check-label small" for="hide-health">{t('serverDetail.jobs.hideHealth')}</label>
        </div>
      </div>
      <SshSessions sessions={visibleSessions} loadCommands={(sid) => api.servers.sshSession(sid)} />
    {/if}

    {#if auth.can('servers:write')}
      <!-- Einstellungen als Modal (per Zahnrad-Schaltfläche geöffnet). -->
      <Modal title={t('serverDetail.actions.settings')} bind:open={settingsOpen}>
        <div class="mb-4">
          <h3 class="h6">{t('serverDetail.sshProtect.title')}</h3>
          <p class="small text-body-secondary">{t('serverDetail.sshProtect.intro')}</p>

          <!-- Root-Login sperren -->
          <div class="form-check form-switch mb-1">
            <input class="form-check-input" type="checkbox" id="ssh-rootlogin" role="switch"
              checked={rootLoginDisabled} disabled={busy || jobLocked}
              data-testid="ssh-rootlogin-toggle"
              onchange={() => action(
                () => api.servers.setSshRootLogin(id, !rootLoginDisabled),
                t('serverDetail.sshProtect.rootLoginChanged'),
              )} />
            <label class="form-check-label" for="ssh-rootlogin">{t('serverDetail.sshProtect.rootLoginLabel')}</label>
          </div>
          <div class="form-text mb-3">{t('serverDetail.sshProtect.rootLoginHint')}</div>

          <!-- Eigener SSH-Port -->
          <label class="form-label mb-1" for="ssh-port">{t('serverDetail.sshProtect.portLabel')}</label>
          <div class="input-group input-group-sm" style="max-width: 260px">
            <input id="ssh-port" type="number" min="1" max="65535" class="form-control"
              bind:value={portInput} disabled={busy || jobLocked} data-testid="ssh-port-input" />
            <button class="btn btn-outline-primary" onclick={changePort} data-testid="ssh-port-apply"
              disabled={busy || jobLocked || Number(portInput) === server.ssh_port || !portInput}>
              {t('serverDetail.sshProtect.portApply')}
            </button>
          </div>
          <div class="form-text">{t('serverDetail.sshProtect.portHint')}</div>
        </div>

        {#if !isProxmox}
          <div class="border-top pt-3">
            <h3 class="h6">{t('serverDetail.userSync.label')}</h3>
            <div class="form-check form-switch mb-1">
              <input class="form-check-input" type="checkbox" id="user-sync-off" role="switch"
                checked={userSyncDisabled} disabled={busy || !auth.can('servers:write')}
                data-testid="user-sync-toggle"
                onchange={() => action(
                  () => api.servers.updateSettings(id, { user_sync_disabled: !userSyncDisabled }),
                  t('serverDetail.userSync.toggled'),
                )} />
              <label class="form-check-label" for="user-sync-off">{t('serverDetail.userSync.disableLabel')}</label>
            </div>
            <div class="form-text">{t('serverDetail.userSync.hint')}</div>
          </div>
        {/if}

        {#if server.has_docker}
          <div class="border-top pt-3" data-testid="docker-settings">
            <h3 class="h6">{t('serverDetail.dockerPolicy.title')}</h3>
            <div class="form-check form-switch mb-1">
              <input class="form-check-input" type="checkbox" id="docker-updates-off" role="switch"
                checked={dockerUpdatesDisabled} disabled={busy || !auth.can('servers:write')}
                data-testid="docker-updates-toggle"
                onchange={() => action(
                  () => api.servers.updateSettings(id, { docker_updates_disabled: !dockerUpdatesDisabled }),
                  t('serverDetail.dockerPolicy.updatesToggled'),
                )} />
              <label class="form-check-label" for="docker-updates-off">{t('serverDetail.dockerPolicy.updatesLabel')}</label>
            </div>
            <div class="form-text mb-3">{t('serverDetail.dockerPolicy.updatesHint')}</div>

            <div class="form-check form-switch mb-1">
              <input class="form-check-input" type="checkbox" id="docker-cves-off" role="switch"
                checked={dockerCVEsIgnored} disabled={busy || !auth.can('servers:write')}
                data-testid="docker-cves-toggle"
                onchange={() => action(
                  () => api.servers.updateSettings(id, { docker_cves_ignored: !dockerCVEsIgnored }),
                  t('serverDetail.dockerPolicy.cvesToggled'),
                )} />
              <label class="form-check-label" for="docker-cves-off">{t('serverDetail.dockerPolicy.cvesLabel')}</label>
            </div>
            <div class="form-text">{t('serverDetail.dockerPolicy.cvesHint')}</div>
            {#if dockerCVEsIgnored}
              <!-- Was der Schalter zusätzlich abschaltet, gehört gesagt: sonst
                   fällt die automatische Bewertung erreichbarer Container
                   stillschweigend weg. -->
              <div class="alert alert-warning py-2 px-3 small mt-2 mb-0" role="note">
                {t('serverDetail.dockerPolicy.cvesOverrideNote')}
              </div>
            {/if}
          </div>
        {/if}

        <div class="border-top pt-3">
          <h3 class="h6">{t('serverDetail.availability.title')}</h3>
          <div class="form-check form-switch mb-1">
            <input class="form-check-input" type="checkbox" id="unreach-uncritical" role="switch"
              checked={unreachableUncritical} disabled={busy || !auth.can('servers:write')}
              data-testid="unreachable-uncritical-toggle"
              onchange={() => action(
                () => api.servers.updateSettings(id, { unreachable_uncritical: !unreachableUncritical }),
                t('serverDetail.availability.toggled'),
              )} />
            <label class="form-check-label" for="unreach-uncritical">{t('serverDetail.availability.label')}</label>
          </div>
          <div class="form-text mb-2">{t('serverDetail.availability.hint')}</div>
          {#if unreachableUncritical}
            <label class="form-label mb-1" for="grace-days">{t('serverDetail.availability.graceLabel')}</label>
            <div class="input-group input-group-sm" style="max-width: 18rem;">
              <input id="grace-days" type="number" min="1" max="365" class="form-control"
                bind:value={graceDaysInput} disabled={busy || !auth.can('servers:write')} />
              <span class="input-group-text">{t('serverDetail.availability.days')}</span>
              <button class="btn btn-outline-primary" data-testid="grace-days-save"
                disabled={busy || !auth.can('servers:write') || !graceValid(graceDaysInput) || Number(graceDaysInput) === server.unreachable_grace_days}
                onclick={() => action(
                  () => api.servers.updateSettings(id, { unreachable_grace_days: Number(graceDaysInput) }),
                  t('serverDetail.availability.graceSaved'),
                )}>{t('common.save')}</button>
            </div>
            <div class="form-text">{t('serverDetail.availability.graceHint', { days: server.unreachable_grace_days || 28 })}</div>
          {/if}
        </div>

        <div class="mb-1">
          <h3 class="h6">{t('serverDetail.dns.settingsTitle')}</h3>
          <div class="form-text mb-2">{t('serverDetail.dns.settingsHint')}</div>
          <datalist id="dns-presets-list">
            {#each dnsPresets as p (p.ip)}
              <option value={p.ip}>{p.label}</option>
            {/each}
          </datalist>
          {#each dnsInputs as _v, i (i)}
            <input class="form-control form-control-sm mb-2 font-monospace" list="dns-presets-list"
              placeholder={t('serverDetail.dns.serverPlaceholder', { n: i + 1 })}
              bind:value={dnsInputs[i]} disabled={busy || !auth.can('servers:write')}
              data-testid={'dns-input-' + i} />
          {/each}
          <button class="btn btn-outline-primary btn-sm" data-testid="dns-apply"
            disabled={busy || !auth.can('servers:write')} onclick={applyDNS}>{t('serverDetail.dns.applyLabel')}</button>
        </div>

        <hr />
        <div class="mb-1" data-testid="time-settings">
          <h3 class="h6">{t('serverDetail.time.title')}</h3>
          <div class="form-text mb-2">{t('serverDetail.time.intro')}</div>
          <dl class="row small mb-2">
            <dt class="col-5">{t('serverDetail.time.offset')}</dt>
            <dd class="col-7">
              {#if server.time_checked_at}
                <span class="badge {clockBadgeClass(server)}">{clockLabel(server)}</span>
              {:else}-{/if}
            </dd>
            <dt class="col-5">{t('serverDetail.time.ntpService')}</dt>
            <dd class="col-7">{server.ntp_service || '-'}{server.ntp_synchronized ? ' ✓' : ''}</dd>
            <dt class="col-5">{t('serverDetail.time.ntpServers')}</dt>
            <dd class="col-7 font-monospace">{server.ntp_servers || '-'}</dd>
          </dl>
          <button class="btn btn-outline-secondary btn-sm mb-3" data-testid="time-check"
            disabled={busy} onclick={checkTime}>{@html icons.clock} {t('serverDetail.time.checkLabel')}</button>

          <label class="form-label mb-1" for="tz-input">{t('serverDetail.time.timezone')}</label>
          <div class="input-group input-group-sm mb-3" style="max-width: 26rem;">
            <input id="tz-input" class="form-control font-monospace" placeholder="Europe/Berlin"
              bind:value={tzInput} disabled={busy || !auth.can('servers:write')} data-testid="tz-input" />
            <button class="btn btn-outline-primary" data-testid="tz-apply"
              disabled={busy || !auth.can('servers:write') || !tzInput}
              onclick={() => applyTimezone(tzInput)}>{t('serverDetail.time.applyTimezone')}</button>
          </div>

          {#if isContainer}
            <!-- Kein Zeitserver-Formular für Container: die Uhr kommt vom Host,
                 ein Zeitdienst startet dort gar nicht. Ein Eingabefeld
                 anzubieten hieße, zu etwas Unmöglichem aufzufordern. -->
            <div class="alert alert-secondary py-2 px-3 small mb-0" data-testid="time-container-note">
              {t('serverDetail.time.containerNote', { type: server.virtualization })}
            </div>
          {:else}
            <label class="form-label mb-1" for="ntp-0">{t('serverDetail.time.ntpServers')}</label>
            <datalist id="ntp-presets-list">
              {#each ntpPresets as p (p.ip)}<option value={p.ip}>{p.label}</option>{/each}
            </datalist>
            {#each [0, 1, 2, 3] as i (i)}
              <input id={'ntp-' + i} class="form-control form-control-sm mb-2 font-monospace" list="ntp-presets-list"
                placeholder={t('serverDetail.time.ntpServers') + ' ' + (i + 1)}
                value={ntpInput[i] ?? ''} oninput={(e) => (ntpInput[i] = e.currentTarget.value)}
                disabled={busy || !auth.can('servers:write')} data-testid={'ntp-input-' + i} />
            {/each}
            <button class="btn btn-outline-primary btn-sm" data-testid="ntp-apply"
              disabled={busy || !auth.can('servers:write') || ntpInput.filter(Boolean).length === 0}
              onclick={() => applyNTP(ntpInput.filter(Boolean))}>{t('serverDetail.time.applyNtp')}</button>
          {/if}
        </div>
      </Modal>
    {/if}

    <ReconnectWizard {server} bind:open={reconnectOpen} onDone={() => { toasts.success(t('serverDetail.notices.reconnected')); load(); }} />

    <!-- modal-xl: Mit Quellen- und Bemerkungs-Spalte wird die Regeltabelle in
         der lg-Breite zu eng (IP-Version und Bind-Adresse beschneiden). -->
    <Modal title={t('serverDetail.firewall.title')} bind:open={firewallOpen} size="modal-xl">
      <p class="small text-body-secondary">
        {t('serverDetail.firewall.introA')}<strong>{server.name}</strong>{t('serverDetail.firewall.introB', { port: server.ssh_port })}
      </p>
      <!-- Backend-Badge: welches Werkzeug diese Distribution nutzt; Hinweis,
           wenn es erst installiert werden muss. -->
      <div class="mb-2 small" data-testid="fw-backend">
        <span class="text-body-secondary">{t('serverDetail.firewall.backend')}:</span>
        <span class="badge text-bg-secondary">{firewallBackend}</span>
        {#if !server.firewall_tool}
          <span class="text-body-secondary ms-1">{t('serverDetail.firewall.willInstall')}</span>
        {/if}
      </div>
      <div class="mb-3">
        <!-- Der Hinweis zu Docker-Ports steht im Editor selbst, direkt bei den
             betroffenen Ports (siehe firewall.editor.dockerBypass). Der frühere
             Zusatz an dieser Stelle empfahl das Gegenteil - eine Freigabe
             „der Konsistenz halber" -, was der Sache widerspricht: die Regel
             wirkt auf einen von Docker veröffentlichten Port gar nicht. -->
        <FirewallRulesEditor bind:this={firewallEditor} bind:rules={firewallRules}
          bind:sshSources={firewallSSHSources} showSSH lcmSourceIP={server.lcm_source_ip ?? ''}
          listening={listeningPorts} dockerPorts={dockerPublishedPorts} allowlists={ipAllowlists} sshPort={server.ssh_port}
          onRescan={rescanListeningPorts} rescanBusy={listeningScanBusy} disabled={busy || jobLocked} />
      </div>
      <div class="d-flex gap-2 align-items-center">
        <button class="btn btn-primary" data-testid="fw-apply" onclick={() => applyFirewall(true)} disabled={busy || jobLocked}>
          {server.firewall_active ? t('serverDetail.firewall.reapply') : t('serverDetail.firewall.enable')}
        </button>
        {#if server.firewall_active}
          <button class="btn btn-outline-danger" onclick={() => applyFirewall(false)} disabled={busy || jobLocked}>{t('serverDetail.firewall.disable')}</button>
        {/if}
        <span class="ms-auto small">{t('common.status')}: <strong>{server.firewall_active ? t('serverDetail.firewall.active') : t('serverDetail.firewall.inactive')}</strong></span>
      </div>
    </Modal>

    <Modal title={t('serverDetail.securityTool.title')} bind:open={securityToolOpen}>
      <p class="small text-body-secondary">{t('serverDetail.securityTool.intro')}</p>
      <!-- Schritt 1: Tool wählen -->
      <div class="mb-3">
        <label class="form-label small mb-1" for="sec-tool">{t('serverDetail.securityTool.chooseTool')}</label>
        <select id="sec-tool" class="form-select" bind:value={securityTool} data-testid="sec-tool">
          <option value="">{t('serverDetail.securityTool.choosePlaceholder')}</option>
          <!-- Bereits Installiertes bleibt wählbar-aussehend, aber gesperrt:
               Ein zweiter Installationslauf hilft nicht, und die Verwaltung
               steht ohnehin als eigene Karte im Sicherheits-Reiter. Bisher
               galt das nur für SSH-2FA - fail2ban und CrowdSec boten sich
               unverändert zur Installation an, obwohl sie schon liefen. -->
          <option value="fail2ban" disabled={server.fail2ban_installed}>
            fail2ban{#if server.fail2ban_installed} - {t('serverDetail.securityTool.alreadyInstalled')}{/if}
          </option>
          <option value="crowdsec" disabled={server.crowdsec_installed}>
            CrowdSec{#if server.crowdsec_installed} - {t('serverDetail.securityTool.alreadyInstalled')}{/if}
          </option>
          <option value="ssh-2fa" disabled={server.ssh_2fa_enabled}>
            {t('serverDetail.ssh2fa.title')}{#if server.ssh_2fa_enabled} - {t('serverDetail.securityTool.alreadyInstalled')}{/if}
          </option>
        </select>
      </div>

      {#if securityTool === 'ssh-2fa'}
        <!-- SSH-2FA kennt weder Allowlist noch Dienst-Optionen: Es
             installiert das PAM-Modul und schreibt das sshd-Drop-in. -->
        <p class="small text-body-secondary" data-testid="sec-ssh2fa-intro">{t('serverDetail.ssh2fa.intro')}</p>
        <div class="alert alert-warning py-2 small">{t('serverDetail.ssh2fa.installWarn')}</div>
      {:else if securityTool}
        <!-- Allowlist (LCM-IP vorbelegt) + benannte Allowlists aus dem Pool -->
        <div class="mb-3">
          <label class="form-label small mb-1" for="sec-allow">{t('serverDetail.securityTool.allowlist')}</label>
          <input id="sec-allow" class="form-control font-monospace" bind:value={secAllowlist}
            placeholder="1.2.3.4 5.6.7.8" data-testid="sec-allowlist" />
          <div class="form-text">{t('serverDetail.securityTool.allowlistHint')}</div>
          {#if ipAllowlists.length > 0}
            <div class="mt-2 small" data-testid="sec-allowlist-lists">
              <span class="text-body-secondary">{t('serverDetail.securityTool.allowlistLists')}</span>
              <div class="d-flex flex-wrap gap-3 mt-1">
                {#each ipAllowlists as a (a.id)}
                  <label class="d-flex align-items-center gap-1">
                    <input type="checkbox" class="form-check-input mt-0" checked={secAllowlistIds.includes(a.id)} onchange={() => toggleSecAllowlist(a.id)} />
                    <span>{a.name}</span>
                  </label>
                {/each}
              </div>
            </div>
          {/if}
        </div>
      {/if}

      {#if securityTool === 'crowdsec'}
        <div class="form-check form-switch mb-2">
          <input class="form-check-input" type="checkbox" role="switch" id="sec-bouncer" bind:checked={secBouncer} />
          <label class="form-check-label" for="sec-bouncer">{t('serverDetail.securityTool.bouncer')}</label>
        </div>
        <div class="mb-3">
          <label class="form-label small mb-1" for="sec-coll">{t('serverDetail.securityTool.collections')}</label>
          <input id="sec-coll" class="form-control font-monospace" bind:value={secCollections} placeholder="crowdsecurity/sshd" />
        </div>
        <div class="mb-3">
          <label class="form-label small mb-1" for="sec-lapi">{t('serverDetail.securityTool.lapiMode')}</label>
          <select id="sec-lapi" class="form-select" bind:value={secLapiMode} data-testid="sec-lapi">
            <option value="local">{t('serverDetail.securityTool.lapiLocal')}</option>
            <option value="remote" disabled={!secSettings?.crowdsec_lapi_configured}>{t('serverDetail.securityTool.lapiRemote')}{#if !secSettings?.crowdsec_lapi_configured} - {t('serverDetail.securityTool.notConfigured')}{/if}</option>
            <option value="console" disabled={!secSettings?.crowdsec_console_configured}>{t('serverDetail.securityTool.lapiConsole')}{#if !secSettings?.crowdsec_console_configured} - {t('serverDetail.securityTool.notConfigured')}{/if}</option>
          </select>
          <div class="form-text">{t('serverDetail.securityTool.lapiHint')}</div>
        </div>
      {/if}

      <div class="d-flex justify-content-end gap-2">
        <button class="btn btn-outline-secondary" onclick={() => (securityToolOpen = false)}>{t('common.cancel')}</button>
        <button class="btn btn-primary" disabled={!securityTool || busy || jobLocked} onclick={applySecurityTool} data-testid="sec-install">
          {t('serverDetail.securityTool.install')}
        </button>
      </div>
    </Modal>

    <Modal title={t('serverDetail.remove.title')} bind:open={removeOpen}>
      <p>
        <strong>{server.name}</strong>{t('serverDetail.remove.introA')}
      </p>
      {#if isAgent}
        <div class="alert alert-info py-2 small mb-3">
          {t('serverDetail.agent.removeHint')}
        </div>
      {:else}
      <div class="form-check mb-2">
        <input class="form-check-input" type="checkbox" id="rm-purge" bind:checked={removePurge} />
        <label class="form-check-label" for="rm-purge">
          {t('serverDetail.remove.purgeA')}<strong>{t('serverDetail.remove.purgeBold')}</strong>{t('serverDetail.remove.purgeB')}
        </label>
      </div>
      {/if}
      {#if isAgent}
        <!-- kein Purge-Block für Agent-Server -->
      {:else if removePurge}
        <div class="alert alert-info py-2 small mb-3">
          {t('serverDetail.remove.purgeInfo')}
        </div>
      {:else}
        <div class="alert alert-warning py-2 small mb-3">
          {t('serverDetail.remove.purgeWarn')}
        </div>
      {/if}
      <div class="d-flex gap-2">
        <button class="btn btn-outline-secondary" onclick={() => (removeOpen = false)} disabled={busy}>{t('common.cancel')}</button>
        <button class="btn btn-danger" onclick={confirmRemove} disabled={busy || jobLocked}>
          {busy ? t('serverDetail.remove.removing') : removePurge ? t('serverDetail.remove.purgeAndRemove') : t('serverDetail.remove.removeOnly')}
        </button>
      </div>
    </Modal>

    <!-- Rechte einschränken: erklärt genau, was passiert, und warnt vor der
         Unumkehrbarkeit; Bestätigung nötig. -->
    <Modal title={t('serverDetail.restrict.title')} bind:open={restrictOpen}>
      <p class="mb-2">
        {t('serverDetail.restrict.introA')}<strong>{server.name}</strong>{t('serverDetail.restrict.introB')}
      </p>
      <p class="small mb-2">{t('serverDetail.restrict.explains')}</p>
      <ul class="small mb-3">
        <li>{t('serverDetail.restrict.bulletAllowed')}</li>
        <li>{t('serverDetail.restrict.bulletBlocked')}</li>
      </ul>
      <!-- Reichweite ehrlich benennen: Paketverwaltung und Docker führen
           prinzipbedingt Code als root aus, der Modus schützt daher gegen
           Bedienfehler, nicht gegen einen kompromittierten Service-Zugang. -->
      <div class="alert alert-secondary py-2 small mb-3">
        <strong>{t('serverDetail.restrict.scopeBold')}</strong> {t('serverDetail.restrict.scopeText')}
      </div>
      <div class="alert alert-warning py-2 small mb-3">
        <strong>{t('serverDetail.restrict.warnBold')}</strong> {t('serverDetail.restrict.warnText')}
      </div>
      <div class="d-flex gap-2">
        <button class="btn btn-outline-secondary" onclick={() => (restrictOpen = false)} disabled={busy}>{t('common.cancel')}</button>
        <button class="btn btn-warning" data-testid="restrict-confirm" onclick={restrictSudo} disabled={busy || jobLocked}>
          {busy ? t('serverDetail.restrict.restricting') : t('serverDetail.restrict.confirm')}
        </button>
      </div>
    </Modal>

    <!-- Anleitung: volle Rechte manuell wiederherstellen (Einweg-Modus
         rückgängig machen - nur direkt auf dem Server möglich). -->
    <Modal title={t('serverDetail.unrestrict.title')} bind:open={unrestrictGuideOpen}>
      <p class="small mb-2">{t('serverDetail.unrestrict.intro')}</p>
      <p class="small mb-1"><strong>{t('serverDetail.unrestrict.stepCommands')}</strong></p>
      <pre class="bg-body-tertiary border rounded p-2 small mb-3" data-testid="unrestrict-commands"><code>{unrestrictCommands}</code></pre>
      <p class="small mb-1"><strong>{t('serverDetail.unrestrict.stepAfterTitle')}</strong></p>
      <p class="small mb-3">{t('serverDetail.unrestrict.stepAfter')}</p>
      <div class="alert alert-warning py-2 small mb-3">{t('serverDetail.unrestrict.warn')}</div>
      <div class="d-flex gap-2">
        <button class="btn btn-outline-secondary" onclick={() => (unrestrictGuideOpen = false)}>{t('common.close')}</button>
        <button class="btn btn-outline-primary" onclick={() => { unrestrictGuideOpen = false; reconnectOpen = true; }}>
          {t('serverDetail.actions.reconnect')}
        </button>
      </div>
    </Modal>
  {/if}
</div>

<!-- Sortierbare Tabellenkopfzelle: Beschriftung als Schaltfläche, rechts ein
     Pfeil mit der Richtung. Das Zeichen ↕ steht für „hier ließe sich
     sortieren" - ohne das sähe eine unsortierte Spalte wie eine gewöhnliche
     Überschrift aus, und niemand käme auf die Idee zu klicken. -->
{#snippet sortHead(key, label)}
  <th aria-sort={pkgSort.key === key ? (pkgSort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}>
    <button
      type="button"
      class="btn btn-link btn-sm p-0 text-reset text-decoration-none d-inline-flex align-items-center gap-1"
      data-testid={`pkg-sort-${key}`}
      onclick={() => togglePkgSort(key)}
    >
      {label}
      <span class="text-body-secondary small" aria-hidden="true">
        {pkgSort.key === key ? (pkgSort.dir === 'asc' ? '▲' : '▼') : '↕'}
      </span>
    </button>
  </th>
{/snippet}
