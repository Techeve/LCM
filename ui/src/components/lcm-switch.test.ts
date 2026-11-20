import { describe, expect, it } from 'vitest';
import './lcm-switch';
import type { LcmSwitch } from './lcm-switch';

async function mount(checked: boolean): Promise<{ box: LcmSwitch; button: HTMLButtonElement; changes: boolean[] }> {
  const box = document.createElement('lcm-switch') as LcmSwitch;
  box.checked = checked;
  const changes: boolean[] = [];
  box.addEventListener('lcm-change', (e) => changes.push((e as CustomEvent<{ checked: boolean }>).detail.checked));
  document.body.append(box);
  await box.updateComplete;
  return { box, button: box.renderRoot.querySelector('button') as HTMLButtonElement, changes };
}

describe('lcm-switch', () => {
  it('meldet den gewünschten Stand, ohne selbst umzuschalten', async () => {
    const { box, button, changes } = await mount(false);
    button.click();
    await box.updateComplete;
    expect(changes).toEqual([true]);
    expect(box.checked).toBe(false);
    expect(button.getAttribute('aria-checked')).toBe('false');
  });

  it('meldet beim zweiten Klick wieder dasselbe, solange der Aufrufer nichts übernimmt', async () => {
    const { box, button, changes } = await mount(true);
    button.click();
    button.click();
    await box.updateComplete;
    expect(changes).toEqual([false, false]);
  });

  it('zeigt den Stand, den der Aufrufer setzt', async () => {
    const { box, button } = await mount(false);
    box.addEventListener('lcm-change', (e) => (box.checked = (e as CustomEvent<{ checked: boolean }>).detail.checked));
    button.click();
    await box.updateComplete;
    expect(button.getAttribute('aria-checked')).toBe('true');
  });
});
