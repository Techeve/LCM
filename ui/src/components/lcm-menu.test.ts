import { describe, expect, it } from 'vitest';
import './lcm-menu';
import type { LcmMenu } from './lcm-menu';

describe('lcm-menu', () => {
  it('setzt die Position der Erklärung über CSSOM', async () => {
    const menu = document.createElement('lcm-menu') as LcmMenu;
    menu.items = [{ id: 'a', label: 'A', tip: 'Erklärung' }];
    document.body.append(menu);
    await menu.updateComplete;
    menu._tip = { text: 'Erklärung', top: 12, side: 'end' };
    await menu.updateComplete;
    const tip = menu.renderRoot.querySelector<HTMLElement>('.tip');
    expect(tip?.style.insetBlockStart).toBe('12px');
  });

  it('nennt die sichtbaren Initialen des Avatars im zugänglichen Namen', async () => {
    const menu = document.createElement('lcm-menu') as LcmMenu;
    menu.label = 'Konto von Administrator';
    menu.avatar = 'AD';
    document.body.append(menu);
    await menu.updateComplete;
    expect(menu.renderRoot.querySelector('.trigger')?.getAttribute('aria-label')).toBe('Konto von Administrator, AD');
  });
});
