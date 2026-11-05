import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import '../../components/lcm-skeleton';
import { baseStyles } from '../../components/shared';
import { CACHES_URL, hitRate, normaliseCaches, type CacheReport } from '../../api/security';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { secStyles } from './sec-styles';

type Tone = 'good' | 'mid' | 'none';

/** Farbe der Quote. Bewusst grob: Es geht um die Größenordnung, nicht um Prozentpunkte. */
export function rateTone(rate: number | null): Tone {
  if (rate === null) return 'none';
  if (rate >= 70) return 'good';
  if (rate >= 30) return 'mid';
  return 'none';
}

/**
 * <lcm-sec-caches> – Trefferquote der beiden Zwischenspeicher. Es sind zwei verschiedene Dinge, und die Seite
 * fasst sie nicht zu einer Zahl zusammen: Der Scan-Speicher misst, wie gleichförmig die Flotte ist, der
 * Frühwarn-Speicher, wie viel gar nicht erst nach außen gehen musste.
 */
export class SecCaches extends LitElement {
  static override styles = [
    baseStyles,
    secStyles,
    css`
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
        gap: var(--gap);
      }
      .rate {
        margin: 0;
        font-size: var(--fs-3xl);
        font-weight: var(--head-weight);
        font-variant-numeric: tabular-nums;
        line-height: 1.1;
      }
      .rate.good {
        color: var(--ok-text);
      }
      .rate.mid {
        color: var(--warn-text);
      }
      .rate.none {
        color: var(--text-2);
      }
      p {
        margin: var(--space-2) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      dl {
        display: grid;
        grid-template-columns: 1fr auto;
        gap: var(--space-2) var(--space-4);
        margin: var(--space-4) 0 0;
        font-size: var(--fs-s);
      }
      dt {
        color: var(--text-2);
      }
      dd {
        margin: 0;
        font-variant-numeric: tabular-nums;
        text-align: end;
      }
    `,
  ];

  readonly #caches = new LiveData<CacheReport>(this, { url: () => CACHES_URL, transform: normaliseCaches });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  #rate(rate: number | null, hits: number, misses: number): TemplateResult {
    return html`<p class=${`rate ${rateTone(rate)}`}>${rate === null ? '–' : t('security.caches.percent', { value: rate })}</p>
      <p>${rate === null ? t('security.caches.noData') : t('security.caches.hitsOf', { hits: formatNumber(hits), total: formatNumber(hits + misses) })}</p>`;
  }

  #renderScan(report: CacheReport): TemplateResult {
    const { scan } = report;
    return html`<lcm-card level="2" heading=${t('security.caches.scan.title')} subtitle=${t('security.caches.scan.intro')}>
      ${this.#rate(hitRate(scan.hits, scan.misses), scan.hits, scan.misses)}
      <dl>
        <dt>${t('security.caches.scan.entries')}</dt>
        <dd>${formatNumber(scan.entries)} / ${formatNumber(scan.limit)}</dd>
        <dt>${t('security.caches.scan.boundTo')}</dt>
        <dd>${scan.stamp ? formatDateTime(scan.stamp) : t('security.caches.never')}</dd>
      </dl>
      <p>${t('security.caches.scan.volatile')}</p>
    </lcm-card>`;
  }

  #renderAdvisory(report: CacheReport): TemplateResult {
    const { advisory, snapshot } = report;
    return html`<lcm-card level="2" heading=${t('security.caches.advisory.title')} subtitle=${t('security.caches.advisory.intro')}>
      ${this.#rate(hitRate(advisory.hits, advisory.misses), advisory.hits, advisory.misses)}
      <dl>
        <dt>${t('security.caches.advisory.runs')}</dt>
        <dd>${formatNumber(advisory.runs)}</dd>
        <dt>${t('security.caches.advisory.since')}</dt>
        <dd>${advisory.runs > 0 && advisory.sinceAt ? formatDateTime(advisory.sinceAt) : t('security.caches.never')}</dd>
        <dt>${t('security.caches.advisory.entries')}</dt>
        <dd>${formatNumber(snapshot.fresh)} / ${formatNumber(snapshot.entries)}</dd>
        <dt>${t('security.caches.advisory.details')}</dt>
        <dd>${formatNumber(snapshot.details)}</dd>
        <dt>${t('security.caches.advisory.ttl')}</dt>
        <dd>${report.ttlMinutes > 0 ? t('security.caches.advisory.ttlValue', { count: report.ttlMinutes }) : t('security.caches.advisory.ttlOff')}</dd>
      </dl>
      ${report.ttlMinutes === 0 ? html`<p>${t('security.caches.advisory.ttlOffHint')}</p>` : nothing}
      ${auth.can('settings:manage') ? html`<p><a href="/settings/security">${t('security.caches.advisory.tune')}</a></p>` : nothing}
    </lcm-card>`;
  }

  override render() {
    const { data, error } = this.#caches;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="2" icon="warning" heading=${t('security.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="block" label=${t('security.loading')}></lcm-skeleton>`;
    }
    return html`<div class="stack">
      <div class="grid">${this.#renderScan(data)} ${this.#renderAdvisory(data)}</div>
    </div>`;
  }
}
customElements.define('lcm-sec-caches', SecCaches);
