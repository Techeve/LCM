// xterm schreibt seine Stile (Zellgröße, Schrift, Farben, Cursor, Bildlaufleiste) in `<style>`-Elemente und
// 24-Bit-Farben per `setAttribute('style', …)`. Unter `style-src 'self'` verwirft der Browser beides: Das Terminal
// läuft, zeigt aber Proportionalschrift ohne Farben und ohne Cursor. Beides geht stattdessen über CSSOM, was die
// CSP erlaubt. xterm greift dafür auch am Dokument vorbei auf das globale `document` zu (`documentOverride` genügt
// also nicht), darum die Eingriffe an `document` und `Element`: das eine nur beim Öffnen, wo alle `<style>` entstehen,
// das andere, solange ein Terminal lebt. Das trifft sonst niemanden: Ein `style`-Attribut verwirft die CSP ohnehin.

interface Opens {
  open(host: HTMLElement): void;
}

type Sheet = { sheet: CSSStyleSheet; observer: MutationObserver };

/** Statt `<style>`: Ein `<template>` zeigt nichts an und hält trotzdem den Text, den xterm hineinschreibt. */
function styleStandIn(root: ShadowRoot, sheets: Sheet[]): HTMLTemplateElement {
  const element = document.createElement('template');
  const sheet = new CSSStyleSheet();
  const observer = new MutationObserver(() => sheet.replaceSync(element.textContent ?? ''));
  observer.observe(element, { childList: true, characterData: true, subtree: true });
  root.adoptedStyleSheets = [...root.adoptedStyleSheets, sheet];
  sheets.push({ sheet, observer });
  return element;
}

function openWithStandIns(term: Opens, host: HTMLElement, root: ShadowRoot, sheets: Sheet[]): void {
  const create = document.createElement.bind(document);
  const replaced = (tag: string, options?: ElementCreationOptions) => (tag === 'style' ? styleStandIn(root, sheets) : create(tag, options));
  document.createElement = replaced as typeof document.createElement;
  try {
    term.open(host);
  } finally {
    Reflect.deleteProperty(document, 'createElement');
  }
}

/** Leitet `setAttribute('style', …)` auf CSSOM um und gibt zurück, was das aufhebt. */
function styleAttributeViaCssom(): () => void {
  const { setAttribute } = Element.prototype;
  Element.prototype.setAttribute = function (this: Element, name: string, value: string) {
    if (name === 'style' && this instanceof HTMLElement) this.style.cssText = value;
    else setAttribute.call(this, name, value);
  };
  return () => {
    Element.prototype.setAttribute = setAttribute;
  };
}

/** Öffnet das Terminal in `host` und gibt zurück, was seine Stile wieder entfernt. */
export function openStyled(term: Opens, host: HTMLElement): () => void {
  const root = host.getRootNode() as ShadowRoot;
  const sheets: Sheet[] = [];
  openWithStandIns(term, host, root, sheets);
  const restoreAttribute = styleAttributeViaCssom();
  return () => {
    restoreAttribute();
    sheets.forEach((entry) => entry.observer.disconnect());
    const own = sheets.map((entry) => entry.sheet);
    root.adoptedStyleSheets = root.adoptedStyleSheets.filter((sheet) => !own.includes(sheet));
  };
}
