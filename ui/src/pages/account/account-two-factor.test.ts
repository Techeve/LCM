import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const enableTotp = vi.fn();

vi.mock('../../api/account', async (original) => ({
  ...(await original<typeof import('../../api/account')>()),
  startTotpSetup: vi.fn().mockResolvedValue({ secret: 'ABCDEFGHIJKLMNOP', qr_code: '' }),
  enableTotp: (code: string) => enableTotp(code),
}));
vi.mock('../../lib/auth', () => ({
  auth: { user: { totp_enabled: false }, remediation: null, onChange: () => () => undefined, refresh: vi.fn().mockResolvedValue(undefined) },
}));
vi.mock('../../lib/router', () => ({ router: { navigate: vi.fn(), reevaluate: vi.fn() } }));

await import('./account-two-factor');

async function setup() {
  const card = document.createElement('lcm-account-two-factor') as HTMLElement & { updateComplete: Promise<boolean> };
  document.body.append(card);
  await card.updateComplete;
  card.shadowRoot!.querySelector<HTMLElement>('lcm-button[variant="primary"]')!.click();
  await vi.waitFor(() => expect(card.shadowRoot!.querySelector('lcm-input.code')).not.toBeNull());
  await card.updateComplete;
  return { card, input: card.shadowRoot!.querySelector('lcm-input.code')! };
}

beforeEach(() => enableTotp.mockReset().mockResolvedValue(undefined));
afterEach(() => document.body.replaceChildren());

describe('2FA einrichten', () => {
  it('schickt den Code nicht von selbst ab, wenn sechs Ziffern stehen', async () => {
    const { card, input } = await setup();
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '123456' }, bubbles: true }));
    await card.updateComplete;
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(enableTotp).not.toHaveBeenCalled();
  });

  it('schickt ihn mit Enter ab, aber erst bei sechs Ziffern', async () => {
    const { card, input } = await setup();
    const enter = () => input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true }));
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '123' }, bubbles: true }));
    enter();
    expect(enableTotp).not.toHaveBeenCalled();
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '123456' }, bubbles: true }));
    await card.updateComplete;
    enter();
    await vi.waitFor(() => expect(enableTotp).toHaveBeenCalledWith('123456'));
  });

  it('schickt ihn mit dem Knopf ab', async () => {
    const { card, input } = await setup();
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '654321' }, bubbles: true }));
    await card.updateComplete;
    const buttons = [...card.shadowRoot!.querySelectorAll<HTMLElement>('lcm-button[variant="primary"]')];
    buttons.at(-1)!.click();
    await vi.waitFor(() => expect(enableTotp).toHaveBeenCalledWith('654321'));
  });
});
