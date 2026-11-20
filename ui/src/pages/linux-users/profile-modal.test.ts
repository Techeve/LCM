import { describe, expect, it } from 'vitest';
import './profile-modal';
import type { LcmLuProfileModal } from './profile-modal';

describe('lcm-lu-profile-modal', () => {
  it('übernimmt nur Änderungen der Regel-Komponente, nicht die aufsteigenden ihrer Schalter', async () => {
    const modal = document.createElement('lcm-lu-profile-modal') as LcmLuProfileModal;
    modal.open = true;
    document.body.append(modal);
    await modal.updateComplete;
    const rules = modal.renderRoot.querySelector('lcm-lu-profile-rules') as HTMLElement & { updateComplete: Promise<unknown> };
    await rules.updateComplete;

    // Ein Schalter in den Regeln meldet sich selbst - das Ereignis steigt composed bis zum Dialog auf.
    const inner = document.createElement('span');
    (rules.shadowRoot ?? rules).append(inner);
    inner.dispatchEvent(new CustomEvent('lcm-change', { detail: { checked: true }, bubbles: true, composed: true }));
    await modal.updateComplete;
    expect(Object.keys(modal._form)).not.toContain('checked');

    // Die Regel-Komponente selbst meldet einen Teil des Formulars.
    rules.dispatchEvent(new CustomEvent('lcm-change', { detail: { description: 'neu' }, bubbles: true, composed: true }));
    await modal.updateComplete;
    expect(modal._form.description).toBe('neu');
    modal.remove();
  });
});
