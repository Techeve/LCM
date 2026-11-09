import { afterEach, describe, expect, it, vi } from 'vitest';
import './lcm-notice';
import type { LcmNotice } from './lcm-notice';

let notice: LcmNotice;

async function mount(setup: (el: LcmNotice) => void, text = 'Ein Satz Text.'): Promise<LcmNotice> {
  notice = document.createElement('lcm-notice') as LcmNotice;
  setup(notice);
  notice.append(text);
  document.body.append(notice);
  await notice.updateComplete;
  return notice;
}

const box = () => notice.shadowRoot!.querySelector<HTMLElement>('.notice')!;

afterEach(() => notice?.remove());

describe('lcm-notice', () => {
  it('announces critical notices as alert and everything else as status', async () => {
    await mount((el) => (el.tone = 'crit'));
    expect(box().getAttribute('role')).toBe('alert');
    notice.tone = 'warn';
    await notice.updateComplete;
    expect(box().getAttribute('role')).toBe('status');
  });

  it('shows the tone in shape, not only colour', async () => {
    await mount((el) => (el.tone = 'warn'));
    expect(notice.shadowRoot!.querySelector('lcm-ampel')?.getAttribute('level')).toBe('warn');
    notice.tone = 'info';
    await notice.updateComplete;
    expect(notice.shadowRoot!.querySelector('lcm-ampel')).toBeNull();
    expect(notice.shadowRoot!.querySelector('.mark svg')).not.toBeNull();
  });

  it('renders heading and separates it from the text in a band', async () => {
    await mount((el) => {
      el.variant = 'band';
      el.heading = 'Wartung';
    });
    const title = notice.shadowRoot!.querySelector('.title')!;
    expect(title.textContent).toBe('Wartung');
    expect(title.classList.contains('with-text')).toBe(true);
  });

  it('offers at most one follow-up action and reports it', async () => {
    await mount((el) => (el.actionLabel = 'Einstellungen'));
    const onAction = vi.fn();
    notice.addEventListener('lcm-action', onAction);
    const buttons = notice.shadowRoot!.querySelectorAll<HTMLButtonElement>('button.action');
    expect(buttons).toHaveLength(1);
    buttons[0].click();
    expect(onAction).toHaveBeenCalledOnce();
  });

  it('turns the action into a link when href is set', async () => {
    await mount((el) => {
      el.actionLabel = 'Zum Job';
      el.href = '/jobs/7';
    });
    expect(notice.shadowRoot!.querySelector('a.action')?.getAttribute('href')).toBe('/jobs/7');
  });

  it('is only dismissible on request and leaves hiding to the page', async () => {
    await mount(() => {});
    expect(notice.shadowRoot!.querySelector('.x')).toBeNull();
    notice.dismissible = true;
    notice.closeLabel = 'Schließen';
    await notice.updateComplete;
    const onDismiss = vi.fn();
    notice.addEventListener('lcm-dismiss', onDismiss);
    const close = notice.shadowRoot!.querySelector<HTMLButtonElement>('.x')!;
    expect(close.getAttribute('aria-label')).toBe('Schließen');
    close.click();
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(notice.isConnected).toBe(true);
  });
});
