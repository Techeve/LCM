import { describe, expect, it } from 'vitest';
import './lcm-combobox';
import type { LcmCombobox } from './lcm-combobox';

describe('lcm-combobox', () => {
  it('Escape schließt nur die offene Liste, nicht das umgebende Fenster', async () => {
    const box = document.createElement('lcm-combobox') as LcmCombobox;
    box.options = [{ value: 'a', label: 'A' }];
    document.body.append(box);
    await box.updateComplete;
    Object.assign(box.renderRoot.querySelector('.list') as HTMLElement, { showPopover() {}, hidePopover() {} });
    const input = box.renderRoot.querySelector('input') as HTMLInputElement;
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    await box.updateComplete;
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    input.dispatchEvent(escape);
    expect(escape.defaultPrevented).toBe(true);
  });

  it('öffnet wieder, wenn etwas anderes die Liste geschlossen hat', async () => {
    const box = document.createElement('lcm-combobox') as LcmCombobox;
    box.options = [{ value: 'a', label: 'A' }];
    document.body.append(box);
    await box.updateComplete;
    let shown = 0;
    const list = box.renderRoot.querySelector('.list') as HTMLElement;
    Object.assign(list, { showPopover: () => shown++, hidePopover() {} });
    const input = box.renderRoot.querySelector('input') as HTMLInputElement;

    input.dispatchEvent(new Event('focus'));
    await box.updateComplete;
    expect(input.getAttribute('aria-expanded')).toBe('true');

    // Der Browser schließt das Popover von sich aus (Scrollen, anderes Popover): der Zustand muss folgen.
    list.dispatchEvent(Object.assign(new Event('toggle'), { newState: 'closed' }));
    await box.updateComplete;
    expect(input.getAttribute('aria-expanded')).toBe('false');

    input.dispatchEvent(new Event('click'));
    await box.updateComplete;
    expect(shown).toBe(2);
    expect(input.getAttribute('aria-expanded')).toBe('true');
  });
});
