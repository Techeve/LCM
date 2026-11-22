import { afterEach, describe, expect, it, vi } from 'vitest';
import './lcm-modal';
import type { LcmModal } from './lcm-modal';

async function mount(attributes: Record<string, string>): Promise<{ modal: LcmModal; dialog: HTMLDialogElement }> {
  const modal = document.createElement('lcm-modal') as LcmModal;
  for (const [name, value] of Object.entries(attributes)) modal.setAttribute(name, value);
  document.body.append(modal);
  await modal.updateComplete;
  const dialog = modal.shadowRoot!.querySelector('dialog')!;
  return { modal, dialog };
}

afterEach(() => document.body.replaceChildren());

describe('lcm-modal', () => {
  it('öffnet gewöhnlich als modales Fenster', async () => {
    const { modal, dialog } = await mount({});
    const showModal = vi.spyOn(dialog, 'showModal');
    const show = vi.spyOn(dialog, 'show');
    modal.open = true;
    await modal.updateComplete;
    expect(showModal).toHaveBeenCalledTimes(1);
    expect(show).not.toHaveBeenCalled();
  });

  it('öffnet mit non-modal ohne Fokusfalle und Hintergrund, und schließt wieder', async () => {
    const { modal, dialog } = await mount({ 'non-modal': '' });
    const showModal = vi.spyOn(dialog, 'showModal');
    const show = vi.spyOn(dialog, 'show');
    modal.open = true;
    await modal.updateComplete;
    expect(show).toHaveBeenCalledTimes(1);
    expect(showModal).not.toHaveBeenCalled();
    expect(dialog.hasAttribute('data-non-modal')).toBe(true);
    expect(dialog.open).toBe(true);
    modal.open = false;
    await modal.updateComplete;
    expect(dialog.open).toBe(false);
  });
});
