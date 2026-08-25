<script>
  // Geführtes Server-Onboarding in drei Schritten:
  //  1. Verbindungsdaten eingeben
  //  2. Host-Key-Fingerprint auslesen und BESTÄTIGEN (MitM-Schutz)
  //  3. Joinen (Service-User + einzigartiges Zertifikat + Scan)
  //
  // Alternativ Modus "Agent" (LCM Remote): der Server verbindet sich selbst
  // AUSGEHEND per lcm-agent - für Server hinter NAT/ohne feste IP. Hier wird
  // nur ein Name vergeben; LCM erzeugt das Enrollment-Token (einmalige
  // Anzeige) samt fertigem Installations-Befehl.
  import { push, link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { i18n } from '../stores/i18n.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let mode = $state('ssh'); // 'ssh' | 'agent'
  let step = $state(1);
  let busy = $state(false);
  let error = $state('');

  let form = $state({ name: '', host: '', port: 22, login_user: 'root', login_password: '', auth_method: 'password', restricted_access: false });
  let fingerprint = $state('');
  let keyType = $state('');
  let onboardingPubKey = $state('');
  // Bereits angelegte Server (für die sofortige Duplikat-Warnung beim Host).
  let existingServers = $state([]);

  // Server mit gleichem Host/IP - sobald der Nutzer die Adresse eintippt, wird
  // gewarnt, dass diese Maschine bereits verwaltet wird (der Server prüft es
  // beim Join zusätzlich verbindlich).
  let dupServer = $derived.by(() => {
    const h = form.host.trim().toLowerCase();
    if (!h) return null;
    return existingServers.find((s) => (s.host ?? '').trim().toLowerCase() === h) ?? null;
  });

  // Agent-Modus: Ergebnis der Token-Erzeugung (einmalige Anzeige).
  // Die Anleitung ist dreistufig: Repository einrichten → Agent installieren
  // → Dienst per Enroll einrichten; darunter die repolose Alternative.
  let agentName = $state('');
  let agentResult = $state(null); // { server, token }
  let copied = $state(''); // welcher Copy-Button zuletzt gedrückt wurde
  const origin = window.location.origin;
  // Der lcm-agent verbindet sich auf einem EIGENEN, dedizierten Port (LCM
  // Remote) - nicht auf dem UI/REST-Port. Der Port kommt aus /system/info;
  // der Host ist derselbe wie im Browser (die erreichbare Adresse). Solange
  // der Port unbekannt/0 ist, fällt die Enroll-URL auf die Origin zurück.
  let agentPort = $state(0);
  const agentBase = $derived(
    agentPort > 0
      ? `${window.location.protocol}//${window.location.hostname}:${agentPort}`
      : origin,
  );
  const repoCommand = 'curl -fsSL https://repo.techeve.de/setup.sh | sudo sh';
  const installCommand = 'sudo apt install lcm-agent';
  // Enroll zeigt auf den Agent-Port; der einmalige Binary-Download läuft
  // weiterhin über die REST-API auf dem UI-Port (origin).
  const enrollCommand = $derived(agentResult
    ? `sudo lcm-agent enroll ${agentBase} ${agentResult.token}`
    : '');
  const manualCommand = $derived(agentResult
    ? `sudo curl -fkLo /usr/local/bin/lcm-agent ${origin}/api/v1/agent/download/amd64 && sudo chmod +x /usr/local/bin/lcm-agent && sudo lcm-agent enroll ${agentBase} ${agentResult.token}`
    : '');

  async function createAgent() {
    busy = true;
    error = '';
    try {
      agentResult = await api.servers.createAgent({ name: agentName });
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function copyCommand(text, which) {
    try {
      await navigator.clipboard.writeText(text);
      copied = which;
      setTimeout(() => {
        if (copied === which) copied = '';
      }, 2000);
    } catch {
      // Ohne HTTPS/Clipboard-Rechte: Nutzer kopiert von Hand.
    }
  }

  // Public Key des System-Onboarding-Keys laden (für den Hinweis bei Key-Login).
  (async () => {
    try {
      const s = await api.system.getSettings();
      onboardingPubKey = s?.onboarding_pub_key ?? '';
    } catch {
      // Ohne settings:manage nicht abrufbar - Key-Login bleibt trotzdem wählbar.
    }
  })();

  // Serverliste laden - für den Host-Duplikat-Check (best effort).
  (async () => {
    try {
      existingServers = (await api.servers.list()) ?? [];
    } catch {
      existingServers = [];
    }
  })();

  // Dedizierten Agent-Port laden (für die Enroll-URL). Öffentlicher Endpunkt.
  (async () => {
    try {
      const info = await api.system.info();
      agentPort = info?.agent_port ?? 0;
    } catch {
      agentPort = 0; // Fallback: Enroll-URL nutzt die Origin.
    }
  })();

  async function probe() {
    busy = true;
    error = '';
    try {
      const res = await api.servers.probe(form.host, Number(form.port));
      fingerprint = res.fingerprint;
      keyType = res.key_type;
      step = 2;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  // MikroTik RouterOS: eigener, schlanker Onboarding-Fluss (nur Überwachung).
  let roForm = $state({ name: '', host: '', port: 22, login_user: 'admin', login_password: '', auth_method: 'password' });
  let roStep = $state(1); // 1 = Formular, 2 = Fingerprint bestätigen
  let roFingerprint = $state('');
  let roKeyType = $state('');
  let roResult = $state(null); // { server, public_key } (public_key nur im Key-Modus)

  // Synology DSM: Onboarding über die DSM-Web-API (kein SSH). Schritt 1
  // Formular, Schritt 2 Zertifikats-Fingerprint bestätigen - erst danach
  // gehen die Zugangsdaten über die Leitung.
  let dsmForm = $state({ name: '', host: '', port: 5001, account: '', password: '' });
  let dsmStep = $state(1);
  let dsmFingerprint = $state('');

  async function dsmProbe() {
    busy = true;
    error = '';
    try {
      const res = await api.servers.probeDSM(dsmForm.host, Number(dsmForm.port));
      dsmFingerprint = res.cert_fingerprint;
      dsmStep = 2;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function dsmCreate() {
    busy = true;
    error = '';
    try {
      const server = await api.servers.createDSM({
        name: dsmForm.name,
        host: dsmForm.host,
        port: Number(dsmForm.port),
        account: dsmForm.account,
        password: dsmForm.password,
        confirmed_fingerprint: dsmFingerprint,
      });
      push(`/servers/${server.id}`);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function roProbe() {
    busy = true;
    error = '';
    try {
      const res = await api.servers.probe(roForm.host, Number(roForm.port));
      roFingerprint = res.fingerprint;
      roKeyType = res.key_type;
      roStep = 2;
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function roCreate() {
    busy = true;
    error = '';
    try {
      const res = await api.servers.createRouterOS({
        name: roForm.name,
        host: roForm.host,
        port: Number(roForm.port),
        login_user: roForm.login_user,
        login_password: roForm.auth_method === 'key' ? '' : roForm.login_password,
        auth_method: roForm.auth_method,
        confirmed_fingerprint: roFingerprint,
      });
      // Key-Modus liefert einen Public Key zum Import → Ergebnis anzeigen;
      // Passwort-Modus ist sofort fertig → direkt zum Server.
      if (res.public_key) {
        roResult = res;
      } else {
        push(`/servers/${res.server.id}`);
      }
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function join() {
    busy = true;
    error = '';
    try {
      const server = await api.servers.join({
        name: form.name,
        host: form.host,
        port: Number(form.port),
        login_user: form.login_user,
        login_password: form.auth_method === 'key' ? '' : form.login_password,
        auth_method: form.auth_method,
        confirmed_fingerprint: fingerprint,
        restricted_access: form.restricted_access,
      });
      push(`/servers/${server.id}`);
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="container" style="max-width: 640px">
  <h1 class="h3 mb-4">{t('joinWizard.title')}</h1>

  <div class="mb-4">
    <span class="form-label d-block">{t('joinWizard.modeLabel')}</span>
    <div class="btn-group btn-group-sm" role="group">
      <input type="radio" class="btn-check" id="mode-ssh" value="ssh" bind:group={mode} disabled={busy || !!agentResult} />
      <label class="btn btn-outline-primary" for="mode-ssh">{t('joinWizard.modeSsh')}</label>
      <input type="radio" class="btn-check" id="mode-agent" value="agent" bind:group={mode} disabled={busy || !!agentResult} />
      <label class="btn btn-outline-primary" for="mode-agent">{t('joinWizard.modeAgent')}</label>
      <input type="radio" class="btn-check" id="mode-routeros" value="routeros" bind:group={mode} disabled={busy || !!agentResult || !!roResult} />
      <label class="btn btn-outline-primary" for="mode-routeros">{t('joinWizard.modeRouterOS')}</label>
      <input type="radio" class="btn-check" id="mode-dsm" value="dsm" bind:group={mode} disabled={busy || !!agentResult || !!roResult} />
      <label class="btn btn-outline-primary" for="mode-dsm">{t('joinWizard.modeDSM')}</label>
    </div>
    <div class="form-text">
      {#if mode === 'agent'}{t('joinWizard.agent.modeHint')}
      {:else if mode === 'routeros'}{t('joinWizard.routeros.modeHint')}
      {:else if mode === 'dsm'}{t('joinWizard.dsm.modeHint')}
      {:else}{t('joinWizard.modeSshHint')}{/if}
    </div>
  </div>

  {#if error}
    <div class="alert alert-danger">{error}</div>
  {/if}

  {#if mode === 'agent'}
    {#if !agentResult}
      <div class="card">
        <div class="card-body">
          <div class="mb-3">
            <label class="form-label" for="agent-name">{t('joinWizard.name')}</label>
            <input id="agent-name" class="form-control" bind:value={agentName} placeholder="notebook01" />
            <div class="form-text">{t('joinWizard.agent.nameHint')}</div>
          </div>
          <button class="btn btn-primary" onclick={createAgent} disabled={busy || !agentName.trim()} data-testid="agent-create">
            {busy ? t('joinWizard.agent.creating') : t('joinWizard.agent.create')}
          </button>
        </div>
      </div>
    {:else}
      <div class="card border-warning" data-testid="agent-token-card">
        <div class="card-body">
          <h2 class="h6">{t('joinWizard.agent.readyTitle', { name: agentResult.server?.name ?? agentName })}</h2>
          <p class="small mb-3">
            <strong>{t('joinWizard.agent.onceBold')}</strong>{t('joinWizard.agent.onceText')}
          </p>

          <div class="mb-3">
            <span class="form-label d-block">{t('joinWizard.agent.repoLabel')}</span>
            <div class="form-text mb-1">{t('joinWizard.agent.repoHint')}</div>
            <div class="input-group input-group-sm">
              <input class="form-control font-monospace" readonly value={repoCommand} />
              <button class="btn btn-outline-secondary" type="button" onclick={() => copyCommand(repoCommand, 'repo')}>
                {copied === 'repo' ? t('joinWizard.agent.copied') : t('common.copy')}
              </button>
            </div>
          </div>

          <div class="mb-3">
            <span class="form-label d-block">{t('joinWizard.agent.aptLabel')}</span>
            <div class="input-group input-group-sm">
              <input class="form-control font-monospace" readonly value={installCommand} />
              <button class="btn btn-outline-secondary" type="button" onclick={() => copyCommand(installCommand, 'apt')}>
                {copied === 'apt' ? t('joinWizard.agent.copied') : t('common.copy')}
              </button>
            </div>
          </div>

          <div class="mb-3">
            <span class="form-label d-block">{t('joinWizard.agent.enrollLabel')}</span>
            <div class="form-text mb-1">{t('joinWizard.agent.enrollHint')}</div>
            <div class="input-group input-group-sm">
              <input class="form-control font-monospace" readonly value={enrollCommand} />
              <button class="btn btn-outline-secondary" type="button" onclick={() => copyCommand(enrollCommand, 'enroll')}>
                {copied === 'enroll' ? t('joinWizard.agent.copied') : t('common.copy')}
              </button>
            </div>
            {#if agentPort > 0}
              <div class="form-text mt-1">{t('joinWizard.agent.portHint', { port: agentPort })}</div>
            {/if}
          </div>

          <div class="mb-3 border-top pt-3">
            <span class="form-label d-block">{t('joinWizard.agent.manualLabel')}</span>
            <div class="form-text mb-1">{t('joinWizard.agent.manualHint')}</div>
            <div class="input-group input-group-sm">
              <input class="form-control font-monospace" readonly value={manualCommand} />
              <button class="btn btn-outline-secondary" type="button" onclick={() => copyCommand(manualCommand, 'manual')}>
                {copied === 'manual' ? t('joinWizard.agent.copied') : t('common.copy')}
              </button>
            </div>
            <div class="form-text">{t('joinWizard.agent.archHint')}</div>
          </div>

          <p class="small text-body-secondary mb-3">{t('joinWizard.agent.afterHint')}</p>
          <button class="btn btn-primary" onclick={() => push(`/servers/${agentResult.server.id}`)} data-testid="agent-to-server">
            {t('joinWizard.agent.toServer')}
          </button>
        </div>
      </div>
    {/if}
  {:else if mode === 'dsm'}
    {#if dsmStep === 1}
      <div class="card">
        <div class="card-body">
          <p class="small text-body-secondary">{t('joinWizard.dsm.intro')}</p>
          <div class="mb-3">
            <label class="form-label" for="dsm-name">{t('joinWizard.name')}</label>
            <input id="dsm-name" class="form-control" bind:value={dsmForm.name} placeholder="nas01" data-testid="dsm-name" />
          </div>
          <div class="row">
            <div class="col-8 mb-3">
              <label class="form-label" for="dsm-host">{t('joinWizard.hostIp')}</label>
              <input id="dsm-host" class="form-control" bind:value={dsmForm.host} placeholder="192.168.1.20" data-testid="dsm-host" />
            </div>
            <div class="col-4 mb-3">
              <label class="form-label" for="dsm-port">{t('joinWizard.dsm.port')}</label>
              <input id="dsm-port" type="number" class="form-control" bind:value={dsmForm.port} />
            </div>
          </div>
          <div class="mb-3">
            <label class="form-label" for="dsm-account">{t('joinWizard.dsm.account')}</label>
            <input id="dsm-account" class="form-control" bind:value={dsmForm.account} autocomplete="off" data-testid="dsm-account" />
            <div class="form-text">{t('joinWizard.dsm.accountHint')}</div>
          </div>
          <div class="mb-3">
            <label class="form-label" for="dsm-pw">{t('common.password')}</label>
            <input id="dsm-pw" type="password" class="form-control" bind:value={dsmForm.password} autocomplete="off" data-testid="dsm-password" />
          </div>
          <button class="btn btn-primary mt-2" onclick={dsmProbe} data-testid="dsm-probe"
            disabled={busy || !dsmForm.host || !dsmForm.name || !dsmForm.account || !dsmForm.password}>
            {busy ? t('joinWizard.probing') : t('joinWizard.probeNext')}
          </button>
        </div>
      </div>
    {:else}
      <div class="card border-warning">
        <div class="card-body">
          <p>{t('joinWizard.dsm.fpConfirm')}</p>
          <pre class="bg-body-secondary p-3 rounded" style="white-space: pre-wrap; word-break: break-all"><code>{dsmFingerprint}</code></pre>
          <p class="small text-body-secondary">{t('joinWizard.dsm.fpHint')}</p>
          <div class="d-flex gap-2">
            <button class="btn btn-outline-secondary" onclick={() => (dsmStep = 1)} disabled={busy}>{t('common.back')}</button>
            <button class="btn btn-success" onclick={dsmCreate} disabled={busy} data-testid="dsm-create">
              {busy ? t('joinWizard.joining') : t('joinWizard.dsm.create')}
            </button>
          </div>
        </div>
      </div>
    {/if}
  {:else if mode === 'routeros'}
    {#if roResult}
      <div class="card border-warning" data-testid="routeros-key-card">
        <div class="card-body">
          <h2 class="h6">{t('joinWizard.routeros.keyReadyTitle', { name: roResult.server?.name ?? roForm.name })}</h2>
          <p class="small mb-2">{t('joinWizard.routeros.keyReadyHint')}</p>
          <pre class="bg-body-secondary p-2 rounded" style="white-space: pre-wrap; word-break: break-all; font-size: .75rem"><code>{roResult.public_key}</code></pre>
          <div class="input-group input-group-sm mb-3">
            <input class="form-control font-monospace" readonly value={`/user ssh-keys import public-key-file=lcm.pub user=${roForm.login_user}`} />
          </div>
          <button class="btn btn-primary" onclick={() => push(`/servers/${roResult.server.id}`)}>{t('joinWizard.routeros.toServer')}</button>
        </div>
      </div>
    {:else if roStep === 1}
      <div class="card">
        <div class="card-body">
          <div class="mb-3">
            <label class="form-label" for="ro-name">{t('joinWizard.name')}</label>
            <input id="ro-name" class="form-control" bind:value={roForm.name} placeholder="router01" />
          </div>
          <div class="row">
            <div class="col-8 mb-3">
              <label class="form-label" for="ro-host">{t('joinWizard.hostIp')}</label>
              <input id="ro-host" class="form-control" bind:value={roForm.host} placeholder="192.168.88.1" />
            </div>
            <div class="col-4 mb-3">
              <label class="form-label" for="ro-port">{t('joinWizard.sshPort')}</label>
              <input id="ro-port" type="number" class="form-control" bind:value={roForm.port} />
            </div>
          </div>
          <div class="mb-3">
            <label class="form-label" for="ro-user">{t('joinWizard.loginUser')}</label>
            <input id="ro-user" class="form-control" bind:value={roForm.login_user} autocomplete="off" placeholder="admin" />
            <div class="form-text">{t('joinWizard.routeros.userHint')}</div>
          </div>
          <div class="mb-3">
            <span class="form-label d-block">{t('joinWizard.auth')}</span>
            <div class="btn-group btn-group-sm" role="group">
              <input type="radio" class="btn-check" id="ro-auth-pw" value="password" bind:group={roForm.auth_method} />
              <label class="btn btn-outline-primary" for="ro-auth-pw">{t('common.password')}</label>
              <input type="radio" class="btn-check" id="ro-auth-key" value="key" bind:group={roForm.auth_method} />
              <label class="btn btn-outline-primary" for="ro-auth-key">{t('joinWizard.routeros.keyAuth')}</label>
            </div>
          </div>
          {#if roForm.auth_method === 'password'}
            <div class="mb-3">
              <label class="form-label" for="ro-pw">{t('common.password')}</label>
              <input id="ro-pw" type="password" class="form-control" bind:value={roForm.login_password} autocomplete="off" />
            </div>
          {:else}
            <div class="alert alert-info small">{t('joinWizard.routeros.keyModeHint')}</div>
          {/if}
          <button class="btn btn-primary mt-2" onclick={roProbe} data-testid="routeros-probe"
            disabled={busy || !roForm.host || !roForm.name || !roForm.login_user}>
            {busy ? t('joinWizard.probing') : t('joinWizard.probeNext')}
          </button>
        </div>
      </div>
    {:else}
      <div class="card border-warning">
        <div class="card-body">
          <p>{t('joinWizard.fpConfirm')}</p>
          <pre class="bg-body-secondary p-3 rounded"><code>{roKeyType}
{roFingerprint}</code></pre>
          <p class="small text-body-secondary">{t('joinWizard.fpHint')}</p>
          <div class="d-flex gap-2">
            <button class="btn btn-outline-secondary" onclick={() => (roStep = 1)} disabled={busy}>{t('common.back')}</button>
            <button class="btn btn-success" onclick={roCreate} disabled={busy} data-testid="routeros-create">
              {busy ? t('joinWizard.joining') : t('joinWizard.routeros.create')}
            </button>
          </div>
        </div>
      </div>
    {/if}
  {:else}

  <ol class="list-group list-group-numbered mb-4">
    <li class="list-group-item {step >= 1 ? 'fw-semibold' : 'text-body-secondary'}">{t('joinWizard.step1')}</li>
    <li class="list-group-item {step >= 2 ? 'fw-semibold' : 'text-body-secondary'}">{t('joinWizard.step2')}</li>
    <li class="list-group-item {step >= 3 ? 'fw-semibold' : 'text-body-secondary'}">{t('joinWizard.step3')}</li>
  </ol>

  {#if step === 1}
    <div class="card">
      <div class="card-body">
        <div class="mb-3">
          <label class="form-label" for="name">{t('joinWizard.name')}</label>
          <input id="name" class="form-control" bind:value={form.name} placeholder="web01" />
        </div>
        <div class="row">
          <div class="col-8 mb-3">
            <label class="form-label" for="host">{t('joinWizard.hostIp')}</label>
            <input id="host" class="form-control {dupServer ? 'is-invalid' : ''}" bind:value={form.host} placeholder="10.0.0.5" />
            {#if dupServer}
              <div class="invalid-feedback d-block" data-testid="join-dup-host">
                {t('joinWizard.duplicateHost')}
                <a href={`/servers/${dupServer.id}`} use:link>{dupServer.name}</a>.
              </div>
            {/if}
          </div>
          <div class="col-4 mb-3">
            <label class="form-label" for="port">{t('joinWizard.sshPort')}</label>
            <input id="port" type="number" class="form-control" bind:value={form.port} />
          </div>
        </div>
        <div class="mb-3">
          <label class="form-label" for="user">{t('joinWizard.loginUser')}</label>
          <input id="user" class="form-control" bind:value={form.login_user} autocomplete="off" />
          <div class="form-text">
            <code>root</code>{t('joinWizard.loginUserHint')}
          </div>
          <!-- Aufklappbare Schritt-für-Schritt-Anleitung: Root-SSH fürs
               Onboarding öffnen. Bewusst <details> statt Tooltip - die
               Anleitung braucht Befehle und Platz. -->
          <details class="mt-2 small" data-testid="root-ssh-guide">
            <summary class="text-primary" style="cursor: pointer">{t('joinWizard.rootGuide.title')}</summary>
            <div class="border rounded p-3 mt-2 bg-body-tertiary">
              <p class="mb-2">{t('joinWizard.rootGuide.intro')}</p>
              <ol class="ps-3 mb-2">
                <li class="mb-2">{t('joinWizard.rootGuide.step1')}</li>
                <li class="mb-2">
                  {t('joinWizard.rootGuide.step2a')}<code>sudo passwd root</code>{t('joinWizard.rootGuide.step2b')}
                </li>
                <li class="mb-2">
                  {t('joinWizard.rootGuide.step3a')}
                  <pre class="p-2 mt-1 mb-1 bg-body-secondary rounded" style="white-space: pre-wrap"><code>echo 'PermitRootLogin yes' | sudo tee /etc/ssh/sshd_config.d/00-root-onboarding.conf</code></pre>
                  {t('joinWizard.rootGuide.step3b')}<code>/etc/ssh/sshd_config.d</code>{t('joinWizard.rootGuide.step3c')}<code>/etc/ssh/sshd_config</code>{t('joinWizard.rootGuide.step3d')}<code>PermitRootLogin yes</code>{t('joinWizard.rootGuide.step3e')}
                </li>
                <li class="mb-2">
                  {t('joinWizard.rootGuide.step4a')}<code>sudo systemctl restart ssh</code>{t('joinWizard.rootGuide.step4b')}<code>sudo systemctl restart sshd</code>{t('joinWizard.rootGuide.step4c')}
                </li>
                <li>{t('joinWizard.rootGuide.step5')}</li>
              </ol>
              <div class="alert alert-warning py-2 small mb-0">
                {t('joinWizard.rootGuide.afterA')}<code>sudo rm /etc/ssh/sshd_config.d/00-root-onboarding.conf</code>{t('joinWizard.rootGuide.afterB')}
              </div>
            </div>
          </details>
        </div>
        <div class="mb-3">
          <span class="form-label d-block">{t('joinWizard.auth')}</span>
          <div class="btn-group btn-group-sm" role="group">
            <input type="radio" class="btn-check" id="auth-pw" value="password" bind:group={form.auth_method} />
            <label class="btn btn-outline-primary" for="auth-pw">{t('common.password')}</label>
            <input type="radio" class="btn-check" id="auth-key" value="key" bind:group={form.auth_method} />
            <label class="btn btn-outline-primary" for="auth-key">{t('joinWizard.systemKey')}</label>
          </div>
        </div>
        {#if form.auth_method === 'password'}
          <div class="mb-3">
            <label class="form-label" for="pw">{t('common.password')}</label>
            <input id="pw" type="password" class="form-control" bind:value={form.login_password} autocomplete="off" />
            <div class="form-text">{t('joinWizard.passwordHint')}</div>
          </div>
        {:else}
          <div class="alert alert-info small">
            {t('joinWizard.keyHintA')}<code>~/.ssh/authorized_keys</code>{t('joinWizard.keyHintB')}
            {#if onboardingPubKey}
              <pre class="mb-0 mt-2 p-2 bg-body-secondary rounded" style="white-space: pre-wrap; word-break: break-all; font-size: .75rem"><code>{onboardingPubKey}</code></pre>
            {:else}
              <div class="mt-1">{t('joinWizard.keyFindHintA')}<strong>{t('joinWizard.keyFindHintBold')}</strong>.</div>
            {/if}
          </div>
        {/if}

        <div class="border-top pt-3 mt-2">
          <div class="form-check">
            <input class="form-check-input" type="checkbox" id="restricted" bind:checked={form.restricted_access} />
            <label class="form-check-label" for="restricted">{t('joinWizard.restrictedLabel')}</label>
          </div>
          <div class="form-text">{t('joinWizard.restrictedHint')}</div>
        </div>

        <button class="btn btn-primary mt-3" onclick={probe} disabled={busy || !form.host || !form.name || !!dupServer}>
          {busy ? t('joinWizard.probing') : t('joinWizard.probeNext')}
        </button>
      </div>
    </div>
  {:else if step === 2}
    <div class="card border-warning">
      <div class="card-body">
        <p>{t('joinWizard.fpConfirm')}</p>
        <pre class="bg-body-secondary p-3 rounded"><code>{keyType}
{fingerprint}</code></pre>
        <p class="small text-body-secondary">
          {t('joinWizard.fpHint')}
        </p>
        <div class="d-flex gap-2">
          <button class="btn btn-outline-secondary" onclick={() => (step = 1)}>{t('common.back')}</button>
          <button class="btn btn-success" onclick={() => (step = 3)}>{t('joinWizard.fpOk')}</button>
        </div>
      </div>
    </div>
  {:else}
    <div class="card">
      <div class="card-body">
        <p>
          {t('joinWizard.joinIntroA')}<strong>{t('joinWizard.joinIntroBold')}</strong>{t('joinWizard.joinIntroB')}
        </p>
        <div class="d-flex gap-2">
          <button class="btn btn-outline-secondary" onclick={() => (step = 2)} disabled={busy}>{t('common.back')}</button>
          <button class="btn btn-primary" onclick={join} disabled={busy}>
            {busy ? t('joinWizard.joining') : t('joinWizard.joinNow')}
          </button>
        </div>
      </div>
    </div>
  {/if}
  {/if}
</div>
