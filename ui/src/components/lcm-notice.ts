import { LitElement, css, html, nothing } from 'lit';
import './lcm-ampel.js';
import { icon } from './icons.js';
import { baseStyles, emit, type StatusLevel } from './shared.js';

export type NoticeTone = 'ok' | 'info' | 'warn' | 'crit';

const LEVEL: Record<Exclude<NoticeTone, 'info'>, StatusLevel> = { ok: 'great', warn: 'warn', crit: 'crit' };

/**
 * Regel für alle Meldungen: Kurzes schwebt unten rechts (Toast), Behebbares steht am Auslöser (Feld,
 * Speicherleiste, Fensterfuß), Dauerhaftes hängt am Objekt (Band).
 *
 * <lcm-notice tone="warn" heading="Wartung" action-label="Einstellungen">Ein Satz Text.</lcm-notice> – jede feste
 * Meldung: Hinweis in einer Karte, Fehler über dem Fensterfuß, Zustand am Objekt, Systemhinweis unter dem Menü.
 * Eine Anatomie: Ampel (Farbe und Form), Titel, ein Satz Text (Slot), höchstens eine Folgeaktion, bei Bedarf
 * Schließen; ein 3-px-Streifen links trägt den Ton.
 *   variant="line" (Vorgabe): Block in Karten und Fenstern, Text unter dem Titel.
 *   variant="band": eine Zeile am Objekt („Titel · Text“, Aktion rechts), bricht schmal um.
 *   variant="bar":  wie band, randlos über die volle Breite unter dem Hauptmenü.
 * Folgeaktion: `action-label` (mit `href` als Link, sonst Knopf mit Ereignis `lcm-action`) oder Slot `action`.
 * `dismissible` zeigt Schließen (`close-label`) und meldet `lcm-dismiss`; ausblenden muss die Seite.
 * Kritisches wird als Alarm angesagt (`role="alert"`), alles andere als Status.
 */
