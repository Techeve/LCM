import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import './lcm-command-palette';
import type { LcmCommandPalette, PaletteResultGroup } from './lcm-command-palette';

const groups: PaletteResultGroup[] = [
  { id: 'jump', label: 'Seiten', items: [{ id: 'page:servers', label: 'Server' }, { id: 'page:jobs', label: 'Jobs' }] },
  { id: 'general', label: 'Befehle', items: [{ id: 'cmd:back', label: 'Zurück' }] },
];

let palette: LcmCommandPalette;

beforeEach(async () => {
  palette = document.createElement('lcm-command-palette') as LcmCommandPalette;
  palette.label = 'Befehlspalette';
  palette.groups = groups;
  document.body.append(palette);
  await palette.updateComplete;
});

afterEach(() => palette.remove());

const input = () => palette.shadowRoot!.querySelector('input')!;
const options = () => [...palette.shadowRoot!.querySelectorAll<HTMLElement>('[role=option]')];
const dialog = () => palette.shadowRoot!.querySelector('dialog')!;

async function press(key: string): Promise<void> {
  input().dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, composed: true, cancelable: true }));
  await palette.updateComplete;
}

describe('accessibility', () => {
  it('is a combobox for a listbox with grouped options', () => {
    expect(input().getAttribute('role')).toBe('combobox');
    expect(input().getAttribute('aria-expanded')).toBe('true');
    const list = palette.shadowRoot!.querySelector('[role=listbox]')!;
    expect(input().getAttribute('aria-controls')).toBe(list.id);
    expect(list.querySelectorAll('[role=group]')).toHaveLength(2);
    expect(options().map((option) => option.getAttribute('aria-selected'))).toEqual(['true', 'false', 'false']);
    expect(input().getAttribute('aria-activedescendant')).toBe(options()[0].id);
  });

  it('shows the empty text instead of an empty listbox', async () => {
    palette.groups = [];
    palette.emptyText = 'Nichts gefunden.';
    await palette.updateComplete;
    expect(palette.shadowRoot!.querySelector('[role=listbox]')).toBeNull();
    expect(palette.shadowRoot!.querySelector('.empty')?.textContent).toBe('Nichts gefunden.');
    expect(input().hasAttribute('aria-controls')).toBe(false);
  });
});

describe('keyboard', () => {
  it('moves with the arrows, wraps around and follows with aria-activedescendant', async () => {
    await press('ArrowDown');
    expect(input().getAttribute('aria-activedescendant')).toBe(options()[1].id);
    await press('ArrowDown');
    await press('ArrowDown');
    expect(options()[0].getAttribute('aria-selected')).toBe('true');
    await press('ArrowUp');
    expect(options()[2].getAttribute('aria-selected')).toBe('true');
    await press('Home');
    expect(options()[0].getAttribute('aria-selected')).toBe('true');
    await press('End');
    expect(options()[2].getAttribute('aria-selected')).toBe('true');
  });

  it('selects the active result with Enter', async () => {
    const selected = vi.fn();
    palette.addEventListener('lcm-palette-select', (event) => selected((event as CustomEvent).detail));
    await press('ArrowDown');
    await press('Enter');
    expect(selected).toHaveBeenCalledWith({ id: 'page:jobs' });
  });

  it('selects by click', async () => {
    const selected = vi.fn();
    palette.addEventListener('lcm-palette-select', (event) => selected((event as CustomEvent).detail));
    options()[2].click();
    expect(selected).toHaveBeenCalledWith({ id: 'cmd:back' });
  });

  it('closes on Esc and reports it', async () => {
    const closed = vi.fn();
    palette.addEventListener('lcm-palette-close', closed);
    palette.open = true;
    await palette.updateComplete;
    dialog().dispatchEvent(new Event('cancel', { cancelable: true }));
    expect(closed).toHaveBeenCalledOnce();
    expect(palette.open).toBe(false);
  });
});

describe('query', () => {
  it('reports typing and starts at the first result again when the results change', async () => {
    const queried = vi.fn();
    palette.addEventListener('lcm-palette-query', (event) => queried((event as CustomEvent).detail));
    input().value = 'job';
    input().dispatchEvent(new Event('input', { bubbles: true, composed: true }));
    expect(queried).toHaveBeenCalledWith({ query: 'job' });

    await press('ArrowDown');
    palette.groups = [groups[0]];
    await palette.updateComplete;
    expect(options()[0].getAttribute('aria-selected')).toBe('true');
  });

  it('keeps the selection when the same results arrive again', async () => {
    await press('ArrowDown');
    palette.groups = groups.map((group) => ({ ...group }));
    await palette.updateComplete;
    expect(options()[1].getAttribute('aria-selected')).toBe('true');
  });
});
