import { afterEach, describe, expect, it, vi } from 'vitest';
import './session-mode-field';
import type { SessionModeField } from './session-mode-field';
import type { LcmChoice } from '../../components/lcm-choice';
import { i18n } from '../../lib/i18n';

let field: SessionModeField;

async function mount(setup: (el: SessionModeField) => void): Promise<SessionModeField> {
  await i18n.setLanguage('de');
  field = document.createElement('lcm-session-mode-field') as SessionModeField;
  setup(field);
  document.body.append(field);
  await field.updateComplete;
  const choice = field.shadowRoot!.querySelector<LcmChoice>('lcm-choice')!;
  await choice.updateComplete;
  return field;
}

const choice = () => field.shadowRoot!.querySelector<LcmChoice>('lcm-choice')!;
const radios = () => [...choice().shadowRoot!.querySelectorAll<HTMLInputElement>('input[type=radio]')];
const notes = () => [...field.shadowRoot!.querySelectorAll('p')].map((p) => p.textContent?.trim());

afterEach(() => field?.remove());

describe('lcm-session-mode-field', () => {
  it('offers both ways stacked, each with title and text, memory first', async () => {
    await mount((el) => (el.cookieAvailable = true));
    expect(choice().stacked).toBe(true);
    const labels = [...choice().shadowRoot!.querySelectorAll('label')].map((label) => ({
      title: label.querySelector('.title')?.textContent,
      text: label.querySelector('.text')?.textContent,
    }));
    expect(labels.map((label) => label.title)).toEqual([
      'Nur im Arbeitsspeicher (sicherste Variante, Standard)',
      'Sitzungs-Cookie (bequemer, nur mit HTTPS)',
    ]);
    expect(labels[0].text).toContain('nur für diesen einen Tab');
    expect(labels[1].text).toContain('übersteht das Neuladen');
  });

  it('marks the saved way and says that switching signs everyone out', async () => {
    await mount((el) => {
      el.value = 'memory';
      el.cookieAvailable = true;
    });
    expect(radios().map((radio) => radio.checked)).toEqual([true, false]);
    expect(notes()).toEqual(['Beim Wechsel werden alle Benutzer abgemeldet.']);
  });

  it('disables the cookie way over HTTP and says why, visibly', async () => {
    await mount((el) => (el.cookieAvailable = false));
    expect(radios().map((radio) => radio.disabled)).toEqual([false, true]);
    expect(notes()).toContain('Nur verfügbar, wenn LCM über HTTPS aufgerufen wird.');
  });

  it('reports the choice as lcm-change from the selection inside', async () => {
    await mount((el) => (el.cookieAvailable = true));
    const onChange = vi.fn();
    field.addEventListener('lcm-change', (event) => onChange((event as CustomEvent<{ value: string }>).detail.value));
    radios()[1].click();
    expect(onChange).toHaveBeenCalledOnce();
    expect(onChange).toHaveBeenCalledWith('cookie');
  });
});
