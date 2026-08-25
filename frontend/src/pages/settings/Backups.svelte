<script>
  // Backup-Einstellungen: Intervall/Aufbewahrung des LCM-Selbst-Backups,
  // manueller Trigger, Historie mit Download und Wiederherstellung sowie
  // Restore aus einer hochgeladenen Datei (auch auf einer frischen Instanz).
  // Der Backup-Schedule gehört bewusst hierher (Einstellungen), nicht in eine
  // Servergruppe.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import PasswordStrength from '../../components/PasswordStrength.svelte';

  const t = (k, p) => i18n.t(k, p);

  let settings = $state(null);
  let backups = $state([]);
  let error = $state('');
  let notice = $state('');
  // Laufende Aktion. Backup erstellen und vor allem das Wiederherstellen sind
  // langlaufend und destruktiv (das Restore loest einen Neustart aus) - ohne
  // Sperre loest ein zweiter Klick den Vorgang ein zweites Mal aus, bevor der
  // erste geantwortet hat.
  let busy = $state('');

  // Passphrase für ein manuelles Backup (leer = LCM_BACKUP_PASSPHRASE).
  let backupPass = $state('');

  // Rollback aus der Historie: gewähltes Backup + Passphrase.
  let restoreTarget = $state(null);
  let restorePass = $state('');

  // Restore aus hochgeladener Datei.
  let uploadFile = $state(null);
  let uploadPass = $state('');

  async function load() {
    error = '';
    try {
      settings = await api.system.getSettings();
      backups = await api.system.listBackups();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    }
  }

  function fail(e) {
    error = e instanceof ApiError ? e.message : String(e);
  }

  // Passphrase für GEPLANTE Backups (write-only; leer = unverändert). R2-027:
  // vorher gab es keinen Weg, sie über die Oberfläche zu hinterlegen - das ab
  // Werk aktive geplante Backup scheiterte still bei jedem Lauf.
  let schedulePass = $state('');

  async function saveSchedule(event) {
    event.preventDefault();
    error = '';
    notice = '';
    try {
      await api.system.configureBackups({
        enabled: settings.backup_enabled,
        interval_hours: settings.backup_interval_hours,
        retention: settings.backup_retention,
        time: settings.backup_time,
        dir: settings.backup_dir,
        auto_restart: settings.restore_auto_restart,
        passphrase: schedulePass,
      });
      schedulePass = '';
      notice = t('settings.backups.savedSchedule');
      await load();
    } catch (e) {
      fail(e);
    }
  }

  async function backupNow() {
    if (busy) return;
    busy = 'backup';
    error = '';
    notice = '';
    try {
      await api.system.triggerBackup(backupPass);
      backupPass = '';
      notice = t('settings.backups.backupCreated');
      await load();
    } catch (e) {
      fail(e);
    } finally {
      busy = '';
    }
  }

  async function download(name) {
    error = '';
    try {
      await api.system.downloadBackup(name);
    } catch (e) {
      fail(e);
    }
  }

  function startRestore(name) {
    error = '';
    notice = '';
    restorePass = '';
    restoreTarget = name;
  }

  async function remove(name) {
    if (busy) return;
    error = '';
    notice = '';
    if (!confirm(t('settings.backups.deleteConfirm', { name }))) return;
    busy = 'delete';
    try {
      await api.system.deleteBackup(name);
      backups = await api.system.listBackups();
      notice = t('settings.backups.deleted', { name });
    } catch (e) {
      fail(e);
    } finally {
      busy = '';
    }
  }

  async function confirmRestore() {
    if (busy) return;
    busy = 'restore';
    error = '';
    notice = '';
    try {
      const res = await api.system.restoreBackup(restoreTarget, restorePass);
      restoreTarget = null;
      restorePass = '';
      showStaged(res);
    } catch (e) {
      fail(e);
    } finally {
      busy = '';
    }
  }

  function onUploadPick(event) {
    uploadFile = event.currentTarget.files?.[0] ?? null;
  }

  async function uploadRestore(event) {
    event.preventDefault();
    if (busy) return;
    error = '';
    notice = '';
    if (!uploadFile) {
      error = t('settings.backups.pickFile');
      return;
    }
    busy = 'upload';
    try {
      const res = await api.system.restoreUpload(uploadFile, uploadPass);
      uploadFile = null;
      uploadPass = '';
      showStaged(res);
    } catch (e) {
      fail(e);
    }
  }

  // Meldet das Ergebnis einer vorbereiteten Wiederherstellung. Bei
  // Auto-Neustart startet der Service selbst neu (Exit 42 → Supervisor):
  // die aktuelle Session wird ungültig, daher den Nutzer nach kurzer
  // Info-Anzeige aktiv abmelden, damit er nach dem Neustart sauber neu
  // anmeldet (statt in einen 401-Fehler zu laufen).
  function showStaged(res) {
    if (res?.restart === 'auto') {
      notice = (res?.message || t('settings.backups.stagedFallback'))
        + t('settings.backups.autoRestartSuffix');
      setTimeout(() => api.client.logout(), 4000);
      return;
    }
    notice = res?.message || t('settings.backups.stagedManual');
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.backups.title')}>
  {#if error}<div class="alert alert-danger">{error}</div>{/if}
  {#if notice}<div class="alert alert-success">{notice}</div>{/if}

  {#if settings}
    <div class="card mb-4">
      <div class="card-body">
        <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
          <h3 class="h6 mb-0">{t('settings.backups.autoBackup')}</h3>
          <!-- Passphrase-Status: nur das Flag aus der Umgebung, nie der Wert. -->
          {#if settings.backup_passphrase_set}
            <span class="badge text-bg-success" title={t('settings.backups.passOkHint')} data-testid="backup-pass-badge">{t('settings.backups.passOk')}</span>
          {:else}
            <span class="badge text-bg-warning" data-testid="backup-pass-badge">{t('settings.backups.passMissing')}</span>
          {/if}
        </div>
        {#if settings.backup_enabled && !settings.backup_passphrase_set}
          <div class="alert alert-warning" data-testid="backup-pass-warning">
            <p class="mb-2">{t('settings.backups.passMissingBody')}</p>
            <pre class="small mb-2"><code>{`# /etc/systemd/system/lcm.service.d/backup.conf
[Service]
Environment=LCM_BACKUP_PASSPHRASE=ein-langes-geheimnis`}</code></pre>
            <p class="small mb-0">{t('settings.backups.passMissingOutro')}</p>
          </div>
        {/if}
        <form onsubmit={saveSchedule}>
          <div class="form-check mb-3">
            <input class="form-check-input" type="checkbox" id="be" bind:checked={settings.backup_enabled} />
            <label class="form-check-label" for="be">{t('settings.backups.enabledLabel')}</label>
          </div>
          <div class="row g-3 mb-3">
            <div class="col-md-3">
              <label class="form-label" for="bih">{t('settings.backups.intervalLabel')}</label>
              <input id="bih" type="number" min="1" class="form-control" bind:value={settings.backup_interval_hours} />
              <div class="form-text">{t('settings.backups.intervalHint')}</div>
            </div>
            <div class="col-md-3">
              <label class="form-label" for="bt">{t('settings.backups.timeLabel')}</label>
              <input id="bt" type="time" class="form-control" bind:value={settings.backup_time}
                data-testid="backup-time" />
              <div class="form-text">{t('settings.backups.timeHint')}</div>
            </div>
            <div class="col-md-3">
              <label class="form-label" for="br">{t('settings.backups.retentionLabel')}</label>
              <input id="br" type="number" min="1" class="form-control" bind:value={settings.backup_retention} />
            </div>
            <div class="col-md-3">
              <label class="form-label" for="bd">{t('settings.backups.dirLabel')}</label>
              <input id="bd" class="form-control" bind:value={settings.backup_dir} placeholder={t('settings.backups.dirPlaceholder')} />
              <div class="form-text">{t('settings.backups.dirHint')}</div>
            </div>
          </div>
          <div class="row g-3 mb-3">
            <div class="col-md-6">
              <label class="form-label" for="bpass">{t('settings.backups.passLabel')}</label>
              <input id="bpass" type="password" class="form-control" bind:value={schedulePass}
                placeholder={settings.backup_passphrase_set ? t('settings.backups.passUnchanged') : ''}
                autocomplete="new-password" data-testid="backup-pass-input" />
              <div class="form-text">{t('settings.backups.passHint')}</div>
              <PasswordStrength password={schedulePass} />
            </div>
          </div>
          <div class="form-check mb-3">
            <input class="form-check-input" type="checkbox" id="rar" bind:checked={settings.restore_auto_restart} />
            <label class="form-check-label" for="rar">{t('settings.backups.autoRestartLabel')}</label>
            <div class="form-text">
              {t('settings.backups.autoRestartHintA')}<code>LCM_RESTORE_AUTO_RESTART</code>{t('settings.backups.autoRestartHintB')}
            </div>
          </div>
          <button class="btn btn-primary">{t('common.save')}</button>
        </form>
      </div>
    </div>
  {/if}

  <div class="card mb-4">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-center mb-3 gap-2 flex-wrap">
        <h3 class="h6 mb-0">{t('settings.backups.createdBackups')}</h3>
        <div class="d-flex gap-2">
          <input
            type="password"
            class="form-control form-control-sm"
            style="max-width: 15rem"
            placeholder={t('settings.backups.passPlaceholder')}
            autocomplete="new-password"
            bind:value={backupPass}
          />
          <button class="btn btn-sm btn-primary text-nowrap" data-testid="backup-now"
            disabled={!!busy} onclick={backupNow}>
            {busy === 'backup' ? t('common.loading') : t('settings.backups.backupNow')}
          </button>
        </div>
      </div>
      {#if backupPass}
        <div class="d-flex justify-content-end mb-3">
          <div style="max-width: 20rem; width: 100%">
            <PasswordStrength password={backupPass} />
          </div>
        </div>
      {/if}
      <div class="table-responsive">
        <table class="table table-sm mb-0 align-middle">
          <thead><tr><th>{t('settings.backups.colFile')}</th><th>{t('settings.backups.colSize')}</th><th>{t('settings.backups.colTrigger')}</th><th>{t('settings.backups.colTime')}</th><th class="text-end">{t('settings.backups.colActions')}</th></tr></thead>
          <tbody>
            {#each backups as b (b.id)}
              <tr>
                <td class="small">{b.file_name}</td>
                <td class="small">{Math.round(b.size_bytes / 1024)} KB</td>
                <td class="small">{b.trigger}</td>
                <td class="small text-body-secondary">{new Date(b.created_at).toLocaleString()}</td>
                <td class="text-end text-nowrap">
                  <button class="btn btn-sm btn-outline-secondary" onclick={() => download(b.file_name)}>{t('settings.backups.download')}</button>
                  <button class="btn btn-sm btn-outline-danger" disabled={!!busy} onclick={() => startRestore(b.file_name)}>{t('settings.backups.restore')}</button>
                  <button class="btn btn-sm btn-outline-danger" title={t('settings.backups.deleteTitle')} disabled={!!busy} onclick={() => remove(b.file_name)}>{t('common.delete')}</button>
                </td>
              </tr>
              {#if restoreTarget === b.file_name}
                <tr>
                  <td colspan="5">
                    <div class="alert alert-warning mb-0">
                      <p class="mb-2">
                        <strong>{t('settings.backups.restoreWarnBold')}</strong>{t('settings.backups.restoreWarn')}
                      </p>
                      <div class="d-flex gap-2 align-items-center flex-wrap">
                        <input
                          type="password"
                          class="form-control form-control-sm"
                          style="max-width: 18rem"
                          placeholder={t('settings.backups.restorePassPlaceholder')}
                          autocomplete="new-password"
                          bind:value={restorePass}
                        />
                        <button class="btn btn-sm btn-danger" data-testid="restore-confirm"
                          disabled={!!busy} onclick={confirmRestore}>
                          {busy === 'restore' ? t('common.loading') : t('settings.backups.confirmRestore')}
                        </button>
                        <button class="btn btn-sm btn-outline-secondary" onclick={() => (restoreTarget = null)}>{t('common.cancel')}</button>
                      </div>
                    </div>
                  </td>
                </tr>
              {/if}
            {:else}
              <tr><td colspan="5" class="text-body-secondary small">{t('settings.backups.noBackups')}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-body">
      <h3 class="h6">{t('settings.backups.fromFile')}</h3>
      <p class="text-body-secondary small">
        {t('settings.backups.fromFileHintA')}<code>.lcmbak</code>{t('settings.backups.fromFileHintB')}
      </p>
      <form onsubmit={uploadRestore}>
        <div class="row g-2 align-items-center">
          <div class="col-md-5">
            <input class="form-control form-control-sm" type="file" accept=".lcmbak" onchange={onUploadPick} />
          </div>
          <div class="col-md-4">
            <input
              type="password"
              class="form-control form-control-sm"
              placeholder={t('settings.backups.restorePassPlaceholder')}
              autocomplete="new-password"
              bind:value={uploadPass}
            />
          </div>
          <div class="col-md-3">
            <button class="btn btn-sm btn-danger w-100" disabled={!uploadFile || !!busy}>
              {busy === 'upload' ? t('common.loading') : t('settings.backups.uploadRestore')}
            </button>
          </div>
        </div>
      </form>
    </div>
  </div>
</SettingsLayout>
