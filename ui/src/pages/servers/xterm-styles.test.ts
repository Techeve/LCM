import { describe, expect, it } from 'vitest';
import { openStyled } from './xterm-styles';

/** Macht es wie xterm: anlegen, einhängen, Text setzen. */
function fakeTerminal() {
  const made: { style?: HTMLElement; span?: HTMLElement } = {};
  return {
    made,
    open(host: HTMLElement) {
      const style = document.createElement('style');
      host.append(style);
      style.textContent = '.a { color: red; }';
      made.style = style;
      made.span = document.createElement('span');
    },
  };
}

function shadowHost(): { root: ShadowRoot; host: HTMLElement } {
  const outer = document.createElement('div');
  document.body.append(outer);
  const root = outer.attachShadow({ mode: 'open' });
  const host = document.createElement('div');
  root.append(host);
  return { root, host };
}

describe('openStyled', () => {
  it('legt statt eines Style-Elements ein Stylesheet im Shadow-Root an', () => {
    const { root, host } = shadowHost();
    const term = fakeTerminal();
    openStyled(term, host);
    expect(term.made.style?.tagName).toBe('TEMPLATE');
    expect(root.adoptedStyleSheets).toHaveLength(1);
  });

  it('setzt Einzelstile über CSSOM, andere Attribute wie gewohnt', () => {
    const { host } = shadowHost();
    const term = fakeTerminal();
    const remove = openStyled(term, host);
    const span = term.made.span as HTMLElement;
    Object.defineProperty(span, 'style', { value: { cssText: '' } });
    span.setAttribute('style', 'color: red');
    span.setAttribute('class', 'x');
    remove();
    expect(span.style.cssText).toBe('color: red');
    expect(span.getAttribute('class')).toBe('x');
  });

  it('räumt die Stile weg und lässt fremde stehen', () => {
    const { root, host } = shadowHost();
    const foreign = new CSSStyleSheet();
    root.adoptedStyleSheets = [foreign];
    const remove = openStyled(fakeTerminal(), host);
    remove();
    expect(root.adoptedStyleSheets).toEqual([foreign]);
  });

  it('stellt die Eingriffe wieder her, auch wenn das Öffnen scheitert', () => {
    const { host } = shadowHost();
    const { createElement } = document;
    const broken = {
      open() {
        throw new Error('kaputt');
      },
    };
    expect(() => openStyled(broken, host)).toThrow('kaputt');
    expect(document.createElement).toBe(createElement);
  });
});
