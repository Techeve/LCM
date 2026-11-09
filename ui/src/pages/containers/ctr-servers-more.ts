import { LitElement, css, html } from 'lit';
import { placeFloating } from '../../components/floating';
import { baseStyles, uid } from '../../components/shared';
import type { ServerRef } from '../../api/containers';

/**
 * <lcm-ctr-servers-more label="+2 weitere" heading="Alle 5 Server" .servers=${…}> – der Rest einer gekürzten
 * Serverliste als Knopf; er klappt ein Feld mit allen Servern als Links auf. Enter oder Leertaste öffnen, der Fokus
 * geht auf den ersten Link, Pfeiltasten, Pos1 und Ende springen, Esc oder ein Klick daneben schließen.
 */
export class CtrServersMore extends LitElement {
  static override properties = {
    label: { type: String },
    heading: { type: String },
    servers: { attribute: false },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline;
      }
      .trigger {
        padding: 0;
        border: 0;
        background: none;
        color: var(--accent-text);
        font: inherit;
        cursor: pointer;
      }
      .trigger:hover {
        text-decoration: underline;
      }
      .trigger:focus-visible {
        outline: none;
        border-radius: var(--radius-s);
        box-shadow: 0 0 0 2px var(--focus-ring);
      }
      .list {
        min-inline-size: 12rem;
        max-inline-size: min(24rem, calc(100vw - 16px));
        margin: 0;
        padding: var(--space-3);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        overflow: auto;
      }
      .heading {
        margin-block-end: var(--space-2);
        color: var(--text-2);
      }
      ul {
        display: grid;
        gap: var(--space-1);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      a {
        display: block;
        padding: 0.15rem var(--space-2);
        border-radius: var(--radius-s);
        color: var(--accent-text);
        text-decoration: none;
        overflow-wrap: anywhere;
      }
      a:hover {
        background: var(--surface-2);
        text-decoration: underline;
      }
      a:focus-visible {
        outline: none;
        box-shadow: inset 0 0 0 2px var(--focus-ring);
      }
    `,
  ];

  declare label: string;
  declare heading: string;
  declare servers: ServerRef[];
  readonly #listId = uid('servers');
  #escaped = false;

  constructor() {
    super();
    this.label = '';
    this.heading = '';
    this.servers = [];
  }

  #links(): HTMLAnchorElement[] {
    return [...this.renderRoot.querySelectorAll<HTMLAnchorElement>('.list a')];
  }

  #onToggle(event: Event): void {
    if ((event as ToggleEvent).newState !== 'open') return this.#returnFocus();
    placeFloating(this.renderRoot.querySelector('.trigger')!, event.target as HTMLElement, { align: 'start', gap: 6 });
    this.#links()[0]?.focus();
  }

  /** Nach Esc zurück zum Knopf; der Browser tut das für Popover in einem Schatten nicht verlässlich. */
  #returnFocus(): void {
    if (!this.#escaped) return;
    this.#escaped = false;
    this.renderRoot.querySelector<HTMLElement>('.trigger')?.focus();
  }

  #close(): void {
    this.renderRoot.querySelector<HTMLElement>('.list')?.hidePopover();
  }

  #onKeyDown(event: KeyboardEvent): void {
    const links = this.#links();
    const index = links.indexOf(this.shadowRoot?.activeElement as HTMLAnchorElement);
    const move = (to: number) => {
      event.preventDefault();
      links[(to + links.length) % links.length]?.focus();
    };
    if (event.key === 'Escape') this.#escaped = true;
    else if (event.key === 'ArrowDown') move(index + 1);
    else if (event.key === 'ArrowUp') move(index - 1);
    else if (event.key === 'Home') move(0);
    else if (event.key === 'End') move(links.length - 1);
  }

  /**
   * Verlässt der Fokus das Feld mit Tab, schließt es; sonst bliebe es offen stehen. Die Maus darf den Fokus nicht
   * bewegen (`mousedown`): Safari fokussiert angeklickte Links nicht, das Feld schlösse sich vor dem Klick.
   */
  #onFocusOut(event: FocusEvent): void {
    const next = event.relatedTarget as Node | null;
    if (next && this.renderRoot.querySelector('.list')?.contains(next)) return;
    if (next === this.renderRoot.querySelector('.trigger')) return;
    this.#close();
  }

  override render() {
    return html`<button class="trigger" type="button" popovertarget=${this.#listId} aria-haspopup="true">${this.label}</button>
      <div
        id=${this.#listId}
        class="list"
        popover="auto"
        role="group"
        aria-label=${this.heading}
        @toggle=${this.#onToggle}
        @keydown=${this.#onKeyDown}
        @focusout=${this.#onFocusOut}
        @mousedown=${(event: Event) => event.preventDefault()}
      >
        <div class="heading">${this.heading}</div>
        <ul>
          ${this.servers.map((server) => html`<li><a href=${`/servers/${server.id}`} @click=${() => this.#close()}>${server.name}</a></li>`)}
        </ul>
      </div>`;
  }
}
customElements.define('lcm-ctr-servers-more', CtrServersMore);
