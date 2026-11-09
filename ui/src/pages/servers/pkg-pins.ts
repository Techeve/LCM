import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-switch';
import { formLayoutStyles } from '../../components/layout-styles';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { PIN_NAME_MAX, packagesApi, pinInputValid, pinNameProblem, pinsUrl, type PackagePin, type PinInput, type PinsView } from '../../api/servers-packages';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { store } from '../../lib/store';
import './set-action';
import { settingsStyles } from './set-styles';
import { plainLabels } from './srv-tab';

/** Beispiel im leeren Namensfeld: ein Präfix-Muster, wie es für Kernel gebraucht wird. */
const PATTERN_EXAMPLE = 'linux-image-*';
const EMPTY_INPUT: PinInput = { name: '', no_remove: true, hold: false, note: '', global: false };

/**
 * `<lcm-pkg-pins>`: Paket-Pins im mittigen Fenster. Pins schützen Pakete vor dem Aufräumen und frieren auf Wunsch
 * ihre Version ein; sie gelten global oder nur für diesen Server. Auf dem Server wirken sie erst nach „Anwenden“.
 */
export class PkgPins extends LitElement {
  static override properties = {
    _error: { state: true },
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    proxmox: { type: Boolean },
    view: { attribute: false },
    _input: { state: true },
    _busy: { state: true },
    _removing: { state: true },
  };
  static override styles = [
    baseStyles,
    formLayoutStyles,
    settingsStyles,
    css`
      .switches {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-3) var(--space-5);
      }
      .effects {
        display: inline-flex;
        flex-wrap: wrap;
        gap: var(--space-1);
      }
    `,
  ];

  declare open: boolean;
  declare serverId: number;
  declare serverName: string;
  declare proxmox: boolean;
  declare view: PinsView | undefined;
  /** Fehler der letzten Änderung; steht im Dialog bzw. über dem Fensterfuß. */
  declare _error: string;
  declare _input: PinInput;
  declare _busy: boolean;
  declare _removing: PackagePin | null;

  constructor() {
    super();
    this._error = '';
    this.open = false;
    this.serverId = 0;
    this.serverName = '';
    this.proxmox = false;
    this.view = undefined;
    this._input = { ...EMPTY_INPUT };
    this._busy = false;
    this._removing = null;
    new I18nController(this);
  }

