import { afterEach, describe, expect, it, vi } from 'vitest';

const loginTwoFactor = vi.fn();

vi.mock('../lib/auth', () => ({
  auth: { loginTwoFactor: (...args: unknown[]) => loginTwoFactor(...args), login: vi.fn(), onChange: () => () => undefined, remediation: null, isAuthenticated: false },
}));
vi.mock('../lib/router', () => ({
  router: { afterLogin: vi.fn().mockResolvedValue(undefined), navigate: vi.fn(), onChange: () => () => undefined, state: { query: new URLSearchParams() } },
}));

await import('./login-page');

afterEach(() => {
  document.body.replaceChildren();
  loginTwoFactor.mockReset();
});

async function twoFactorStep() {
  const page = document.createElement('lcm-page-login') as HTMLElement & { updateComplete: Promise<boolean>; _step: string };
  document.body.append(page);
  await page.updateComplete;
  page._step = 'two-factor';
  await page.updateComplete;
  return { page, input: page.shadowRoot!.querySelector('lcm-input')! };
}

describe('Anmeldung mit zweitem Faktor', () => {
  it('schickt den Code nicht von selbst ab, wenn sechs Ziffern stehen', async () => {
    const { page, input } = await twoFactorStep();
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '123456' }, bubbles: true }));
    await page.updateComplete;
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(loginTwoFactor).not.toHaveBeenCalled();
  });

  it('schickt ihn mit Enter ab', async () => {
    loginTwoFactor.mockResolvedValue(undefined);
    const { page, input } = await twoFactorStep();
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '123456' }, bubbles: true }));
    await page.updateComplete;
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true }));
    await vi.waitFor(() => expect(loginTwoFactor).toHaveBeenCalled());
  });
});
