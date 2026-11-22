import { afterEach, describe, expect, it, vi } from 'vitest';
import { readGlobal } from '../../../api/settings';
import type { Draft } from '../system/form-state';

// Die beiden Bausteine laden sonst ihre Daten; hier zählt nur, ob der Block sie zeichnet.
vi.mock('../endpoints/acme-card', () => ({}));
vi.mock('../endpoints/endpoint-dialog', () => ({}));
vi.mock('../../../lib/auth', () => ({ auth: { can: () => true, onChange: () => () => undefined } }));

const { LcmAddressBlock } = await import('./address-block');

type Block = InstanceType<typeof LcmAddressBlock>;

const BASE: Draft = { address_mode: 'separate', ui_port: '9310', agent_port: '9320', mcp_port: '9330', agent_enabled: true, mcp_enabled: true };

async function mount(draft: Draft = BASE, options: { wizard?: boolean; settings?: Record<string, unknown> } = {}): Promise<Block> {
  const block = new LcmAddressBlock();
  block.wizard = options.wizard ?? false;
  block.draft = draft;
  block.settings = readGlobal(options.settings ?? {});
  document.body.append(block);
  await block.updateComplete;
  return block;
}

const switchOf = (block: Block, service: string) => block.shadowRoot!.querySelector<HTMLElement & { checked: boolean; disabled: boolean }>(`[data-switch="${service}"] lcm-switch`);

afterEach(() => document.body.replaceChildren());

describe('lcm-address-block', () => {
  it('shows gear and Let\'s Encrypt in the settings, access moved into the gear dialog', async () => {
    const block = await mount(BASE);
    const root = block.shadowRoot!;
    expect(root.querySelector('lcm-access-section')).toBeNull();
    expect(root.querySelector('lcm-settings-acme')).not.toBeNull();
    expect(root.querySelector('lcm-settings-proxy')).not.toBeNull();
    expect(root.querySelector('lcm-button[data-endpoint]')).not.toBeNull();
    expect(root.querySelector('lcm-endpoint-dialog')).not.toBeNull();
  });

  it('keeps only mode, addresses and ports in the wizard', async () => {
    const block = await mount(BASE, { wizard: true });
    const root = block.shadowRoot!;
    expect(root.querySelector('lcm-choice')).not.toBeNull();
    expect(root.querySelector('lcm-input[data-key="ui_port"]')).not.toBeNull();
    expect(root.querySelector('lcm-settings-acme')).toBeNull();
    expect(root.querySelector('lcm-settings-proxy')).toBeNull();
    expect(root.querySelector('lcm-explainer')).toBeNull();
    expect(root.querySelector('lcm-button[data-endpoint]')).toBeNull();
    expect(root.querySelector('lcm-endpoint-dialog')).toBeNull();
  });
});

describe('Schalter für Agent und MCP', () => {
  it('stehen in allen drei Modi unter Agent und MCP, die Oberfläche hat keinen', async () => {
    for (const mode of ['separate', 'one_name', 'shared_port']) {
      const block = await mount({ ...BASE, address_mode: mode, public_host: 'lcm.example.com', public_base_url: 'https://lcm.example.com', public_agent_url: 'https://agent.example.com', public_mcp_url: 'https://mcp.example.com' });
      expect(switchOf(block, 'agent'), mode).not.toBeNull();
      expect(switchOf(block, 'mcp'), mode).not.toBeNull();
      expect(block.shadowRoot!.querySelector('[data-switch="ui"]'), mode).toBeNull();
      document.body.replaceChildren();
    }
  });

  it('melden den neuen Stand als Entwurfswert', async () => {
    const block = await mount();
    const seen: Record<string, string | boolean>[] = [];
    block.addEventListener('lcm-address-change', (event) => seen.push((event as CustomEvent<{ values: Record<string, string | boolean> }>).detail.values));
    switchOf(block, 'agent')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { checked: false }, bubbles: true, composed: true }));
    expect(seen).toEqual([{ agent_enabled: false }]);
  });

  it('C ohne Namen: aus, gesperrt, mit Hinweis darunter', async () => {
    const block = await mount({ ...BASE, address_mode: 'shared_port', shared_tls_port: '443', public_base_url: 'https://lcm.example.com', public_agent_url: '', public_mcp_url: 'https://mcp.example.com' });
    const agent = switchOf(block, 'agent')!;
    expect(agent.checked).toBe(false);
    expect(agent.disabled).toBe(true);
    expect(block.shadowRoot!.querySelector('[data-switch-hint="agent"]')?.textContent).toBe('settings.general.address.noDomain');
    expect(switchOf(block, 'mcp')!.disabled).toBe(false);
    expect(block.shadowRoot!.querySelector('[data-switch-hint="mcp"]')).toBeNull();
  });

  it('C: ein Name im leeren Feld schaltet den Dienst im Entwurf ein, ein leeres Feld aus', async () => {
    const block = await mount({ ...BASE, address_mode: 'shared_port', shared_tls_port: '443', public_base_url: 'https://lcm.example.com', public_agent_url: '', agent_enabled: false });
    const seen: Record<string, string | boolean>[] = [];
    block.addEventListener('lcm-address-change', (event) => seen.push((event as CustomEvent<{ values: Record<string, string | boolean> }>).detail.values));
    const input = block.shadowRoot!.querySelector('lcm-input[data-key="public_agent_url"]')!;
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'a' } }));
    block.draft = { ...block.draft, public_agent_url: 'https://a', agent_enabled: true };
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'ab' } }));
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: '' } }));
    expect(seen).toEqual([
      { public_agent_url: 'https://a', agent_enabled: true },
      { public_agent_url: 'https://ab' },
      { public_agent_url: '', agent_enabled: false },
    ]);
  });

  it('der Wechsel in Modus C schaltet Dienste ohne Namen aus', async () => {
    const block = await mount({ ...BASE, public_base_url: 'https://lcm.example.com' });
    const seen: Record<string, string | boolean>[] = [];
    block.addEventListener('lcm-address-change', (event) => seen.push((event as CustomEvent<{ values: Record<string, string | boolean> }>).detail.values));
    block.shadowRoot!.querySelector('lcm-choice')!.dispatchEvent(new CustomEvent('lcm-change', { detail: { value: 'shared_port' } }));
    expect(seen[0]).toMatchObject({ address_mode: 'shared_port', agent_enabled: false, mcp_enabled: false });
  });

  it('die config.json sperrt den Agent-Schalter mit Hinweis', async () => {
    const block = await mount(BASE, { settings: { address_locks: { agent_enabled: true } } });
    expect(switchOf(block, 'agent')!.disabled).toBe(true);
    expect(block.shadowRoot!.querySelector('[data-switch-hint="agent"]')?.textContent).toBe('settings.general.address.locked');
    expect(switchOf(block, 'mcp')!.disabled).toBe(false);
  });

  it('warnt vor dem Ausschalten des Agenten, den Server nutzen', async () => {
    const block = await mount({ ...BASE, agent_enabled: false }, { settings: { agent_enabled: true, agent_servers: 3 } });
    expect(block.shadowRoot!.querySelector('[data-field="agent-offline"]')?.textContent).toBe('settings.service.agent.offline');
    document.body.replaceChildren();
    const quiet = await mount(BASE, { settings: { agent_enabled: true, agent_servers: 3 } });
    expect(quiet.shadowRoot!.querySelector('[data-field="agent-offline"]')).toBeNull();
  });
});