  #reload(): void {
    store.invalidate(pinsUrl(this.serverId));
  }

  /** Ein Fehler bleibt dort, wo die Aktion ausgelöst wurde: im Löschdialog oder über dem Fensterfuß. */
  async #mutate(request: () => Promise<unknown>, done: string): Promise<boolean> {
    this._busy = true;
    this._error = '';
    try {
      await request();
      toast({ tone: 'ok', title: done });
      this.#reload();
      return true;
    } catch (error) {
      this._error = errorText(error);
      return false;
    } finally {
      this._busy = false;
    }
  }

  async #add(): Promise<void> {
    const input = this._input;
    if (!pinInputValid(input)) return;
    if (await this.#mutate(() => packagesApi.createPin(this.serverId, input), t('serverPackages.pins.added', { name: input.name.trim() }))) this._input = { ...EMPTY_INPUT };
  }

  async #remove(): Promise<void> {
    const pin = this._removing;
    if (!pin) return;
    if (await this.#mutate(() => packagesApi.deletePin(this.serverId, pin.id), t('serverPackages.pins.removed', { name: pin.name }))) this._removing = null;
  }

  #close(): void {
    this._error = '';
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  get #columns(): Column<PackagePin>[] {
    return [
      { key: 'name', label: t('serverPackages.pins.column.name'), kind: 'mono' },
      { key: 'serverId', label: t('serverPackages.pins.column.scope'), render: (r) => html`<lcm-badge tone=${r.serverId ? 'neutral' : 'info'}>${t(r.serverId ? 'serverPackages.pins.scope.server' : 'serverPackages.pins.scope.global')}</lcm-badge>` },
      {
        key: 'effect',
        label: t('serverPackages.pins.column.effect'),
        render: (r) =>
          html`<span class="effects">${r.noRemove ? html`<lcm-badge tone="ok">${t('serverPackages.pins.effect.noRemove')}</lcm-badge>` : nothing}${r.hold ? html`<lcm-badge tone="warn">${t('serverPackages.pins.effect.hold')}</lcm-badge>` : nothing}</span>`,
      },
      { key: 'note', label: t('serverPackages.pins.column.note'), render: (r) => r.note || '–' },
      {
        key: 'remove',
        label: t('serverPackages.pins.column.actions'),
        align: 'end',
        render: (r) => html`<lcm-button size="small" variant="ghost" icon="trash" icon-only label=${t('serverPackages.pins.remove.aria', { name: r.name })} @click=${() => (this._removing = this.view?.pins.find((p) => String(p.id) === String(r.id)) ?? null)}></lcm-button>`,
      },
    ];
  }

  #renderActions(view: PinsView): TemplateResult {
    return html`<div class="row">
      <lcm-set-action
        no-confirm
        icon="shield"
        server-id=${this.serverId}
        server-name=${this.serverName}
        ?disabled=${view.kernelPreset.length === 0 || this._busy}
        label=${t('serverPackages.pins.kernel.label')}
        help=${t('serverPackages.pins.kernel.help', { patterns: view.kernelPreset.join(', ') })}
        done=${t('serverPackages.pins.kernel.done')}
        .run=${async () => {
          await packagesApi.pinKernel(this.serverId);
          this.#reload();
        }}
      ></lcm-set-action>
      <lcm-set-action
        icon="download"
        server-id=${this.serverId}
        server-name=${this.serverName}
        label=${t('serverPackages.pins.apply.label')}
        help=${t('serverPackages.pins.apply.help')}
        heading=${t('serverPackages.pins.apply.heading', { name: this.serverName })}
        action="pins-apply"
        .impacts=${[{ label: t('serverPackages.pins.apply.pins'), value: t('serverPackages.pins.apply.pinsValue', { count: view.pins.length }) }]}
        .run=${() => packagesApi.applyPins(this.serverId)}
      ></lcm-set-action>
    </div>`;
  }

  #renderForm(): TemplateResult {
    const input = this._input;
    const set = (change: Partial<PinInput>) => (this._input = { ...this._input, ...change });
    const nameProblem = pinNameProblem(input.name);
    return html`<h3>${t('serverPackages.pins.new.title')}</h3>
      <div class="field-row">
        <lcm-input label=${t('serverPackages.pins.new.name')} mono maxlength=${PIN_NAME_MAX} placeholder=${PATTERN_EXAMPLE} hint=${t('serverPackages.pins.new.nameHint')} error=${nameProblem ? t(nameProblem, { max: PIN_NAME_MAX }) : ''} .value=${input.name} @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ name: e.detail.value })}></lcm-input>
        <lcm-input label=${t('serverPackages.pins.new.note')} width="full" maxlength=${LIMITS.description} placeholder=${t('serverPackages.pins.new.notePlaceholder')} .value=${input.note} @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ note: e.detail.value })}></lcm-input>
      </div>
      <div class="switches">
        <lcm-switch label=${t('serverPackages.pins.effect.noRemove')} on-text=${t('serverPackages.pins.new.noRemoveOn')} .checked=${input.no_remove} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => set({ no_remove: e.detail.checked })}></lcm-switch>
        <lcm-switch label=${t('serverPackages.pins.effect.hold')} on-text=${t('serverPackages.pins.new.holdOn')} .checked=${input.hold} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => set({ hold: e.detail.checked })}></lcm-switch>
        <lcm-switch label=${t('serverPackages.pins.new.global')} on-text=${t('serverPackages.pins.new.globalOn')} .checked=${input.global} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => set({ global: e.detail.checked })}></lcm-switch>
      </div>
      ${input.hold ? html`<p class="note warn">${t('serverPackages.pins.new.holdWarn')}</p>` : nothing}
      <div class="row">
        <lcm-button variant="primary" icon="plus" ?busy=${this._busy} ?disabled=${!pinInputValid(input)} @click=${() => void this.#add()}>${t('serverPackages.pins.new.add')}</lcm-button>
      </div>`;
  }

  #renderBody(view: PinsView): TemplateResult {
    if (!view.available) {
      return html`<p class="note">${this.proxmox ? t('serverPackages.pins.proxmox') : view.reason}</p>`;
    }
    return html`<div class="form-stack">
      ${this.#renderActions(view)}
      <lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${view.pins.map((p) => ({ ...p, id: String(p.id) }))} .labels=${plainLabels(t('serverPackages.pins.title'), t('serverPackages.pins.empty'))}></lcm-data-table>
      ${this.#renderForm()}
    </div>`;
  }

  override render() {
    const view = this.view;
    const removing = this._removing;
    return html`<lcm-modal ?open=${this.open} size="l" heading=${t('serverPackages.pins.title')} subtitle=${t('serverPackages.pins.subtitle')} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
        ${view ? this.#renderBody(view) : nothing}
        ${this._error && !removing ? html`<lcm-notice slot="note" tone="crit">${this._error}</lcm-notice>` : nothing}
        <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('serverPackages.pins.close')}</lcm-button>
      </lcm-modal>
      <lcm-dialog
        ?open=${removing !== null}
        tone="danger"
        heading=${t('serverPackages.pins.remove.heading', { name: removing?.name ?? '' })}
        confirm-label=${t('serverPackages.pins.remove.label')}
        cancel-label=${t('servers.confirm.cancel')}
        .impacts=${removing
          ? [
              { label: t('serverPackages.pins.column.scope'), value: t(removing.serverId ? 'serverPackages.pins.scope.server' : 'serverPackages.pins.scope.global') },
              { label: t('serverPackages.pins.remove.effect'), value: t('serverPackages.pins.remove.effectValue'), tone: 'warn' },
            ]
          : []}
        ?busy=${this._busy}
        error=${removing ? this._error : ''}
        @lcm-cancel=${() => ((this._removing = null), (this._error = ''))}
        @lcm-confirm=${() => void this.#remove()}
      ></lcm-dialog>`;
  }
}
customElements.define('lcm-pkg-pins', PkgPins);
