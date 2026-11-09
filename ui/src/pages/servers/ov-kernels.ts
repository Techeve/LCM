import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-notice';
import { baseStyles, utilStyles } from '../../components/shared';
import { serversApi, type ServerItem } from '../../api/servers';
import { kernelStates, type KernelInfo, type KernelState } from '../../api/server-detail';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import './srv-action';

const TONE: Record<KernelState, string> = { running: 'ok', awaiting: 'warn', removable: '', fallback: '' };
const LABEL: Record<KernelState, string> = {
  running: 'serverDetail.kernels.states.running',
  awaiting: 'serverDetail.kernels.states.awaiting',
  removable: 'serverDetail.kernels.states.removable',
  fallback: 'serverDetail.kernels.states.fallback',
};

/**
 * `<lcm-srv-kernels .server .kernel can-write>`: welcher Kernel läuft und welche installiert sind. Die älteren sind die
 * Rückfallebene, falls ein neuer nicht bootet. In Containern gibt es die Karte nicht (der Kernel kommt vom Host).
 */
export class SrvKernels extends LitElement {
  static override properties = {
    server: { attribute: false },
    kernel: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      table {
        inline-size: 100%;
        border-collapse: collapse;
        font-size: var(--fs-s);
      }
      th {
        padding: 0 var(--space-3) var(--space-2) 0;
        color: var(--text-2);
        font-weight: var(--fw-medium);
        text-align: start;
      }
      td {
        padding: var(--space-2) var(--space-3) var(--space-2) 0;
        border-block-start: 1px solid var(--line);
      }
      .mono {
        font-family: var(--font-mono);
        overflow-wrap: anywhere;
      }
    `,
  ];

  declare server: ServerItem;
  declare kernel: KernelInfo;
  declare canWrite: boolean;

  constructor() {
    super();
    this.canWrite = false;
    new I18nController(this);
  }

  #cleanup() {
    const s = this.server;
    if (!this.canWrite || this.kernel.removable.length === 0) return nothing;
    return html`<lcm-srv-action
      slot="actions"
      variant="outline"
      icon="trash"
      server-id=${s.id}
      server-name=${s.name}
      action="kernels-cleanup"
      label=${t('serverDetail.kernels.cleanup', { count: this.kernel.removable.length })}
      help=${t('servers.packages.kernels.help')}
      meaning=${t('servers.packages.kernels.meaning')}
      .run=${() => serversApi.cleanupKernels(s.id)}
    ></lcm-srv-action>`;
  }

  override render() {
    const kernel = this.kernel;
    const states = kernelStates(kernel);
    const subtitle = t('serverDetail.kernels.count', { count: kernel.installed.length });
    return html`<lcm-card heading=${t('serverDetail.kernels.title')} subtitle=${subtitle}>
      ${this.#cleanup()}
      <table>
        <thead>
          <tr>
            <th>${t('serverDetail.kernels.release')}</th>
            <th>${t('serverDetail.kernels.package')}</th>
            <th>${t('serverDetail.kernels.state')}</th>
          </tr>
        </thead>
        <tbody>
          ${kernel.installed.map(
            (k, i) => html`<tr>
              <td class="mono">${k.release || '–'}</td>
              <td class="mono muted">${k.name}</td>
              <td><lcm-badge tone=${TONE[states[i] ?? 'fallback']}>${t(LABEL[states[i] ?? 'fallback'])}</lcm-badge></td>
            </tr>`,
          )}
        </tbody>
      </table>
      ${kernel.installed.some((k) => k.running) ? nothing : html`<lcm-notice tone="warn">${t('serverDetail.kernels.unknownRunning', { release: kernel.running })}</lcm-notice>`}
    </lcm-card>`;
  }
}
customElements.define('lcm-srv-kernels', SrvKernels);
