import { afterEach, describe, expect, it } from 'vitest';
import { deepQuery } from './deep-query';

function host(name: string, inner: string): HTMLElement {
  const element = document.createElement(name);
  element.attachShadow({ mode: 'open' }).innerHTML = inner;
  return element;
}

afterEach(() => document.body.replaceChildren());

describe('deepQuery', () => {
  it('findet Elemente im Dokument und in verschachtelten Shadow-Bäumen', () => {
    const outer = host('x-outer', '<p id="a">a</p>');
    const inner = host('x-inner', '<span id="b">b</span>');
    outer.shadowRoot!.append(inner);
    document.body.append(outer);
    expect(deepQuery('#a')?.id).toBe('a');
    expect(deepQuery('#b')?.id).toBe('b');
    expect(deepQuery('#c')).toBeNull();
  });

  it('löst einen Pfad teilweise auf und sucht jeden Teil im Treffer des vorigen', () => {
    const left = host('x-tabs', '<button data-id="one">1</button><button data-id="two">2</button>');
    const right = host('x-tabs', '<button data-id="two">other</button>');
    left.dataset.tour = 'left';
    right.dataset.tour = 'right';
    document.body.append(left, right);
    expect(deepQuery('[data-tour="right"] >>> [data-id="two"]')?.textContent).toBe('other');
    expect(deepQuery('[data-tour="left"] >>> [data-id="two"]')?.textContent).toBe('2');
    expect(deepQuery('[data-tour="left"] >>> [data-id="three"]')).toBeNull();
    expect(deepQuery('[data-tour="none"] >>> button')).toBeNull();
  });
});
