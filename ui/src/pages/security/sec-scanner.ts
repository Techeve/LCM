import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-ampel';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-server-action';
import '../../components/lcm-term';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, utilStyles } from '../../components/shared';
import type { StatusLevel } from '../../components/shared';
import { securityApi, type DbFreshness, type ScannerStatus } from '../../api/security';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { actionLabels } from '../servers/preview';
import { jobStartedToast } from './sec-styles';

/** Name des Werkzeugs, wie es in der Oberfläche steht. */
const SCANNER_NAME = 'Trivy';

const LEVEL: Record<DbFreshness, StatusLevel> = { fresh: 'ok', stale: 'warn', critical: 'crit', unknown: 'unknown' };
const FRESHNESS_TEXT: Record<DbFreshness, string> = {
  fresh: 'security.scanner.freshness.fresh',
  stale: 'security.scanner.freshness.stale',
  critical: 'security.scanner.freshness.critical',
  unknown: 'security.scanner.freshness.unknown',
};

/** Alter in Worten: ab zwei Tagen in Tagen, denn „seit 240 Stunden“ muss der Leser erst umrechnen. */
export function ageText(hours: number): string {
  return hours >= 48 ? t('security.scanner.ageDays', { count: Math.floor(hours / 24) }) : t('security.scanner.ageHours', { count: hours });
}

/**
 * <lcm-sec-scanner .scanner=${…} can-manage> – Stand des CVE-Scanners: Version, Alter der Schwachstellen-Datenbank,
 * Abschottung (Sandbox) und, mit Recht, der Knopf zum Nachladen. Steht über der Fundliste, denn deren Aussage
 * hängt am Stand der Datenbank: „keine Funde“ ist sonst nicht von „seit Wochen nicht nachgesehen“ zu unterscheiden.
 */
export class SecScanner extends LitElement {
  static override properties = { scanner: { attribute: false }, canManage: { type: Boolean, attribute: 'can-manage' }, _busy: { state: true } };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
      }
      .row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-5);
      }
      .text {
        flex: 1;
        min-inline-size: 16rem;
      }
      .line {
        margin: 0;
      }
      .line.warn {
        color: var(--warn-text);
      }
      .line.crit {
        color: var(--crit-text);
      }
      .muted {
        font-size: var(--fs-s);
      }
      .badges {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
        margin-block-start: var(--space-2);
      }
    `,
  ];

  declare scanner: ScannerStatus | undefined;
  declare canManage: boolean;
  declare _busy: boolean;

  constructor() {
    super();
    this.scanner = undefined;
    this.canManage = false;
    this._busy = false;
    new I18nController(this);
  }

  async #update(): Promise<void> {
    this._busy = true;
    try {
      const job = await securityApi.updateScannerDb();
      if (job.job_id) jobStartedToast(t('security.scanner.update.label'), job.job_id);
      else toast({ tone: 'info', title: t('security.scanner.update.started') });
    } catch (error) {
      toast({ tone: 'crit', title: t('security.scanner.update.failed'), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  #headline(scanner: ScannerStatus) {
    if (!scanner.available) return html`<p class="line crit">${t('security.scanner.noScanner')}</p>`;
    if (!scanner.updatedAt) return html`<p class="line crit">${t('security.scanner.never')}</p>`;
    const tone = scanner.freshness === 'critical' ? 'crit' : scanner.freshness === 'stale' ? 'warn' : '';
    return html`<p class=${`line ${tone}`}>
      ${t('security.scanner.state', { when: formatDateTime(scanner.updatedAt) })}
      ${scanner.freshness === 'fresh' ? nothing : html`<strong>${t('security.scanner.stale', { age: ageText(scanner.ageHours) })}</strong>`}
    </p>`;
  }

  /** Trivy selbst war schon von CVEs betroffen: Eine neuere Version gehört sichtbar, aktualisiert wird am LCM-Host. */
  #newVersion(scanner: ScannerStatus) {
    if (!scanner.available || !scanner.updateVersion) return nothing;
    return html`<lcm-badge tone="warn"
      ><lcm-term heading=${t('security.scanner.newVersion', { version: scanner.updateVersion })} definition=${t('security.scanner.newVersionHelp')}
        >${t('security.scanner.newVersion', { version: scanner.updateVersion })}</lcm-term
      ></lcm-badge
    >`;
  }

  #sandbox(scanner: ScannerStatus) {
    if (!scanner.available) return nothing;
    if (scanner.sandboxed) return html`<lcm-badge tone="ok">${t('security.scanner.sandboxOn', { backend: scanner.sandboxBackend })}</lcm-badge>`;
    return html`<lcm-badge tone="warn"
        ><lcm-term heading=${t('security.scanner.sandboxOff')} definition=${t('security.scanner.sandboxHelp')}>${t('security.scanner.sandboxOff')}</lcm-term></lcm-badge
      >
      ${scanner.sandboxNote ? html`<span class="muted">${scanner.sandboxNote}</span>` : nothing}`;
  }

  override render() {
    const scanner = this.scanner;
    if (!scanner) return nothing;
    return html`<lcm-card level="2">
      <div class="row">
        <lcm-ampel level=${LEVEL[scanner.freshness]} size="m" label=${t(FRESHNESS_TEXT[scanner.freshness])}></lcm-ampel>
        <div class="text">
          ${this.#headline(scanner)}
          <div class="badges">
            ${scanner.available && scanner.version ? html`<span class="muted mono">${SCANNER_NAME} ${scanner.version}</span>` : nothing} ${this.#newVersion(scanner)}
            ${this.#sandbox(scanner)}
          </div>
        </div>
        ${scanner.available && this.canManage
          ? html`<lcm-server-action
              icon="refresh"
              label=${t('security.scanner.update.label')}
              help=${t('security.scanner.update.help')}
              meaning=${t('security.scanner.update.meaning')}
              ?busy=${this._busy}
              .labels=${actionLabels()}
              @click=${() => void this.#update()}
            ></lcm-server-action>`
          : nothing}
      </div>
    </lcm-card>`;
  }
}
customElements.define('lcm-sec-scanner', SecScanner);
