import { describe, expect, it } from 'vitest';
import './lcm-select';
import type { LcmSelect } from './lcm-select';

const OPTIONS = [
  { value: 'a', label: 'A' },
  { value: 'b', label: 'B' },
];

async function mount(): Promise<{ box: LcmSelect; select: HTMLSelectElement; changes: string[] }> {
  const box = document.createElement('lcm-select') as LcmSelect;
  box.options = OPTIONS;
  box.value = 'a';
  const changes: string[] = [];
  box.addEventListener('lcm-change', (e) => changes.push((e as CustomEvent<{ value: string }>).detail.value));
  document.body.append(box);
  await box.updateComplete;
  return { box, select: box.renderRoot.querySelector('select') as HTMLSelectElement, changes };
}

async function choose(box: LcmSelect, select: HTMLSelectElement, value: string): Promise<void> {
  select.value = value;
  select.dispatchEvent(new Event('change'));
  await box.updateComplete;
}

describe('lcm-select', () => {
  it('meldet die Wahl, ohne den eigenen Wert zu setzen', async () => {
    const { box, select, changes } = await mount();
    await choose(box, select, 'b');
    expect(changes).toEqual(['b']);
    expect(box.value).toBe('a');
  });

  it('springt zurück, wenn der Aufrufer die Wahl nicht übernimmt', async () => {
    const { box, select } = await mount();
    await choose(box, select, 'b');
    expect(select.value).toBe('a');
  });

  it('zeigt den Wert, den der Aufrufer übernimmt', async () => {
    const { box, select } = await mount();
    box.addEventListener('lcm-change', (e) => (box.value = (e as CustomEvent<{ value: string }>).detail.value));
    await choose(box, select, 'b');
    expect(box.value).toBe('b');
    expect(select.value).toBe('b');
  });

  it('folgt einem neuen Wert von außen', async () => {
    const { box, select } = await mount();
    box.value = 'b';
    await box.updateComplete;
    expect(select.value).toBe('b');
  });
});
