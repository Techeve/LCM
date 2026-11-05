import { afterEach, describe, expect, it } from 'vitest';
import { arrowTarget, focusPageSearch, focusablesIn } from './focus-nav';

afterEach(() => document.body.replaceChildren());

function path(element: Element): EventTarget[] {
  const nodes: EventTarget[] = [];
  for (let node: Node | null = element; node; node = node instanceof ShadowRoot ? node.host : node.parentNode) nodes.push(node);
  return nodes;
}

describe('arrowTarget', () => {
  it('moves along a menu bar and wraps around', () => {
    document.body.innerHTML = '<nav><a href="/a">A</a><a href="/b">B</a><button>C</button></nav>';
    const [a, b, c] = Array.from(document.querySelectorAll<HTMLElement>('a, button'));
    expect(arrowTarget(path(a!), 'ArrowRight')).toBe(b);
    expect(arrowTarget(path(a!), 'ArrowLeft')).toBe(c);
    expect(arrowTarget(path(c!), 'ArrowRight')).toBe(a);
    expect(arrowTarget(path(a!), 'ArrowDown')).toBeNull();
  });

  it('jumps to the first and last entry of a menu bar with Home and End', () => {
    document.body.innerHTML = '<nav><a href="/a">A</a><a href="/b">B</a><button>C</button></nav>';
    const [a, b, c] = Array.from(document.querySelectorAll<HTMLElement>('a, button'));
    expect(arrowTarget(path(b!), 'Home')).toBe(a);
    expect(arrowTarget(path(b!), 'End')).toBe(c);
    expect(arrowTarget(path(a!), 'Home')).toBe(a);
  });

  it('leaves Home and End alone outside a menu bar and in table rows', () => {
    document.body.innerHTML = '<div><button>A</button></div><table><tbody><tr tabindex="0"><td><a href="/x">x</a></td></tr></tbody></table>';
    expect(arrowTarget(path(document.querySelector('button')!), 'End')).toBeNull();
    expect(arrowTarget(path(document.querySelector('a')!), 'End')).toBeNull();
    expect(arrowTarget(path(document.querySelector('tr')!), 'Home')).toBeNull();
  });

  it('steps from a table row into its controls, also inside shadow roots, and back', () => {
    document.body.innerHTML = '<table><tbody><tr tabindex="0"><td>x</td><td><span id="host"></span></td><td><a href="/x">x</a></td></tr></tbody></table>';
    const shadow = document.getElementById('host')!.attachShadow({ mode: 'open' });
    shadow.innerHTML = '<button>in shadow</button>';
    const row = document.querySelector('tr')!;
    const inner = shadow.querySelector('button')!;
    const link = document.querySelector('a')!;
    expect(arrowTarget(path(row), 'ArrowRight')).toBe(inner);
    expect(arrowTarget(path(inner), 'ArrowRight')).toBe(link);
    expect(arrowTarget(path(inner), 'ArrowLeft')).toBe(row);
    expect(arrowTarget(path(link), 'ArrowRight')).toBeNull();
  });

  it('finds controls slotted into another shadow root', () => {
    document.body.innerHTML = '<table role="grid"><tbody><tr tabindex="0"><td><span id="cell"><span id="switch"></span></span></td></tr></tbody></table>';
    document.getElementById('cell')!.attachShadow({ mode: 'open' }).innerHTML = '<div><slot></slot></div>';
    const inner = document.getElementById('switch')!.attachShadow({ mode: 'open' });
    inner.innerHTML = '<button role="switch">x</button>';
    expect(arrowTarget(path(document.querySelector('tr')!), 'ArrowRight')).toBe(inner.querySelector('button'));
  });

  it('leaves elements with their own arrow keys alone', () => {
    document.body.innerHTML = '<nav><div role="tablist"><button role="tab">A</button><button role="tab">B</button></div></nav>';
    expect(arrowTarget(path(document.querySelector('button')!), 'ArrowRight')).toBeNull();
  });
});

describe('focusPageSearch', () => {
  it('finds a search field inside a shadow root and focuses it', () => {
    document.body.innerHTML = '<main><div id="page"></div></main>';
    const shadow = document.getElementById('page')!.attachShadow({ mode: 'open' });
    shadow.innerHTML = '<input type="text"><input type="search">';
    expect(focusPageSearch(document.querySelector('main'))).toBe(true);
    expect(shadow.activeElement).toBe(shadow.querySelector('input[type=search]'));
    expect(focusPageSearch(document.createElement('main'))).toBe(false);
  });

  it('lists only visible controls', () => {
    document.body.innerHTML = '<div><button>a</button><button disabled>b</button><div hidden><button>c</button></div></div>';
    expect(focusablesIn(document.querySelector('div')!).map((b) => b.textContent)).toEqual(['a']);
  });
});