describe('Adressen in Modus B', () => {
  const draft = { ...BASE, address_mode: 'one_name', public_host: 'lcm.example.com' };

  it('hat je Adresse einen Kopierknopf', async () => {
    const block = await mount(draft);
    const fields = [...block.shadowRoot!.querySelectorAll<HTMLElement & { value: string }>('lcm-copy-field')];
    expect(fields.map((field) => field.value)).toEqual(['https://lcm.example.com:9310', 'https://lcm.example.com:9320', 'https://lcm.example.com:9330']);
  });

  it('nennt Agent und MCP „(nicht aktiv)“, solange ihr Schalter aus ist', async () => {
    const block = await mount({ ...draft, agent_enabled: false, mcp_enabled: true });
    const root = block.shadowRoot!;
    expect(root.querySelector('[data-field="inactive-agent"]')?.textContent).toBe('settings.general.address.inactive');
    expect(root.querySelector('[data-field="inactive-mcp"]')).toBeNull();
    expect(root.querySelector('[data-field="inactive-ui"]')).toBeNull();
  });
});

describe('Adressen in Modus B im Assistenten', () => {
  it('stehen als Text da, ohne Kopierknöpfe, jede Zeile mit drei Zellen', async () => {
    const block = await mount({ ...BASE, address_mode: 'one_name', public_host: 'lcm.example.com' }, { wizard: true });
    const root = block.shadowRoot!;
    expect(root.querySelector('lcm-copy-field')).toBeNull();
    expect([...root.querySelectorAll('.url')].map((url) => url.textContent)).toEqual(['https://lcm.example.com:9310', 'https://lcm.example.com:9320', 'https://lcm.example.com:9330']);
    for (const row of root.querySelectorAll('ul.derived > li')) expect(row.children).toHaveLength(3);
  });
});

describe('Modus A: nur der Name', () => {
  const draft = { ...BASE, public_base_url: 'https://lcm.example.com:9310', public_agent_url: '', public_mcp_url: '' };
  const field = (block: Block, key: string) => block.shadowRoot!.querySelector<HTMLElement & { value: string }>(`lcm-input[data-key="${key}"]`)!;

  it('zeigt die gespeicherte Adresse als name:port', async () => {
    const block = await mount(draft);
    expect(field(block, 'public_base_url').value).toBe('lcm.example.com:9310');
    expect(field(block, 'public_base_url').getAttribute('placeholder')).toBe('lcm.example.com');
  });

  it('macht aus der Eingabe https://name[:port] für den Entwurf, ohne den Port 443', async () => {
    const block = await mount(draft);
    const seen: Record<string, string | boolean>[] = [];
    block.addEventListener('lcm-address-change', (event) => seen.push((event as CustomEvent<{ values: Record<string, string | boolean> }>).detail.values));
    const input = field(block, 'public_agent_url');
    for (const value of ['agent.example.com', 'agent.example.com:8443', 'agent.example.com:443', 'https://agent.example.com/x', '']) input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value } }));
    expect(seen.map((values) => values.public_agent_url)).toEqual(['https://agent.example.com', 'https://agent.example.com:8443', 'https://agent.example.com', 'https://agent.example.com', '']);
  });

  it('lässt beim Tippen „:443“ stehen, obwohl die Adresse den Port nicht trägt', async () => {
    const block = await mount(draft);
    const input = field(block, 'public_agent_url');
    input.dispatchEvent(new CustomEvent('lcm-input', { detail: { value: 'agent.example.com:443' } }));
    block.draft = { ...block.draft, public_agent_url: 'https://agent.example.com' };
    await block.updateComplete;
    expect(field(block, 'public_agent_url').value).toBe('agent.example.com:443');
  });
});
