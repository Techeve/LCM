import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-dialog';
import '../../components/lcm-meter';
import '../../components/lcm-server-action';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { fetchPreview, normaliseServers, SERVERS_URL, type ActionPreviewData } from '../../api/servers';
import { BULK_URL, bulkPercent, normaliseBulk, securityApi, type BulkStatus } from '../../api/security';
import { ApiError } from '../../lib/api';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { store } from '../../lib/store';
import { actionLabels, durationText, fullCommands, previewNotes, toBubblePreview } from '../servers/preview';

/** Wer von den Servern betroffen wäre: erreichbare Server, ein Beispiel für die Befehle und dessen Vorschau. */
interface Reach {
  total: number;
  reachable: number;
  sample: string;
  preview: ActionPreviewData | undefined;
}

const REBOOT_TEXT = {
  no: 'security.bulk.impact.reboot.no',
  maybe: 'security.bulk.impact.reboot.maybe',
  yes: 'security.bulk.impact.reboot.yes',
} as const;

/**
 * <lcm-sec-bulk> – Sammel-Update: Knopf mit Erklärblase und Bestätigung mit Wirkung, dazu der Fortschritt.
 * Der Stand kommt über das Thema `bulk`. Wer nicht alle betroffenen Server sehen darf, erfährt nur, dass eines läuft.
 */
export class SecBulk extends LitElement {
  static override properties = { _dialogError: { state: true }, _open: { state: true }, _busy: { state: true }, _reach: { state: true }, _loading: { state: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .bulk {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: flex-end;
        gap: var(--space-3) var(--space-4);
      }
      .state {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
        min-inline-size: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .progress {
        display: flex;
        align-items: center;
        gap: var(--space-3);
      }
      .progress lcm-meter {
        min-inline-size: 10rem;
      }
      .warn {
        color: var(--warn-text);
      }
      h3 {
        margin: var(--space-3) 0 0;
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      pre {
        max-block-size: 12rem;
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
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _open: boolean;
  declare _busy: boolean;
  declare _reach: Reach | undefined;
  declare _loading: boolean;
  readonly #bulk = new LiveData<BulkStatus>(this, { url: () => BULK_URL, topics: () => ['bulk'], transform: normaliseBulk });

  constructor() {
    super();
    this._dialogError = '';
    this._open = false;
    this._busy = false;
    this._reach = undefined;
    this._loading = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#registerPalette();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override updated(): void {
    this.#registerPalette();
  }

  #registerPalette(): void {
    palette.register(this, auth.can('servers:write') ? [{ id: 'security:bulk', key: 'u', label: t('security.palette.bulk'), icon: 'download', run: () => void this.#ask() }] : []);
  }

  /** Zählt die erreichbaren Server und holt die Befehlsvorschau eines davon; sie zeigt, was auf jedem läuft. */
  async #loadReach(fresh: boolean): Promise<void> {
    if (this._loading || (this._reach && !fresh)) return;
    this._loading = true;
    try {
      const url = `${SERVERS_URL}?limit=1000&sort=name`;
      const page = normaliseServers(await (fresh ? store.refresh<unknown>(url) : store.get<unknown>(url)));
      const reachable = page.items.filter((server) => server.reachable);
      const sample = reachable[0];
      const preview = sample ? await fetchPreview(sample.id, 'upgrade-security').catch(() => undefined) : undefined;
      this._reach = { total: page.total, reachable: reachable.length, sample: sample?.name ?? '', preview };
    } catch {
      this._reach = undefined; // der Dialog zeigt dann nur, was ohnehin feststeht
    } finally {
      this._loading = false;
    }
  }

  async #ask(): Promise<void> {
    if (this.#bulk.data?.running) return;
    await this.#loadReach(true);
    this._open = true;
  }

  async #start(): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await securityApi.startBulk();
      this._open = false;
      toast({ tone: 'info', title: t('security.bulk.started') });
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        this._open = false;
        toast({ tone: 'info', title: t('security.bulk.alreadyRunning') });
        this.#bulk.reload();
      } else {
        this._dialogError = errorText(error);
      }
    } finally {
      this._busy = false;
    }
  }

  #impacts(): Impact[] {
    const reach = this._reach;
    if (!reach) return [];
    const skipped = reach.total - reach.reachable;
    const impacts: Impact[] = [
      { label: t('security.bulk.impact.servers'), value: t('security.bulk.impact.serversValue', { reachable: reach.reachable, total: reach.total }), tone: reach.reachable > 0 ? 'ok' : 'crit' },
    ];
    if (skipped > 0) impacts.push({ label: t('security.bulk.impact.skipped'), value: t('security.bulk.impact.skippedValue', { count: skipped }), tone: 'warn' });
    impacts.push({ label: t('security.bulk.impact.order'), value: t('security.bulk.impact.orderValue') });
    const data = reach.preview;
    if (data) {
      impacts.push({ label: t('security.bulk.impact.rebootLabel'), value: t(REBOOT_TEXT[data.reboot]), tone: data.reboot === 'no' ? 'ok' : 'warn' });
      impacts.push({ label: t('security.bulk.impact.reversible'), value: t(data.reversible ? 'security.bulk.impact.yes' : 'security.bulk.impact.no'), tone: data.reversible ? 'ok' : 'warn' });
      const duration = durationText(data.duration);
      if (duration) impacts.push({ label: t('security.bulk.impact.duration'), value: duration });
    }
    return impacts;
  }

  #dialogBody() {
    const data = this._reach?.preview;
    if (!data) return nothing;
    const notes = previewNotes(data);
    const commands = fullCommands(data);
    return html`${commands.length > 0 ? html`<h3>${t('security.bulk.commands', { name: this._reach?.sample ?? '' })}</h3><pre>${commands.join('\n\n')}</pre>` : nothing}
      ${notes.map((note) => html`<p class="note">${note}</p>`)}`;
  }

  #bubble() {
    if (this._loading && !this._reach) return { loading: true };
    const data = this._reach?.preview;
    return data ? { ...toBubblePreview(data), packages: undefined } : {};
  }

