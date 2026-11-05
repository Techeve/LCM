import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-dialog';
import '../../components/lcm-server-action';
import type { IconName } from '../../components/icons';
import type { ButtonVariant } from '../../components/button-styles';
import type { ActionPreview } from '../../components/lcm-server-action';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { fetchPreview, refreshServer, type ActionPreviewData, type PreviewAction, type PreviewParams, type StartedJob } from '../../api/servers';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';
import { actionLabels, fullCommands, previewNotes, toBubblePreview, toImpacts } from './preview';

/**
 * `<lcm-srv-action>`: eine Server-Aktion mit Erklärblase (Befehlsvorschau aus dem Backend, beim Öffnen der Blase
 * geladen) und Bestätigung mit Wirkung. `run` startet den Job; danach laufen Toast und Ladeanzeige über die Ereignisse.
 * `expect` verlangt zusätzlich den getippten Servernamen.
 */
export class SrvAction extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    action: { type: String },
    params: { attribute: false },
    label: { type: String },
    help: { type: String },
    meaning: { type: String },
    icon: { type: String },
    variant: { type: String },
    disabled: { type: Boolean },
    confirmHeading: { type: String, attribute: 'confirm-heading' },
    confirmLabel: { type: String, attribute: 'confirm-label' },
    danger: { type: Boolean },
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
    css`
      :host {
        display: inline-flex;
      }
      pre {
        max-block-size: 14rem;
        margin: var(--space-2) 0 0;
        padding: var(--space-2) var(--space-3);
        overflow: auto;
        border-radius: var(--radius-s);
        background: var(--surface-1);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.55;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
      p.note {
        margin: var(--space-2) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      h3 {
        margin: var(--space-3) 0 0;
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
    `,
  ];

  declare serverId: number;
  declare serverName: string;
  declare action: PreviewAction | '';
  declare params: PreviewParams;
  declare label: string;
  declare help: string;
  declare meaning: string;
  declare icon: IconName | '';
  declare variant: ButtonVariant;
  declare disabled: boolean;
  declare confirmHeading: string;
  declare confirmLabel: string;
  declare danger: boolean;
  declare done: string;
  declare noConfirm: boolean;
  declare run: (() => Promise<StartedJob | void>) | undefined;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _data: ActionPreviewData | undefined;
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
    this.meaning = '';
    this.icon = '';
    this.variant = 'secondary';
    this.disabled = false;
    this.confirmHeading = '';
    this.confirmLabel = '';
    this.danger = false;
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

  /** Lädt die Vorschau, wenn sich Server, Aktion oder Parameter geändert haben. Ohne Aktion gibt es keine. */
  async #load(): Promise<void> {
    if (!this.action || this.#loadedKey === this.#key) return;
    const key = this.#key;
    this.#loadedKey = key;
    this._loading = true;
    try {
      const data = await fetchPreview(this.serverId, this.action, this.params);
      if (this.#loadedKey === key) this._data = data;
    } catch {
      if (this.#loadedKey === key) this.#loadedKey = ''; // beim nächsten Öffnen erneut versuchen
    } finally {
      this._loading = false;
    }
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if ((changed.has('serverId') || changed.has('action') || changed.has('params')) && this.#loadedKey !== this.#key) {
      this._data = undefined;
      this.#loadedKey = '';
    }
  }

  /** Öffnet die Bestätigung, wie ein Klick auf den Knopf (Befehlspalette, Link mit `?do=`). */
  openConfirm(): Promise<void> {
    return this.#onClick();
  }

  async #onClick(): Promise<void> {
    if (this.noConfirm || !this.run) {
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
      const job = await this.run();
      this._open = false;
      if (job?.job_id) {
        announceJob(this.label, this.serverName, job.job_id);
      } else if (this.done) {
        toast({ tone: 'ok', title: this.done });
      }
      if (job?.job_id || this.done) refreshServer(this.serverId);
      this.dispatchEvent(new CustomEvent('lcm-started', { detail: job, bubbles: true, composed: true }));
    } catch (error) {
      // Mit offenem Bestätigungsdialog bleibt der Fehler dort, sonst kommt er als Toast.
      if (this._open) this._dialogError = errorText(error);
      else toast({ tone: 'crit', title: t('servers.action.failed', { label: this.label }), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  #bubble(): ActionPreview {
    if (this._loading && !this._data) return { loading: true };
    return this._data ? toBubblePreview(this._data) : {};
  }

  #dialogBody() {
    const data = this._data;
    if (!data) return nothing;
    const notes = previewNotes(data);
    const commands = fullCommands(data);
    return html`${data.packages.length > 0
        ? html`<h3>${t('servers.confirm.packagesHeading')}</h3><pre>${data.packages.map((p) => (p.to ? `${p.name} ${p.from} → ${p.to}` : p.name)).join('\n')}</pre>`
        : nothing}
      ${notes.map((note) => html`<p class="note">${note}</p>`)}
      ${commands.length > 0 ? html`<h3>${t('servers.confirm.commandsHeading')}</h3><pre>${commands.join('\n\n')}</pre>` : nothing}`;
  }

  override render() {
    return html`<lcm-server-action
        label=${this.label}
        help=${this.help}
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
        heading=${this.confirmHeading || this.label}
        confirm-label=${this.confirmLabel || this.label}
        cancel-label=${t('servers.confirm.cancel')}
        .impacts=${toImpacts(this.serverName, this._data)}
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._open = false)}
        @lcm-confirm=${() => void this.#start()}
      >
        ${this.#dialogBody()}
      </lcm-dialog>`;
  }
}
customElements.define('lcm-srv-action', SrvAction);
