import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import './ctr-servers-more';
import type { CtrServersMore } from './ctr-servers-more';

let more: CtrServersMore;

beforeEach(async () => {
  more = document.createElement('lcm-ctr-servers-more') as CtrServersMore;
  more.servers = [{ id: 1, name: 'web01' }];
  document.body.append(more);
  await more.updateComplete;
});

afterEach(() => more.remove());

describe('lcm-ctr-servers-more', () => {
  it('keeps the mouse from moving focus, Safari would close the list before the click', () => {
    const press = new MouseEvent('mousedown', { bubbles: true, composed: true, cancelable: true });
    more.shadowRoot!.querySelector('a')!.dispatchEvent(press);
    expect(press.defaultPrevented).toBe(true);
  });
});