  #renderState(status: BulkStatus) {
    if (status.running) {
      const done = status.completed + status.failed;
      return html`<div class="state">
        <div class="progress">
          ${status.detailed ? html`<lcm-meter level="ok" value=${bulkPercent(status)} label=${t('security.bulk.progressLabel')}></lcm-meter>` : nothing}
          <span>${status.detailed ? t('security.bulk.running', { done, total: status.total }) : t('security.bulk.runningOther')}</span>
        </div>
        ${status.current ? html`<span>${t('security.bulk.current', { name: status.current })}</span>` : nothing}
      </div>`;
    }
    if (!status.detailed || !status.finishedAt) return nothing;
    return html`<div class="state">
      <span class=${status.failed > 0 ? 'warn' : ''}>${t('security.bulk.done', { when: formatRelative(status.finishedAt), completed: status.completed, failed: status.failed })}</span>
      ${status.lastError ? html`<span>${t('security.bulk.lastError', { error: status.lastError })}</span>` : nothing}
    </div>`;
  }

  override render() {
    const status = this.#bulk.data;
    const running = status?.running === true;
    return html`<div class="bulk">
        ${status ? this.#renderState(status) : nothing}
        ${auth.can('servers:write')
          ? html`<lcm-server-action
              variant="primary"
              icon="download"
              label=${t('security.bulk.label')}
              help=${t('security.bulk.help')}
              meaning=${t('security.bulk.meaning')}
              ?disabled=${running}
              ?busy=${this._loading && !this._open}
              .labels=${actionLabels()}
              .preview=${this.#bubble()}
              @lcm-help-open=${() => void this.#loadReach(false)}
              @click=${() => void this.#ask()}
            ></lcm-server-action>`
          : nothing}
      </div>
      <lcm-dialog
        ?open=${this._open}
        heading=${t('security.bulk.confirmHeading')}
        message=${t('security.bulk.confirmMessage')}
        confirm-label=${t('security.bulk.confirm')}
        cancel-label=${t('security.bulk.cancel')}
        .impacts=${this.#impacts()}
        error=${this._dialogError}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._open = false)}
        @lcm-confirm=${() => void this.#start()}
      >
        ${this.#dialogBody()}
      </lcm-dialog>`;
  }
}
customElements.define('lcm-sec-bulk', SecBulk);