export class LcmNotice extends LitElement {
  static override properties = {
    tone: { type: String, reflect: true },
    variant: { type: String, reflect: true },
    heading: { type: String },
    actionLabel: { type: String, attribute: 'action-label' },
    href: { type: String },
    dismissible: { type: Boolean },
    closeLabel: { type: String, attribute: 'close-label' },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        --t: var(--info);
        --t-soft: var(--info-soft);
        --t-text: var(--info-text);
        display: block;
        font-size: var(--fs-s);
        line-height: 1.5;
      }
      :host([tone='ok']) {
        --t: var(--ok);
        --t-soft: var(--ok-soft);
        --t-text: var(--ok-text);
      }
      :host([tone='warn']) {
        --t: var(--warn);
        --t-soft: var(--warn-soft);
        --t-text: var(--warn-text);
      }
      :host([tone='crit']) {
        --t: var(--crit);
        --t-soft: var(--crit-soft);
        --t-text: var(--crit-text);
      }
      .notice {
        display: flex;
        flex-wrap: wrap;
        align-items: flex-start;
        gap: var(--space-1) var(--space-3);
        padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
        border-radius: var(--radius-s);
        background: var(--t-soft);
        color: var(--text-1);
        box-shadow: inset 3px 0 0 var(--t);
      }
      .mark {
        display: inline-grid;
        place-items: center;
        block-size: 1.5em;
        color: var(--t);
      }
      .mark .icon {
        inline-size: 1.15rem;
        block-size: 1.15rem;
      }
      .body {
        flex: 1 1 0;
        min-inline-size: 0;
        padding-block: 0.05rem;
        overflow-wrap: anywhere;
      }
      .title {
        display: block;
        color: var(--t-text);
        font-weight: var(--fw-semibold);
      }
      .text::slotted(*) {
        margin: 0;
      }
      .end {
        flex: none;
        display: flex;
        align-items: center;
        gap: var(--space-2);
        min-block-size: 1.5em;
      }
      .follow {
        margin-block-start: var(--space-1);
      }
      .follow.empty {
        display: none;
      }
      .action {
        padding: 0;
        border: 0;
        background: none;
        color: var(--accent-text);
        font: inherit;
        font-weight: var(--fw-semibold);
        text-decoration: none;
        white-space: nowrap;
        cursor: pointer;
      }
      .action:hover {
        text-decoration: underline;
      }
      .x {
        display: grid;
        place-items: center;
        inline-size: 1.5rem;
        block-size: 1.5rem;
        flex: none;
        padding: 0;
        border: 0;
        border-radius: var(--radius-s);
        background: transparent;
        color: var(--text-2);
        cursor: pointer;
      }
      .x:hover {
        background: color-mix(in srgb, var(--text-1) 8%, transparent);
        color: var(--text-1);
      }
      /* Zeile am Objekt: Titel und Text laufen hintereinander, die Aktion steht rechts. */
      :host([variant='band']) .notice,
      :host([variant='bar']) .notice {
        align-items: center;
      }
      :host([variant='band']) .title,
      :host([variant='bar']) .title {
        display: inline;
      }
      :host([variant='band']) .title.with-text::after,
      :host([variant='bar']) .title.with-text::after {
        content: ' · ';
        color: var(--text-3);
        font-weight: var(--fw-regular);
      }
      :host([variant='bar']) .notice {
        padding-inline: var(--page-pad) var(--space-3);
        border-radius: 0;
      }
      .end.none {
        display: none;
      }
      /* Schmal: die Aktion rutscht unter den Text. */
      @container notice (max-width: 32rem) {
        :host([variant='band']) .notice,
        :host([variant='bar']) .notice {
          align-items: flex-start;
        }
        .end.has-action {
          flex-basis: 100%;
          justify-content: space-between;
          padding-inline-start: calc(1.15rem + var(--space-3));
        }
      }
      .wrap {
        container: notice / inline-size;
      }
    `,
  ];

  declare tone: NoticeTone;
  declare variant: 'line' | 'band' | 'bar';
  declare heading: string;
  declare actionLabel: string;
  declare href: string;
  declare dismissible: boolean;
  declare closeLabel: string;
  #hasText = false;

  constructor() {
    super();
    this.tone = 'info';
    this.variant = 'line';
    this.heading = '';
    this.actionLabel = '';
    this.href = '';
    this.dismissible = false;
    this.closeLabel = '';
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#hasText = this.#textNodes();
  }

  /** Steht neben dem Titel noch Text im Slot? Dann trennt ein Punkt die beiden in der Zeile. */
  #textNodes(): boolean {
    return [...this.childNodes].some((node) => (node.nodeType === Node.TEXT_NODE ? node.textContent!.trim() !== '' : node instanceof Element && !node.hasAttribute('slot')));
  }

  #onSlotChange(): void {
    const has = this.#textNodes();
    if (has !== this.#hasText) {
      this.#hasText = has;
      this.requestUpdate();
    }
  }

  #mark() {
    if (this.tone === 'info') return html`<span class="mark">${icon('info')}</span>`;
    return html`<span class="mark"><lcm-ampel level=${LEVEL[this.tone]} size="m"></lcm-ampel></span>`;
  }

  #action() {
    if (!this.actionLabel) return html`<slot name="action" @slotchange=${() => this.requestUpdate()}></slot>`;
    if (this.href) return html`<a class="action" href=${this.href}>${this.actionLabel}</a>`;
    return html`<button class="action" type="button" @click=${() => emit(this, 'lcm-action')}>${this.actionLabel}</button>`;
  }

  override render() {
    const inline = this.variant !== 'line';
    const hasAction = !!this.actionLabel || !!this.querySelector(':scope > [slot="action"]');
    const close = this.dismissible
      ? html`<button class="x" type="button" aria-label=${this.closeLabel} @click=${() => emit(this, 'lcm-dismiss')}>${icon('x')}</button>`
      : nothing;
    return html`<div class="wrap">
      <div class="notice" role=${this.tone === 'crit' ? 'alert' : 'status'}>
        ${this.#mark()}
        <div class="body">
          ${this.heading ? html`<span class=${this.#hasText ? 'title with-text' : 'title'}>${this.heading}</span>` : nothing}<slot
            class="text"
            @slotchange=${this.#onSlotChange}
          ></slot>
          ${inline ? nothing : html`<div class=${hasAction ? 'follow' : 'follow empty'}>${this.#action()}</div>`}
        </div>
        <div class=${inline && hasAction ? 'end has-action' : this.dismissible ? 'end' : 'end none'}>${inline ? this.#action() : nothing}${close}</div>
      </div>
    </div>`;
  }
}
customElements.define('lcm-notice', LcmNotice);
