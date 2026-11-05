import type { ReactiveController, ReactiveControllerHost } from 'lit';

export type Align = 'start' | 'end' | 'center';

interface PlaceOptions {
  align?: Align;
  gap?: number;
  matchWidth?: boolean;
  /** Das Popover bleibt beim Scrollen offen; der Aufrufer setzt es dann selbst neu (Eingabefelder mit Liste). */
  keepOnScroll?: boolean;
}

const MARGIN = 8;

/**
 * Positioniert ein geöffnetes Popover (Top Layer) am Anker.
 * Koordinaten setzen wir über das CSSOM (nicht über ein style-Attribut), das ist mit strenger CSP vereinbar.
 * Weicht nach oben bzw. zur anderen Kante aus, wenn der Platz fehlt.
 */
export function placeFloating(anchor: Element, floating: HTMLElement, options: PlaceOptions = {}): void {
  const { align = 'start', gap = 8, matchWidth = false, keepOnScroll = false } = options;
  if (!keepOnScroll) dismissOnScroll(floating);
  const a = anchor.getBoundingClientRect();
  if (matchWidth) floating.style.minInlineSize = `${a.width}px`;
  // Layoutgröße statt getBoundingClientRect: Die Einblendung skaliert das Popover noch, das darf die Lage nicht verfälschen.
  const f = { width: floating.offsetWidth, height: floating.offsetHeight };
  const viewW = document.documentElement.clientWidth;
  const viewH = window.innerHeight;
  const rtl = getComputedStyle(anchor).direction === 'rtl';

  const startLeft = rtl ? a.right - f.width : a.left;
  const endLeft = rtl ? a.left : a.right - f.width;
  const preferred = align === 'center' ? a.left + (a.width - f.width) / 2 : align === 'start' ? startLeft : endLeft;
  const left = Math.min(Math.max(preferred, MARGIN), Math.max(MARGIN, viewW - f.width - MARGIN));

  const below = a.bottom + gap;
  const above = a.top - gap - f.height;
  const fitsBelow = below + f.height <= viewH - MARGIN;
  const placeAbove = !fitsBelow && above >= MARGIN;
  const top = placeAbove ? above : Math.max(MARGIN, Math.min(below, viewH - f.height - MARGIN));

  floating.style.left = `${left}px`;
  floating.style.top = `${top}px`;
  floating.style.maxBlockSize = `${viewH - 2 * MARGIN}px`;
  floating.dataset.placement = placeAbove ? 'top' : 'bottom';
  const arrow = Math.min(Math.max(a.left + a.width / 2 - left, 16), f.width - 16);
  floating.style.setProperty('--arrow-x', `${rtl ? f.width - arrow : arrow}px`);
}

/** Schließt das Popover beim Scrollen der Seite (es hängt im Top Layer und würde sonst vom Anker wegwandern). */
function dismissOnScroll(popover: HTMLElement): void {
  const close = (event: Event): void => {
    if (event.composedPath().includes(popover)) return;
    if (popover.matches(':popover-open')) popover.hidePopover();
  };
  window.addEventListener('scroll', close, { capture: true, passive: true });
  const release = (event: Event): void => {
    if ((event as ToggleEvent).newState !== 'closed') return;
    window.removeEventListener('scroll', close, true);
    popover.removeEventListener('toggle', release);
  };
  popover.addEventListener('toggle', release);
}

interface HoverOptions {
  popover: () => HTMLElement | null;
  anchor: () => Element | null;
  openDelay?: number;
  closeDelay?: number;
  align?: Align;
  onOpen?: () => void;
}

/**
 * Öffnet ein Popover beim Überfahren und bei Tastaturfokus, nach kurzer Verzögerung.
 * Bleibt offen, solange Zeiger oder Fokus im Host (auch im Popover) sind. Esc schließt.
 */
export class HoverCard implements ReactiveController {
  open = false;
  private timer = 0;

  constructor(
    private readonly host: ReactiveControllerHost & HTMLElement,
    private readonly options: HoverOptions,
  ) {
    host.addController(this);
  }

  hostConnected(): void {
    this.host.addEventListener('pointerenter', this.onPointerEnter);
    this.host.addEventListener('pointerleave', this.onLeave);
    this.host.addEventListener('focusin', this.onFocusIn);
    this.host.addEventListener('focusout', this.onFocusOut);
    this.host.addEventListener('keydown', this.onKeyDown);
    this.host.addEventListener('click', this.onClick);
  }

  hostDisconnected(): void {
    window.clearTimeout(this.timer);
    this.host.removeEventListener('pointerenter', this.onPointerEnter);
    this.host.removeEventListener('pointerleave', this.onLeave);
    this.host.removeEventListener('focusin', this.onFocusIn);
    this.host.removeEventListener('focusout', this.onFocusOut);
    this.host.removeEventListener('keydown', this.onKeyDown);
    this.host.removeEventListener('click', this.onClick);
  }

  private schedule(open: boolean, delay: number): void {
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => (open ? this.show() : this.hide()), delay);
  }

  show(): void {
    const popover = this.options.popover();
    const anchor = this.options.anchor();
    if (!popover || !anchor || popover.matches(':popover-open')) return;
    popover.showPopover();
    placeFloating(anchor, popover, { align: this.options.align ?? 'start' });
    this.open = true;
    this.options.onOpen?.();
    this.host.requestUpdate();
  }

  hide(): void {
    window.clearTimeout(this.timer);
    const popover = this.options.popover();
    if (popover?.matches(':popover-open')) popover.hidePopover();
    if (this.open) {
      this.open = false;
      this.host.requestUpdate();
    }
  }

  private readonly onPointerEnter = (event: PointerEvent): void => {
    if (event.pointerType === 'touch') return;
    this.schedule(true, this.options.openDelay ?? 400);
  };

  private readonly onClick = (): void => this.hide();

  private readonly onLeave = (): void => this.schedule(false, this.options.closeDelay ?? 160);

  private readonly onFocusIn = (event: FocusEvent): void => {
    const inner = this.host.shadowRoot?.activeElement ?? (event.target as Element | null);
    if (inner?.matches(':focus-visible')) this.schedule(true, this.options.openDelay ?? 400);
  };

  private readonly onFocusOut = (event: FocusEvent): void => {
    if (!this.host.contains(event.relatedTarget as Node | null)) this.schedule(false, 60);
  };

  private readonly onKeyDown = (event: KeyboardEvent): void => {
    if (event.key === 'Escape' && this.open) {
      event.stopPropagation();
      this.hide();
    }
  };
}
