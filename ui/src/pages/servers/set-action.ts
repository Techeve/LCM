import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-dialog';
import '../../components/lcm-server-action';
import type { ButtonVariant } from '../../components/button-styles';
import type { IconName } from '../../components/icons';
import type { Impact } from '../../components/lcm-dialog';
import type { ActionPreview } from '../../components/lcm-server-action';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { refreshServer } from '../../api/servers';
import { fetchConfigPreview, type ConfigPreviewAction, type ConfigPreviewData, type ConfigPreviewParams } from '../../api/servers-previews';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';
import { actionLabels, effectImpacts, toBubblePreview } from './preview';
import { previewBody, previewBodyStyles } from './preview-panel';

/** Was eine Aktion zurückgibt: die Konsolenausgabe (synchron) oder den gestarteten Job. */
export interface ActionResult {
  output?: string;
  job_id?: string;
}

/**
 * `<lcm-set-action>`: eine Konfigurations-Aktion eines Servers (Firewall, SSH, Zeit, Paketquellen, Sicherheitswerkzeuge,
 * Docker, Konten …). Mit `action` lädt sie die Befehlsvorschau des Backends beim Öffnen der Erklärblase bzw. vor der
 * Bestätigung: Die Blase zeigt die Befehle, der Dialog dazu Hinweise, Umkehrbarkeit und Dauer. `impacts` ergänzt, was
 * nur die Oberfläche weiß (z. B. „22 → 2222“); der Standard-Slot kommt in den Dialog. Nach dem Lauf: Toast (mit Link
 * zum Job, falls einer startet) und `lcm-done` mit der Konsolenausgabe bzw. dem Job.
 */
export class SetAction extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    action: { type: String },
    params: { attribute: false },
    label: { type: String },
    help: { type: String },
    quiet: { type: Boolean },
    meaning: { type: String },
    icon: { type: String },
    variant: { type: String },
    disabled: { type: Boolean },
    heading: { type: String },
    message: { type: String },
    confirmLabel: { type: String, attribute: 'confirm-label' },
    impacts: { attribute: false },
    danger: { type: Boolean },
    expect: { type: String },
    done: { type: String },
    noConfirm: { type: Boolean, attribute: 'no-confirm' },
    run: { attribute: false },
    _data: { state: true },
    _loading: { state: true },
    _open: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    previewBodyStyles,
    css`
      :host {
        display: inline-flex;
      }
    `,
  ];

  declare serverId: number;
  declare serverName: string;
  declare action: ConfigPreviewAction | '';
  declare params: ConfigPreviewParams;
  declare label: string;
  declare help: string;
  declare quiet: boolean;
  declare meaning: string;
  declare icon: IconName | '';
  declare variant: ButtonVariant;
  declare disabled: boolean;
  declare heading: string;
  declare message: string;
  declare confirmLabel: string;
  declare impacts: Impact[];
  declare danger: boolean;
  declare expect: string;
  declare done: string;
  declare noConfirm: boolean;
  declare run: (() => Promise<ActionResult | void>) | undefined;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _data: ConfigPreviewData | undefined;
  declare _loading: boolean;
  declare _open: boolean;
  declare _busy: boolean;

  #loadedKey = '';

  constructor() {
    super();
    this._dialogError = '';
    this.serverId = 0;
    this.serverName = '';
    this.action = '';
    this.params = {};
    this.label = '';
    this.help = '';
    this.quiet = false;
    this.meaning = '';
    this.icon = '';
    this.variant = 'outline';
    this.disabled = false;
    this.heading = '';
    this.message = '';
    this.confirmLabel = '';
    this.impacts = [];
    this.danger = false;
    this.expect = '';
    this.done = '';
    this.noConfirm = false;
    this.run = undefined;
    this._data = undefined;
    this._loading = false;
    this._open = false;
    this._busy = false;
    new I18nController(this);
  }

  get #key(): string {
    return `${this.serverId}|${this.action}|${JSON.stringify(this.params)}`;
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if ((changed.has('serverId') || changed.has('action') || changed.has('params')) && this.#loadedKey !== this.#key) {
      this._data = undefined;
      this.#loadedKey = '';
    }
  }

  /** Lädt die Vorschau, wenn sich Server, Aktion oder Argumente geändert haben. Ohne Aktion gibt es keine. */
  async #load(): Promise<void> {
    if (!this.action || this.#loadedKey === this.#key) return;
    const key = this.#key;
    this.#loadedKey = key;
    this._loading = true;
    try {
      const data = await fetchConfigPreview(this.serverId, this.action, this.params);
      if (this.#loadedKey === key) this._data = data;
    } catch {
      if (this.#loadedKey === key) this.#loadedKey = ''; // beim nächsten Öffnen erneut versuchen
    } finally {
      this._loading = false;
    }
  }

  /** Öffnet die Bestätigung, wie ein Klick auf den Knopf. */
  openConfirm(): Promise<void> {
    return this.#onClick();
  }

  async #onClick(): Promise<void> {
    if (this.noConfirm) {
      await this.#start();
      return;
    }
    await this.#load();
    this._open = true;
  }

  async #start(): Promise<void> {
    if (!this.run) return;
    this._busy = true;
    this._dialogError = '';
    try {
      const result = (await this.run()) ?? {};
      this._open = false;
      this.#announce(result);
      refreshServer(this.serverId);
      this.dispatchEvent(new CustomEvent('lcm-done', { detail: { output: result.output ?? '', job_id: result.job_id ?? '' }, bubbles: true, composed: true }));
    } catch (error) {
      // Mit offenem Bestätigungsdialog bleibt der Fehler dort, sonst kommt er als Toast.
      if (this._open) this._dialogError = errorText(error);
      else toast({ tone: 'crit', title: t('servers.action.failed', { label: this.label }), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  #announce(result: ActionResult): void {
    const jobId = result.job_id;
    if (jobId) {
      announceJob(this.label, this.serverName, jobId);
    } else if (this.done) {
      toast({ tone: 'ok', title: this.done });
    }
  }

  #bubble(): ActionPreview {
    if (this._loading && !this._data) return { loading: true };
    return this._data ? toBubblePreview(this._data) : {};
  }

  /** Server vorn, dann was nur die Oberfläche weiß, dann die Wirkung aus der Vorschau. */
  #impacts(): Impact[] {
    return [{ label: t('servers.confirm.server'), value: this.serverName }, ...this.impacts, ...effectImpacts(this._data)];
  }

  override render() {
    return html`<lcm-server-action
        label=${this.label}
        help=${this.help}
        ?quiet=${this.quiet}
        meaning=${this.meaning}
        icon=${this.icon}
        variant=${this.variant}
        ?disabled=${this.disabled}
        ?busy=${this._busy && !this._open}
        .labels=${actionLabels()}
        .preview=${this.#bubble()}
        @lcm-help-open=${() => void this.#load()}
        @click=${() => void this.#onClick()}
      ></lcm-server-action>
      <lcm-dialog
        ?open=${this._open}
        tone=${this.danger ? 'danger' : 'default'}
        heading=${this.heading || this.label}
        message=${this.message}
        confirm-label=${this.confirmLabel || this.label}
        cancel-label=${t('servers.confirm.cancel')}
        expect=${this.expect || nothing}
        .impacts=${this.#impacts()}
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._open = false)}
        @lcm-confirm=${() => void this.#start()}
      >
        <slot></slot>
        ${previewBody(this._data)}
      </lcm-dialog>`;
  }
}
customElements.define('lcm-set-action', SetAction);
