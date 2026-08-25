<script>
  // Trefferquote der beiden Zwischenspeicher.
  //
  // Es sind zwei sehr verschiedene Dinge, und das steht hier auch so da: Der
  // Scan-Zwischenspeicher misst, wie gleichförmig die Flotte ist (gleicher
  // Paketstand = ein Trivy-Lauf statt vieler). Der Advisory-Zwischenspeicher
  // misst, wie viel vom eigenen Paketbestand gar nicht erst nach außen gehen
  // musste. Eine gemeinsame Zahl für beide wäre bedeutungslos.
  import { link } from 'svelte-spa-router';
  import { api, ApiError } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { i18n } from '../stores/i18n.svelte.js';
  import { fmtNum } from '../lib/format.js';

  const t = (k, p) => i18n.t(k, p);

  let report = $state(null);
  let error = $state('');
  let loaded = $state(false);

  async function load() {
    error = '';
    try {
      report = await api.system.cacheStats();
    } catch (e) {
      error = e instanceof ApiError ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  // rate liefert die Quote in Prozent - oder null, wenn noch nichts gemessen
  // wurde. Null ist NICHT dasselbe wie 0 %: Ohne einen einzigen Zugriff gibt
  // es keine Quote, und „0 %" würde einen schlechten Wert vortäuschen.
  function rate(hits, misses) {
    const total = (hits ?? 0) + (misses ?? 0);
    return total === 0 ? null : Math.round(((hits ?? 0) / total) * 100);
  }

  // Farbe der Quote. Bewusst grob: Es geht um die Größenordnung, nicht um
  // Prozentpunkte.
  function rateClass(pct) {
    if (pct === null) return 'text-body-secondary';
    if (pct >= 70) return 'text-success';
    if (pct >= 30) return 'text-warning';
    return 'text-body-secondary';
  }

  const fmtWhen = (s) => {
    if (!s) return '';
    const d = new Date(s);
    return isNaN(d) ? '' : d.toLocaleString();
  };

  let scanRate = $derived(rate(report?.scan?.hits, report?.scan?.misses));
  let advRate = $derived(rate(Number(report?.advisory?.hits), Number(report?.advisory?.misses)));

  load();
</script>

<p class="text-body-secondary">{t('caches.intro')}</p>

{#if error}<div class="alert alert-danger">{error}</div>{/if}

{#if loaded && report}
  <div class="row g-3">
    <!-- Scan-Zwischenspeicher (Trivy) -->
    <div class="col-md-6">
      <div class="card h-100" data-testid="cache-scan">
        <div class="card-body">
          <h3 class="h6">{t('caches.scanTitle')}</h3>
          <p class="small text-body-secondary">{t('caches.scanIntro')}</p>

          <div class="display-6 {rateClass(scanRate)}" data-testid="cache-scan-rate">
            {scanRate === null ? '-' : `${scanRate} %`}
          </div>
          <p class="small text-body-secondary">
            {scanRate === null
              ? t('caches.noData')
              : t('caches.hitsOf', {
                  hits: fmtNum(report.scan.hits),
                  total: fmtNum(report.scan.hits + report.scan.misses),
                })}
          </p>

          <dl class="row small mb-0">
            <dt class="col-7">{t('caches.entries')}</dt>
            <dd class="col-5">{fmtNum(report.scan.entries)} / {fmtNum(report.scan.limit)}</dd>
            <dt class="col-7">{t('caches.boundTo')}</dt>
            <dd class="col-5">{report.scan.stamp ? fmtWhen(report.scan.stamp) : t('caches.never')}</dd>
          </dl>
          <!-- Ehrlichkeit vor Schönheit: Der Zähler liegt im Arbeitsspeicher.
               Nach einem Neustart sieht eine gute Quote schlecht aus, wenn
               man nicht weiß, dass sie neu beginnt. -->
          <p class="small text-body-secondary mt-2 mb-0">{t('caches.scanVolatile')}</p>
        </div>
      </div>
    </div>

    <!-- Advisory-Zwischenspeicher (Frühwarnung) -->
    <div class="col-md-6">
      <div class="card h-100" data-testid="cache-advisory">
        <div class="card-body">
          <h3 class="h6">{t('caches.advisoryTitle')}</h3>
          <p class="small text-body-secondary">{t('caches.advisoryIntro')}</p>

          <div class="display-6 {rateClass(advRate)}" data-testid="cache-advisory-rate">
            {advRate === null ? '-' : `${advRate} %`}
          </div>
          <p class="small text-body-secondary">
            {advRate === null
              ? t('caches.noData')
              : t('caches.hitsOf', {
                  hits: fmtNum(Number(report.advisory.hits)),
                  total: fmtNum(Number(report.advisory.hits) + Number(report.advisory.misses)),
                })}
          </p>

          <dl class="row small mb-0">
            <dt class="col-7">{t('caches.runs')}</dt>
            <dd class="col-5">{fmtNum(Number(report.advisory.runs ?? 0))}</dd>
            <dt class="col-7">{t('caches.since')}</dt>
            <dd class="col-5">
              {report.advisory.runs ? fmtWhen(report.advisory.since_at) : t('caches.never')}
            </dd>
            <dt class="col-7">{t('caches.entriesFresh')}</dt>
            <dd class="col-5">
              {fmtNum(Number(report.snapshot.fresh))} / {fmtNum(Number(report.snapshot.entries))}
            </dd>
            <dt class="col-7">{t('caches.details')}</dt>
            <dd class="col-5">{fmtNum(Number(report.snapshot.details))}</dd>
            <dt class="col-7">{t('caches.ttl')}</dt>
            <dd class="col-5">
              {report.ttl_minutes > 0 ? t('caches.ttlValue', { min: report.ttl_minutes }) : t('caches.ttlOff')}
            </dd>
          </dl>

          {#if report.ttl_minutes === 0}
            <p class="small text-body-secondary mt-2 mb-0" data-testid="cache-ttl-off">{t('caches.ttlOffHint')}</p>
          {/if}
          {#if auth.can('settings:manage')}
            <a href="/settings/security" use:link class="small d-inline-block mt-2">{t('caches.tune')}</a>
          {/if}
        </div>
      </div>
    </div>
  </div>

  <p class="small text-body-secondary mt-3">{t('caches.readingHint')}</p>
{:else if loaded}
  <p class="text-body-secondary">{t('caches.noData')}</p>
{/if}
