<script>
  // Übersicht ALLER Zeitpläne: Gruppen-Schedules (mit ihren Rules) und die
  // system-globalen Schedules (Backup, Log-Bereinigung). Jeder Schedule ist
  // manuell auslösbar.
  import { api, ApiError } from '../../api';
  import { i18n } from '../../stores/i18n.svelte.js';
  import SettingsLayout from '../../components/SettingsLayout.svelte';
  import { toasts } from '../../stores/toast.svelte.js';

  const t = (k, p) => i18n.t(k, p);

  let schedules = $state([]);
  // Laufendes Ausloesen (Name des Schedules) - sperrt alle „Jetzt"-Knoepfe,
  // damit ein zweiter Klick den Lauf nicht ein zweites Mal anstoesst.
  let triggering = $state('');

  async function load() {
    try {
      schedules = await api.system.schedulesOverview();
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    }
  }

  async function trigger(s) {
    if (triggering) return;
    triggering = s.name;
    toasts.clear();
    try {
      if (s.kind === 'schedule') {
        await api.groups.triggerSchedule(s.schedule_id);
      } else {
        await api.system.triggerSystemSchedule(s.kind);
      }
      // Bewusst „ausgeloest", nicht „erfolgreich": Der Lauf startet hier nur -
      // sein Ergebnis steht in der Job-Historie.
      toasts.success(t('settings.schedules.triggered', { name: s.name }));
    } catch (e) {
      toasts.error(e instanceof ApiError ? e.message : String(e));
    } finally {
      triggering = '';
    }
  }

  // Nur Gruppen-Schedules stammen aus einer Servergruppe - alles andere
  // (Backup, Log-Bereinigung, CVE-Scan, Alarm-Auswertung) ist ein
  // System-Schedule aus den Einstellungen bzw. fest verdrahtet.
  function kindLabel(kind) {
    return kind === 'schedule' ? t('settings.schedules.originGroup') : t('settings.schedules.originSystem');
  }

  // Inhalt eines Schedules: die Typen seiner Rules (bzw. der System-Typ).
  function typeLabel(s) {
    if (s.kind !== 'schedule') return s.type;
    return (s.rules ?? []).map((r) => r.type).join(', ') || '-';
  }

  $effect(() => {
    load();
  });
</script>

<SettingsLayout title={t('settings.schedules.title')}>

  <div class="card">
    <div class="card-body">
      <p class="small text-body-secondary">
        {t('settings.schedules.intro')}
      </p>
      <div class="table-responsive">
        <table class="table table-sm align-middle mb-0">
          <thead>
            <tr><th>{t('settings.schedules.colName')}</th><th>{t('settings.schedules.colOrigin')}</th><th>{t('settings.schedules.colRules')}</th><th>{t('settings.schedules.colSchedule')}</th><th>{t('settings.schedules.colNextRun')}</th><th></th></tr>
          </thead>
          <tbody>
            {#each schedules as s (s.kind + (s.schedule_id ?? 0))}
              <tr>
                <td>{s.name}</td>
                <td><span class="badge {s.kind === 'schedule' ? 'border text-body-secondary' : 'text-bg-info'}">{kindLabel(s.kind)}</span></td>
                <td class="small">{typeLabel(s)}</td>
                <td><code class="small">{s.cron_expr}</code></td>
                <td class="small text-body-secondary">{s.next_run ? new Date(s.next_run).toLocaleString() : '-'}</td>
                <td class="text-end">
                  <button class="btn btn-sm btn-outline-primary" disabled={!!triggering}
                    onclick={() => trigger(s)}>{t('common.now')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>
</SettingsLayout>
