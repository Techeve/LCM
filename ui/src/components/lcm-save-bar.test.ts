import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { unsaved } from '../lib/unsaved';
import { shownBars } from './bar-watch';
import './lcm-save-bar';
import type { LcmSaveBar, SaveBarLabels } from './lcm-save-bar';

const labels: SaveBarLabels = {
  region: 'Änderungen speichern',
  unsaved: 'Ungespeicherte Änderungen',
  hint: 'Strg+S',
  save: 'Speichern',
  discard: 'Verwerfen',
  jump: 'Zum Feld',
  fieldError: 'Nicht gespeichert: „{field}“ prüfen',
};

let bar: LcmSaveBar;

beforeEach(async () => {
  bar = document.createElement('lcm-save-bar') as LcmSaveBar;
  bar.labels = labels;
  document.body.append(bar);
  await bar.updateComplete;
});

afterEach(() => bar.remove());

const root = () => bar.shadowRoot!;
const buttons = () => [...root().querySelectorAll('lcm-button')].map((button) => button.textContent!.trim());

async function set(patch: Partial<LcmSaveBar>): Promise<void> {
  Object.assign(bar, patch);
  await bar.updateComplete;
}

describe('lcm-save-bar', () => {
  it('registers while it is visible, so toasts can make room for it', async () => {
    expect(shownBars()).not.toContain(bar);
    await set({ dirty: true });
    expect(shownBars()).toContain(bar);
    await set({ dirty: false });
    expect(shownBars()).not.toContain(bar);
    await set({ dirty: true });
    bar.remove();
    expect(shownBars()).not.toContain(bar);
  });

  it('stays hidden until there is something to save or report', async () => {
    expect(root().querySelector('.bar')).toBeNull();
    await set({ dirty: true });
    expect(root().querySelector('[role=region]')?.getAttribute('aria-label')).toBe('Änderungen speichern');
    expect(root().querySelector('[role=status]')?.textContent).toContain('Ungespeicherte Änderungen');
    expect(buttons()).toEqual(['Verwerfen', 'Speichern']);
  });

  it('shows a save error next to the button as alert', async () => {
    await set({ dirty: true, error: 'Die Datenbank ist schreibgeschützt.' });
    const alert = root().querySelector('[role=alert]')!;
    expect(alert.textContent).toContain('Die Datenbank ist schreibgeschützt.');
    expect(root().querySelector('.bar')?.classList.contains('error')).toBe(true);
  });

  it('names the field and jumps there: scrolls and focuses it', async () => {
    const input = document.createElement('input');
    input.scrollIntoView = vi.fn();
    document.body.append(input);
    await set({ dirty: true, field: { label: 'SMTP-Port', element: () => input } });
    expect(root().querySelector('[role=alert]')?.textContent).toContain('Nicht gespeichert: „SMTP-Port“ prüfen');
    expect(buttons()[0]).toBe('Zum Feld');
    root().querySelector<HTMLElement>('lcm-button')!.click();
    expect(input.scrollIntoView).toHaveBeenCalledOnce();
    expect(document.activeElement).toBe(input);
    input.remove();
  });

  it('reports save and discard', async () => {
    await set({ dirty: true });
    const onSave = vi.fn();
    const onDiscard = vi.fn();
    bar.addEventListener('lcm-save', onSave);
    bar.addEventListener('lcm-discard', onDiscard);
    const [discard, save] = root().querySelectorAll<HTMLElement>('lcm-button');
    save.click();
    discard.click();
    expect(onSave).toHaveBeenCalledOnce();
    expect(onDiscard).toHaveBeenCalledOnce();
  });

  it('can stand in a short card without discard button', async () => {
    await set({ always: true, labels: { ...labels, discard: '', unsaved: '' } });
    expect(buttons()).toEqual(['Speichern']);
    expect(root().querySelector('[role=status]')).toBeNull();
  });
});

describe('unsaved changes', () => {
  it('holds the page while dirty', async () => {
    await set({ dirty: true });
    expect(unsaved.has()).toBe(true);
    await set({ dirty: false });
    expect(unsaved.has()).toBe(false);
  });

  it('lets go when the page disappears', async () => {
    await set({ dirty: true });
    bar.remove();
    expect(unsaved.has()).toBe(false);
  });
});
