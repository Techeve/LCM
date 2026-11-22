import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readAcmeState, type AcmeState } from '../../../api/endpoints';

const updateAcme = vi.fn();
const issueAcme = vi.fn();
/** Der Datenspeicher der zuletzt gebauten Karte. */
const store = vi.hoisted(() => ({ current: { data: undefined } as { data: unknown } }));

vi.mock('../../../api/endpoints', async (original) => ({
  ...(await original<typeof import('../../../api/endpoints')>()),
  endpointsApi: { updateAcme: (body: unknown) => updateAcme(body), issueAcme: () => issueAcme() },
}));
// Der Datenspeicher liefert hier, was der Test vorgibt; `reload` holt nichts, der Test setzt den neuen Stand selbst.
vi.mock('../../../lib/live-data', () => ({
  LiveData: class {
    data: unknown;
    error = '';
    constructor() {
      store.current = this;
    }
    reload(): void {}
  },
}));

const { LcmSettingsAcme } = await import('./acme-card');

const NAMES = ['lcm.example.org'];
const CERTIFICATE = { id: '1', names: NAMES, issuer: 'R11', not_after: '2999-01-01T00:00:00Z', fingerprint: 'ab:cd', renew_at: '2998-12-01T00:00:00Z' };

function stateOf(fields: Record<string, unknown> = {}): AcmeState {
  return readAcmeState({ enabled: false, directory_url: 'https://acme-v02.api.letsencrypt.org/directory', email: '', challenge: 'http-01', terms_accepted: false, running: false, certificates: [], ...fields });
}

type Card = InstanceType<typeof LcmSettingsAcme>;
const root = (card: Card) => card.shadowRoot!;
const requestButton = (card: Card) => root(card).querySelector<HTMLElement>('lcm-button[data-action="request"]');
const emailField = (card: Card) => root(card).querySelector<HTMLElement & { value: string }>('lcm-input[data-field="email"]')!;

async function mount(names = NAMES): Promise<Card> {
  const card = new LcmSettingsAcme();
  card.wizard = true;
  card.open = true;
  card.names = names;
  store.current.data = stateOf();
  document.body.append(card);
  await card.updateComplete;
  return card;
}

/** Wie der Server nach dem Speichern: der neue Stand kommt in den Datenspeicher, und die Karte zeichnet neu. */
async function receive(card: Card, fields: Record<string, unknown>): Promise<void> {
  store.current.data = stateOf(fields);
  card.requestUpdate();
  await card.updateComplete;
}

async function type(card: Card, value: string): Promise<void> {
  emailField(card).dispatchEvent(new CustomEvent('lcm-input', { detail: { value } }));
  root(card)
    .querySelector('lcm-switch[data-field="terms"]')!
    .dispatchEvent(new CustomEvent('lcm-change', { detail: { checked: true } }));
  await card.updateComplete;
}

const SAVED = { enabled: true, email: 'admin@example.com', terms_accepted: true, names: NAMES };

beforeEach(() => {
  updateAcme.mockReset().mockResolvedValue(stateOf(SAVED));
  issueAcme.mockReset().mockResolvedValue(undefined);
});
afterEach(() => document.body.replaceChildren());

describe('Let\'s Encrypt im Assistenten: Zertifikat anfordern', () => {
  it('speichert die Eingaben und fordert an', async () => {
    const card = await mount();
    await type(card, 'admin@example.com');
    requestButton(card)!.click();
    await vi.waitFor(() => expect(issueAcme).toHaveBeenCalledOnce());
    expect(updateAcme).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, email: 'admin@example.com', accept_terms: true, names: NAMES }));
  });

  it('behält die Eingaben, auch wenn der Datenspeicher noch den alten Stand hält und erst später den neuen', async () => {
    const card = await mount();
    await type(card, 'admin@example.com');
    requestButton(card)!.click();
    await vi.waitFor(() => expect(issueAcme).toHaveBeenCalledOnce());
    await card.updateComplete;
    // Der Datenspeicher hält noch den Stand vor dem Speichern: Ein Neuzeichnen darf ihn nicht zurückholen.
    expect(emailField(card).value).toBe('admin@example.com');

    await receive(card, { ...SAVED, running: true });
    expect(emailField(card).value).toBe('admin@example.com');
    await receive(card, { ...SAVED, running: true, last_run_at: '2026-11-22T10:00:00Z' });
    expect(emailField(card).value).toBe('admin@example.com');
    expect((card as unknown as { _draft: { enabled: boolean } })._draft.enabled).toBe(true);
  });

  it('der Knopf ist nach dem Ausstellen weg und die Erfolgsmeldung steht da', async () => {
    const card = await mount();
    await type(card, 'admin@example.com');
    requestButton(card)!.click();
    await vi.waitFor(() => expect(issueAcme).toHaveBeenCalledOnce());
    await receive(card, { ...SAVED, certificates: [CERTIFICATE] });
    expect(requestButton(card)).toBeNull();
    expect(root(card).querySelector('[data-field="acme-done"]')).not.toBeNull();
    expect(emailField(card).value).toBe('admin@example.com');
  });

  it('der Knopf bleibt auch nach einer Änderung der Eingaben weg, erst andere Namen bringen ihn zurück', async () => {
    const card = await mount();
    await receive(card, { ...SAVED, certificates: [CERTIFICATE] });
    expect(requestButton(card)).toBeNull();

    emailField(card).dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'andere@example.com' } }));
    await card.updateComplete;
    expect(requestButton(card)).toBeNull();

    card.names = [...NAMES, 'agent.example.org'];
    await card.updateComplete;
    expect(requestButton(card)).not.toBeNull();
  });
});
