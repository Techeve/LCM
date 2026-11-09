// Die Doku kommt als HTML aus dem eigenen Markdown-Renderer des Backends
// (internal/appdocs/markdown.go, maskiert alles aus der Quelle). Trotzdem landet
// hier kein HTML-String im DOM: Der kleine Zerleger unten liest nur die Tags, die
// der Renderer erzeugt, mit ihren wenigen Attributen, und baut daraus einen Baum.
// Lit setzt den Baum als Elemente und Textknoten zusammen – unbekannte Tags und
// Attribute fallen weg, Text bleibt Text. So braucht die Seite weder innerHTML
// noch eine Trusted-Types-Ausnahme.

import { html, nothing, type TemplateResult } from 'lit';

const BLOCK_TAGS = ['h1', 'h2', 'h3', 'h4', 'p', 'ul', 'ol', 'li', 'pre', 'blockquote', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'div'] as const;
const INLINE_TAGS = ['code', 'strong', 'em', 'a'] as const;
const VOID_TAGS = ['hr', 'br'] as const;

export type DocTag = (typeof BLOCK_TAGS)[number] | (typeof INLINE_TAGS)[number] | (typeof VOID_TAGS)[number];

export interface DocElement {
  tag: DocTag;
  /** Nur geprüfte Werte: `id` (Überschriften), `href` (Links), `external` ('1' bei fremden Zielen). */
  attrs: { id?: string; href?: string; external?: boolean };
  children: DocNode[];
}

export type DocNode = DocElement | string;

