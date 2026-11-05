import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import type { ServerItem } from '../../api/servers';
import { imageActions, opsApi, shortDigest, type CheckStatus, type OpsImage } from '../../api/servers-ops';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import '../security/sec-cell';
import './set-action';
import { tabStyles } from './srv-styles';
import { plainLabels } from './srv-tab';

type Tone = 'neutral' | 'crit';

/** Prüfergebnisse, die eine eigene Marke verdienen; „ok“ sagt die Statusspalte als „aktuell“. */
const CHECK_LABELS: Partial<Record<CheckStatus, { key: string; tone: Tone }>> = {
  unauthorized: { key: 'serverOps.docker.check.unauthorized', tone: 'neutral' },
  not_found: { key: 'serverOps.docker.check.notFound', tone: 'neutral' },
  local: { key: 'serverOps.docker.check.local', tone: 'neutral' },
  error: { key: 'serverOps.docker.check.error', tone: 'crit' },
};

/** `<lcm-ops-docker-images .server .images>`: die Images eines Servers mit Update-Stand, Lücken, Pull, Löschen und Aufräumen. */
export class OpsDockerImages extends LitElement {
  static override properties = {
    server: { attribute: false },
    images: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    hasDocker: { type: Boolean, attribute: 'has-docker' },
    updatesOff: { type: Boolean, attribute: 'updates-off' },
  };
  static override styles = [baseStyles, tabStyles];

  declare server: ServerItem;
  declare images: OpsImage[];
  declare canWrite: boolean;
  declare hasDocker: boolean;
  declare updatesOff: boolean;

  constructor() {
    super();
    this.images = [];
    this.canWrite = false;
    this.hasDocker = false;
    this.updatesOff = false;
    new I18nController(this);
  }

  #refCell(image: OpsImage): TemplateResult {
    return html`<lcm-sec-cell mono>${image.ref || t('serverOps.docker.unnamed')}</lcm-sec-cell>
      ${image.superseded ? html`<lcm-badge>${t('serverOps.docker.superseded')}</lcm-badge>` : nothing}
      ${image.olderCopies > 0 ? html`<lcm-badge>${t('serverOps.docker.olderCopies', { count: image.olderCopies })}</lcm-badge>` : nothing}
      ${!image.inUse && !image.superseded ? html`<lcm-badge>${t('serverOps.docker.unused')}</lcm-badge>` : nothing}`;
  }

  #statusCell(image: OpsImage): TemplateResult | string {
    if (image.updateAvailable) return html`<lcm-badge tone="warn">${t('serverOps.docker.updateAvailable')}</lcm-badge>`;
    if (image.superseded) return '–';
    const check = CHECK_LABELS[image.checkStatus];
    if (check) return html`<lcm-badge tone=${check.tone}>${t(check.key)}</lcm-badge>`;
    if (image.checkStatus === 'ok') return html`<lcm-badge tone="ok">${t('serverOps.docker.current')}</lcm-badge>`;
    return html`<lcm-sec-cell muted>${t('serverOps.docker.notChecked')}</lcm-sec-cell>`;
  }

  #cveCell(image: OpsImage): TemplateResult | string {
    if (image.critical === 0 && image.high === 0) return '–';
    return html`${image.critical > 0 ? html`<lcm-badge tone="crit">${t('serverOps.docker.critical', { count: image.critical })}</lcm-badge> ` : nothing}${image.high > 0 ? html`<lcm-badge tone="warn">${t('serverOps.docker.high', { count: image.high })}</lcm-badge>` : nothing}`;
  }

  #actionCell(image: OpsImage): TemplateResult | string {
    const s = this.server;
    const can = imageActions(image);
    if (!can.pull && !can.remove) return '';
    return html`<span class="actions">
      ${can.pull
        ? html`<lcm-set-action
            variant="outline"
            icon="download"
            server-id=${s.id}
            server-name=${s.name}
            action="docker-pull"
            .params=${{ image: image.ref }}
            label=${t('serverOps.docker.pull.label')}
            help=${this.updatesOff ? t('serverOps.docker.updatesOffHelp') : t('serverOps.docker.pull.help', { image: image.ref })}
            ?disabled=${this.updatesOff}
            heading=${t('serverOps.docker.pull.heading', { image: image.ref })}
            .run=${() => opsApi.dockerPull(s.id, image.ref)}
          ></lcm-set-action>`
        : nothing}
      ${can.remove
        ? html`<lcm-set-action
            variant="outline"
            icon="trash"
            danger
            server-id=${s.id}
            server-name=${s.name}
            action="docker-image-delete"
            .params=${{ image: image.ref }}
            label=${t('serverOps.docker.remove.label')}
            help=${t('serverOps.docker.remove.help', { image: image.ref })}
            heading=${t('serverOps.docker.remove.heading', { image: image.ref })}
            .run=${() => opsApi.dockerRemoveImage(s.id, image.ref)}
          ></lcm-set-action>`
        : nothing}
    </span>`;
  }

  get #columns(): Column<OpsImage>[] {
    return [
      { key: 'ref', label: t('serverOps.docker.column.image'), sortable: true, render: (r) => this.#refCell(r) },
      { key: 'digest', label: t('serverOps.docker.column.digest'), kind: 'mono', render: (r) => shortDigest(r.digest) },
      { key: 'size', label: t('serverOps.docker.column.size'), align: 'end', render: (r) => r.size || '–' },
      { key: 'status', label: t('serverOps.docker.column.status'), render: (r) => this.#statusCell(r) },
      { key: 'cves', label: t('serverOps.docker.column.cves'), render: (r) => this.#cveCell(r) },
      ...(this.canWrite ? [{ key: 'action', label: t('serverOps.docker.column.action'), render: (r: OpsImage) => this.#actionCell(r) }] : []),
    ];
  }

  #renderPrune(): TemplateResult | typeof nothing {
    if (!this.canWrite || !this.hasDocker) return nothing;
    const s = this.server;
    return html`<lcm-set-action
      slot="actions"
      variant="outline"
      icon="trash"
      danger
      server-id=${s.id}
      server-name=${s.name}
      action="docker-prune"
      label=${t('serverOps.docker.prune.label')}
      help=${t('serverOps.docker.prune.help')}
      heading=${t('serverOps.docker.prune.heading', { name: s.name })}
      .run=${() => opsApi.dockerPrune(s.id)}
    ></lcm-set-action>`;
  }

  override render() {
    return html`<lcm-card heading=${t('serverOps.docker.images')}>
      ${this.#renderPrune()}
      <lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${this.images} .labels=${plainLabels(t('serverOps.docker.images'), t('serverOps.docker.noImages'))}></lcm-data-table>
    </lcm-card>`;
  }
}
customElements.define('lcm-ops-docker-images', OpsDockerImages);
