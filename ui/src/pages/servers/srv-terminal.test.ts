import { afterEach, describe, expect, it, vi } from 'vitest';

const terminalTicket = vi.hoisted(() => vi.fn());
const newTerminal = vi.hoisted(() => vi.fn());

vi.mock('@xterm/xterm', () => ({ Terminal: newTerminal }));
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class {} }));
vi.mock('@xterm/xterm/css/xterm.css?inline', () => ({ default: '' }));
vi.mock('../../api/servers', () => ({ serversApi: { terminalTicket } }));

import './srv-terminal';
import type { SrvTerminal } from './srv-terminal';
import type { LcmButton } from '../../components/lcm-button';
import { i18n } from '../../lib/i18n';

let terminal: SrvTerminal;

afterEach(() => {
  terminal?.remove();
  vi.unstubAllGlobals();
  terminalTicket.mockReset();
  newTerminal.mockReset();
});

describe('lcm-srv-terminal', () => {
  it('starts no session when the dialog is closed while the ticket is pending', async () => {
    await i18n.setLanguage('de');
    const sockets = vi.fn();
    vi.stubGlobal('WebSocket', sockets);
    let resolveTicket!: (value: { ticket: string }) => void;
    terminalTicket.mockReturnValue(new Promise((resolve) => (resolveTicket = resolve)));

    terminal = document.createElement('lcm-srv-terminal') as SrvTerminal;
    document.body.append(terminal);
    await terminal.updateComplete;
    terminal.shadowRoot!.querySelector<LcmButton>('lcm-button[variant=primary]')!.click();
    terminal.dispatchEvent(new CustomEvent('noop'));
    // Schließen über die Kopfzeile, solange die Fahrkarte noch aussteht.
    terminal.shadowRoot!.querySelector<HTMLButtonElement>('button.close')!.click();
    resolveTicket({ ticket: 'abc' });
    await Promise.resolve();
    await terminal.updateComplete;

    expect(newTerminal).not.toHaveBeenCalled();
    expect(sockets).not.toHaveBeenCalled();
  });
});