const ALLOWED = new Set<string>([...BLOCK_TAGS, ...INLINE_TAGS, ...VOID_TAGS]);
const VOID = new Set<string>(VOID_TAGS);
const TAG = /<(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:\s+[a-zA-Z-]+="[^"]*")*)\s*\/?>/g;
const ATTR = /([a-zA-Z-]+)="([^"]*)"/g;
const ENTITY = /&(#\d+|#x[0-9a-fA-F]+|amp|lt|gt|quot|apos|nbsp);/g;
const NAMED: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ' };

export function decodeEntities(text: string): string {
  return text.replace(ENTITY, (_, entity: string) => {
    if (entity[0] !== '#') return NAMED[entity] ?? '';
    const code = entity[1] === 'x' || entity[1] === 'X' ? parseInt(entity.slice(2), 16) : parseInt(entity.slice(1), 10);
    return Number.isFinite(code) && code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : '';
  });
}

/**
 * Ziel eines Doku-Links: interne Pfade (alte `/#/doku/…`- und `/doku/…`-Adressen werden zu `/docs/…`), Anker und
 * http(s). Alles andere (javascript:, data:, relative Pfade) ergibt `null`, der Link wird dann zu Text.
 */
export function safeDocHref(raw: string): { href: string; external: boolean } | null {
  const value = raw.trim();
  if (/^#[\w-]+$/.test(value)) return { href: value, external: false };
  if (/^https?:\/\/[^\s"'<>]+$/i.test(value)) return { href: value, external: true };
  if (!value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return null;
  const path = value.startsWith('/#/') ? value.slice(2) : value;
  return { href: path.replace(/^\/doku(?=\/|$|#)/, '/docs'), external: false };
}

function attributesOf(tag: string, raw: string): DocElement['attrs'] {
  const attrs: DocElement['attrs'] = {};
  for (const [, name, value] of raw.matchAll(ATTR)) {
    const decoded = decodeEntities(value ?? '');
    if (name === 'id' && /^h[1-4]$/.test(tag) && /^[a-z0-9-]+$/.test(decoded)) attrs.id = decoded;
    if (name === 'href' && tag === 'a') {
      const target = safeDocHref(decoded);
      if (target) {
        attrs.href = target.href;
        attrs.external = target.external;
      }
    }
  }
  return attrs;
}

/** Zerlegt das Doku-HTML in einen Baum aus erlaubten Elementen und Text. */
export function parseDocHtml(source: string): DocNode[] {
  const root: DocElement = { tag: 'div', attrs: {}, children: [] };
  const stack: DocElement[] = [root];
  const top = () => stack[stack.length - 1]!;
  const addText = (text: string) => {
    const decoded = decodeEntities(text);
    if (decoded) top().children.push(decoded);
  };

  let last = 0;
  for (const match of source.matchAll(TAG)) {
    addText(source.slice(last, match.index));
    last = match.index + match[0].length;
    const [, closing, rawTag, rawAttrs] = match;
    const tag = rawTag!.toLowerCase();
    if (!ALLOWED.has(tag)) continue;
    if (closing) {
      const depth = stack.map((element) => element.tag).lastIndexOf(tag as DocTag);
      if (depth > 0) stack.length = depth;
      continue;
    }
    const element: DocElement = { tag: tag as DocTag, attrs: attributesOf(tag, rawAttrs ?? ''), children: [] };
    top().children.push(element);
    if (!VOID.has(tag)) stack.push(element);
  }
  addText(source.slice(last));
  uniqueIds(root.children);
  return root.children;
}

/**
 * Der Renderer vergibt Anker aus dem Überschriftentext; zweimal „Eigenen Schlüssel erzeugen“ ergäbe zweimal dieselbe
 * Kennung. Die erste behält sie (Links von außen zeigen dorthin), weitere bekommen `-2`, `-3` …
 */
function uniqueIds(nodes: DocNode[], seen = new Map<string, number>()): void {
  for (const node of nodes) {
    if (typeof node === 'string') continue;
    const id = node.attrs.id;
    if (id) {
      const count = (seen.get(id) ?? 0) + 1;
      seen.set(id, count);
      if (count > 1) node.attrs.id = `${id}-${count}`;
    }
    uniqueIds(node.children, seen);
  }
}

export interface DocHeading {
  id: string;
  text: string;
  level: number;
}

export function textOf(node: DocNode): string {
  return typeof node === 'string' ? node : node.children.map(textOf).join('');
}

/** Überschriften der Ebenen 2 und 3 für „Auf dieser Seite“. */
export function headingsOf(nodes: DocNode[]): DocHeading[] {
  const found: DocHeading[] = [];
  const walk = (node: DocNode) => {
    if (typeof node === 'string') return;
    if ((node.tag === 'h2' || node.tag === 'h3') && node.attrs.id) {
      found.push({ id: node.attrs.id, text: textOf(node), level: node.tag === 'h2' ? 2 : 3 });
    }
    node.children.forEach(walk);
  };
  nodes.forEach(walk);
  return found;
}

export interface RenderOptions {
  /** Beschriftung des Ankers neben Überschriften („Link zu diesem Abschnitt“); ohne sie gibt es keinen Anker. */
  anchorLabel?: string;
}

function anchor(id: string | undefined, options: RenderOptions) {
  return id && options.anchorLabel ? html`<a class="anchor" href=${`#${id}`} aria-label=${options.anchorLabel}>#</a>` : nothing;
}

function element(node: DocElement, options: RenderOptions): TemplateResult | typeof nothing {
  const kids = node.children.map((child) => renderDocNode(child, options));
  const id = node.attrs.id;
  switch (node.tag) {
    case 'h1':
      return html`<h1 id=${id ?? nothing}>${kids}</h1>`;
    case 'h2':
      return html`<h2 id=${id ?? nothing}>${kids}${anchor(id, options)}</h2>`;
    case 'h3':
      return html`<h3 id=${id ?? nothing}>${kids}${anchor(id, options)}</h3>`;
    case 'h4':
      return html`<h4 id=${id ?? nothing}>${kids}</h4>`;
    case 'p':
      return html`<p>${kids}</p>`;
    case 'ul':
      return html`<ul>${kids}</ul>`;
    case 'ol':
      return html`<ol>${kids}</ol>`;
    case 'li':
      return html`<li>${kids}</li>`;
    case 'pre':
      return html`<pre tabindex="0">${kids}</pre>`;
    case 'blockquote':
      return html`<blockquote>${kids}</blockquote>`;
    case 'table':
      return html`<div class="table"><table>${kids}</table></div>`;
    case 'thead':
      return html`<thead>${kids}</thead>`;
    case 'tbody':
      return html`<tbody>${kids}</tbody>`;
    case 'tr':
      return html`<tr>${kids}</tr>`;
    case 'th':
      return html`<th>${kids}</th>`;
    case 'td':
      return html`<td>${kids}</td>`;
    case 'code':
      return html`<code>${kids}</code>`;
    case 'strong':
      return html`<strong>${kids}</strong>`;
    case 'em':
      return html`<em>${kids}</em>`;
    case 'a':
      if (!node.attrs.href) return html`${kids}`;
      return node.attrs.external
        ? html`<a href=${node.attrs.href} target="_blank" rel="noopener noreferrer">${kids}</a>`
        : html`<a href=${node.attrs.href}>${kids}</a>`;
    case 'hr':
      return html`<hr />`;
    case 'br':
      return html`<br />`;
    default:
      return html`${kids}`; // div (Tabellenhülle des Renderers): nur der Inhalt zählt
  }
}

export function renderDocNode(node: DocNode, options: RenderOptions): TemplateResult | string | typeof nothing {
  return typeof node === 'string' ? node : element(node, options);
}
