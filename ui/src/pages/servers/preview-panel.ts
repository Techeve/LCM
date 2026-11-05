import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-skeleton';
import { baseStyles } from '../../components/shared';
import type { ActionPreviewData } from '../../api/servers';
import { fetchConfigPreview, type ConfigPreviewAction, type ConfigPreviewData, type ConfigPreviewParams } from '../../api/servers-previews';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { fullCommands, packageLine, previewNotes } from './preview';

/** Hinweise, betroffene Pakete bzw. Images und alle Befehle einer Vorschau - der Teil unter den Wirkungen im Dialog. */
export function previewBody(data: ActionPreviewData | undefined) {
  if (!data) return nothing;
  const notes = previewNotes(data);
  const commands = fullCommands(data);
  return html`${data.packages.length > 0 ? html`<h3>${t('servers.confirm.packagesHeading')}</h3><pre>${data.packages.map(packageLine).join('\n')}</pre>` : nothing}
    ${notes.map((note) => html`<p class="note">${note}</p>`)}
    ${commands.length > 0 ? html`<h3>${t('servers.confirm.commandsHeading')}</h3><pre>${commands.join('\n\n')}</pre>` : nothing}`;
}

export const previewBodyStyles = css`
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
`;

/**
 * `<lcm-preview-panel server-id action .params>`: lädt die Befehlsvorschau einer Konfigurations-Aktion und zeigt sie im
 * Bestätigungsdialog von Formularen, die ihre Bestätigung selbst führen (Firewall, Abschotten, Werkzeuge). Meldet die
 * geladene Vorschau mit `lcm-preview` (detail: die Daten), damit der Dialog Umkehrbarkeit und Dauer daraus zeigt.
 */
export class PreviewPanel extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    action: { type: String },
    params: { attribute: false },
    _data: { state: true },
    _loading: { state: true },
  };
  static override styles = [
    baseStyles,
    previewBodyStyles,
    css`
      :host {
        display: block;
      }
    `,
  ];

  declare serverId: number;
  declare action: ConfigPreviewAction | '';
  declare params: ConfigPreviewParams;
  declare _data: ConfigPreviewData | undefined;
  declare _loading: boolean;

  #loadedKey = '';

  constructor() {
    super();
    this.serverId = 0;
    this.action = '';
    this.params = {};
    this._data = undefined;
    this._loading = false;
    new I18nController(this);
  }

  get #key(): string {
    return `${this.serverId}|${this.action}|${JSON.stringify(this.params)}`;
  }

  override updated(): void {
    if (this.action && this.#loadedKey !== this.#key) void this.#load();
  }

  async #load(): Promise<void> {
    const key = this.#key;
    this.#loadedKey = key;
    this._loading = true;
    this._data = undefined;
    try {
      const data = await fetchConfigPreview(this.serverId, this.action as ConfigPreviewAction, this.params);
      if (this.#loadedKey !== key) return;
      this._data = data;
      this.dispatchEvent(new CustomEvent('lcm-preview', { detail: data, bubbles: true, composed: true }));
    } catch {
      // Ohne Vorschau bleibt der Dialog bei dem, was die Oberfläche selbst weiß.
    } finally {
      if (this.#loadedKey === key) this._loading = false;
    }
  }

  override render() {
    if (this._loading && !this._data) return html`<lcm-skeleton shape="row" lines="2" label=${t('servers.preview.labels.loading')}></lcm-skeleton>`;
    return previewBody(this._data);
  }
}
customElements.define('lcm-preview-panel', PreviewPanel);
