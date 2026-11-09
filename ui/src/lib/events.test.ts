import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EventsClient, HIDDEN_CLOSE_MS, MAX_BACKOFF_MS, POLL_AFTER_FAILURES, POLL_INTERVAL_MS } from './events';

class FakeSocket {
  static all: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  sent: Array<{ type: string; topics: string[] }> = [];
  closed = false;

  constructor(readonly url: string) {
    FakeSocket.all.push(this);
  }
  send(data: string) {
    this.sent.push(JSON.parse(data));
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.();
  }
  receive(message: unknown) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }
  drop(code = 1006) {
    this.onclose?.({ code });
  }
}

const lastSocket = () => FakeSocket.all[FakeSocket.all.length - 1];

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true });
  document.dispatchEvent(new Event('visibilitychange'));
}

// Nach einem Abbruch mit Code 1006 fragt der Client `/auth/me`; ohne Antwort-Vorgabe gilt die Sitzung als gültig.
const fetchMock = vi.fn();
const answer = (status: number) =>
  fetchMock.mockImplementation(async () => new Response('{}', { status, headers: { 'Content-Type': 'application/json' } }));

let client: EventsClient;

/** Lässt den Abruf der Fahrkarte und den folgenden Verbindungsaufbau durchlaufen. */
const settle = () => vi.advanceTimersByTimeAsync(0);

async function start() {
  client.start();
  await settle();
}

beforeEach(() => {
  answer(200);
  vi.stubGlobal('fetch', fetchMock);
  vi.useFakeTimers();
  vi.spyOn(Math, 'random').mockReturnValue(1); // Jitter: volle Wartezeit
  FakeSocket.all = [];
  setVisibility('visible');
  client = new EventsClient((url) => new FakeSocket(url) as unknown as WebSocket, async () => 'T1');
});

afterEach(() => {
  client.stop();
  vi.useRealTimers();
  vi.restoreAllMocks();
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

describe('subscriptions', () => {
  it('connects to the events endpoint of the page origin with a fresh ticket', async () => {
    await start();
    expect(lastSocket().url).toMatch(/^wss?:\/\/[^/]+\/api\/v1\/events\?ticket=T1$/);
  });

  it('ref-counts topics: subscribe once, unsubscribe with the last release', async () => {
    await start();
    lastSocket().open();
    const first = client.subscribe(['servers', 'server:42']);
    const second = client.subscribe(['server:42']);
    expect(lastSocket().sent).toEqual([{ type: 'subscribe', topics: ['servers', 'server:42'] }]);

    first();
    expect(lastSocket().sent.at(-1)).toEqual({ type: 'unsubscribe', topics: ['servers'] });
    second();
    expect(lastSocket().sent.at(-1)).toEqual({ type: 'unsubscribe', topics: ['server:42'] });
    second(); // doppelt freigeben ändert nichts
    expect(lastSocket().sent).toHaveLength(3);
  });

  it('sends all active topics once the connection opens', async () => {
    client.subscribe(['jobs']);
    await start();
    expect(lastSocket().sent).toEqual([]);
    lastSocket().open();
    expect(lastSocket().sent).toEqual([{ type: 'subscribe', topics: ['jobs'] }]);
  });

  it('delivers events and ignores pings and garbage', async () => {
    const received = vi.fn();
    client.onEvent(received);
    await start();
    lastSocket().open();
    lastSocket().receive({ type: 'ping' });
    lastSocket().receive({ type: 'event', event: 'server.status', topic: 'server:42', data: { status: 'red' } });
    lastSocket().receive({ type: 'event', event: 5 });
    lastSocket().onmessage?.({ data: 'not json' });
    expect(received).toHaveBeenCalledOnce();
    expect(received.mock.calls[0][0]).toMatchObject({ event: 'server.status', topic: 'server:42' });
  });
});

describe('reconnect', () => {
  it('backs off exponentially up to the maximum and resyncs once after reconnecting', async () => {
    const resync = vi.fn();
    client.onResync(resync);
    await start();
    lastSocket().open();
    expect(resync).not.toHaveBeenCalled(); // erstes Verbinden braucht keinen Abgleich

    const waits: number[] = [];
    for (let failure = 1; failure < POLL_AFTER_FAILURES; failure++) {
      lastSocket().drop();
      const before = FakeSocket.all.length;
      const expected = Math.min(MAX_BACKOFF_MS, 1000 * 2 ** (failure - 1));
      await vi.advanceTimersByTimeAsync(expected - 1);
      expect(FakeSocket.all).toHaveLength(before);
      await vi.advanceTimersByTimeAsync(1);
      expect(FakeSocket.all).toHaveLength(before + 1);
      waits.push(expected);
    }
    expect(waits.at(-1)).toBe(MAX_BACKOFF_MS);

    lastSocket().open();
    expect(resync).toHaveBeenCalledOnce();
  });

  it('applies jitter to the wait', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    await start();
    lastSocket().open();
    lastSocket().drop();
    const before = FakeSocket.all.length;
    await vi.advanceTimersByTimeAsync(499);
    expect(FakeSocket.all).toHaveLength(before);
    await vi.advanceTimersByTimeAsync(1);
    expect(FakeSocket.all).toHaveLength(before + 1);
  });

  it('asks the store to reload when the server sends resync', async () => {
    const resync = vi.fn();
    client.onResync(resync);
    await start();
    lastSocket().open();
    lastSocket().receive({ type: 'resync' });
    expect(resync).toHaveBeenCalledOnce();
  });

  it('resubscribes after reconnecting', async () => {
    await start();
    lastSocket().open();
    client.subscribe(['dashboard']);
    lastSocket().drop();
    await vi.advanceTimersByTimeAsync(1000);
    lastSocket().open();
    expect(lastSocket().sent).toEqual([{ type: 'subscribe', topics: ['dashboard'] }]);
  });
});

describe('abnormal close (1006)', () => {
  it('asks /auth/me and reports a lost session on 401', async () => {
    answer(401);
    const lost = vi.fn();
    client.onAuthLost(lost);
    await start();
    lastSocket().drop(1006);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/auth/me');
    expect(lost).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(10 * MAX_BACKOFF_MS);
    expect(FakeSocket.all).toHaveLength(1); // stop() hat den Wiederaufbau beendet
  });

  it('keeps retrying while the session is valid', async () => {
    const lost = vi.fn();
    client.onAuthLost(lost);
    await start();
    lastSocket().drop(1006);
    await vi.advanceTimersByTimeAsync(MAX_BACKOFF_MS);
    expect(lost).not.toHaveBeenCalled();
    expect(FakeSocket.all).toHaveLength(2);
  });

  it('ignores network failures of the probe and other close codes', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    const lost = vi.fn();
    client.onAuthLost(lost);
    await start();
    lastSocket().drop(1006);
    await vi.advanceTimersByTimeAsync(0);
    expect(lost).not.toHaveBeenCalled();

    fetchMock.mockClear();
    await vi.advanceTimersByTimeAsync(MAX_BACKOFF_MS);
    lastSocket().drop(1001);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('session and limits', () => {
  it('reports 4401 and does not reconnect', async () => {
    const lost = vi.fn();
    client.onAuthLost(lost);
    await start();
    lastSocket().open();
    lastSocket().drop(4401);
    await vi.advanceTimersByTimeAsync(10 * MAX_BACKOFF_MS);
    expect(lost).toHaveBeenCalledOnce();
    expect(FakeSocket.all).toHaveLength(1);
  });

  it('falls back to slow polling on 4429 and stops once the socket is back', async () => {
    const resync = vi.fn();
    client.onResync(resync);
    await start();
    lastSocket().open();
    lastSocket().drop(4429);

    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(resync).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all).toHaveLength(2); // ein neuer Versuch je Takt
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
    expect(resync).toHaveBeenCalledTimes(2);
  });

  it('falls back to polling after repeated failures', async () => {
    const resync = vi.fn();
    client.onResync(resync);
    await start();
    for (let i = 0; i < POLL_AFTER_FAILURES; i++) {
      lastSocket().drop();
      await vi.advanceTimersByTimeAsync(MAX_BACKOFF_MS);
    }
    const sockets = FakeSocket.all.length;
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS - MAX_BACKOFF_MS - 1);
    expect(FakeSocket.all).toHaveLength(sockets); // kein Backoff mehr
    expect(resync).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(resync).toHaveBeenCalledOnce();
  });

  it('closes on stop and ignores the late close event', async () => {
    await start();
    const socket = lastSocket();
    socket.open();
    client.stop();
    expect(socket.closed).toBe(true);
    socket.drop();
    await vi.advanceTimersByTimeAsync(MAX_BACKOFF_MS);
    expect(FakeSocket.all).toHaveLength(1);
  });
});

describe('hidden tab', () => {
  it('keeps the socket for a short absence', async () => {
    await start();
    const socket = lastSocket();
    socket.open();
    setVisibility('hidden');
    await vi.advanceTimersByTimeAsync(HIDDEN_CLOSE_MS - 1);
    setVisibility('visible');
    await vi.advanceTimersByTimeAsync(HIDDEN_CLOSE_MS);
    expect(socket.closed).toBe(false);
  });

  it('closes after 5 minutes hidden, reconnects and resyncs when visible again', async () => {
    const resync = vi.fn();
    client.onResync(resync);
    await start();
    const socket = lastSocket();
    socket.open();
    client.subscribe(['jobs']);

    setVisibility('hidden');
    await vi.advanceTimersByTimeAsync(HIDDEN_CLOSE_MS);
    expect(socket.closed).toBe(true);
    await vi.advanceTimersByTimeAsync(10 * POLL_INTERVAL_MS);
    expect(FakeSocket.all).toHaveLength(1); // verborgen: keine Wiederverbindung
    expect(resync).not.toHaveBeenCalled();

    setVisibility('visible');
    await settle();
    expect(FakeSocket.all).toHaveLength(2);
    lastSocket().open();
    expect(lastSocket().sent).toEqual([{ type: 'subscribe', topics: ['jobs'] }]);
    expect(resync).toHaveBeenCalledOnce();
  });
});

describe('ticket', () => {
  const ticketReply = (ticket: string) => new Response(JSON.stringify({ ticket }), { status: 200, headers: { 'Content-Type': 'application/json' } });

  function realClient() {
    return new EventsClient((url) => new FakeSocket(url) as unknown as WebSocket);
  }

  it('asks for a fresh ticket before every connection, with the session', async () => {
    let issued = 0;
    fetchMock.mockImplementation(async () => ticketReply(`ticket-${++issued}`));
    const { setSessionToken } = await import('./api');
    setSessionToken('sitzung-1');
    client = realClient();
    await start();

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/events/ticket');
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST' });
    expect(new Headers(fetchMock.mock.calls[0][1].headers).get('X-LCM-Session')).toBe('sitzung-1');
    expect(lastSocket().url).toMatch(/\?ticket=ticket-1$/);

    lastSocket().open();
    lastSocket().drop(1001);
    await vi.advanceTimersByTimeAsync(1000);
    expect(lastSocket().url).toMatch(/\?ticket=ticket-2$/); // nie dieselbe Fahrkarte zweimal
    setSessionToken(null);
  });

  it('treats a rejected ticket as a lost session and does not connect', async () => {
    fetchMock.mockImplementation(async () => new Response('{}', { status: 401, headers: { 'Content-Type': 'application/json' } }));
    const lost = vi.fn();
    client = realClient();
    client.onAuthLost(lost);
    await start();
    expect(lost).toHaveBeenCalledOnce();
    expect(FakeSocket.all).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(10 * MAX_BACKOFF_MS);
    expect(FakeSocket.all).toHaveLength(0);
  });

  it('retries with backoff when the ticket cannot be fetched', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    fetchMock.mockImplementation(async () => ticketReply('spaeter'));
    client = realClient();
    await start();
    expect(FakeSocket.all).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(MAX_BACKOFF_MS);
    expect(FakeSocket.all).toHaveLength(1);
    expect(lastSocket().url).toMatch(/\?ticket=spaeter$/);
  });

  it('does not connect with a ticket that arrives after stop()', async () => {
    let deliver!: (response: Response) => void;
    fetchMock.mockImplementation(() => new Promise<Response>((resolve) => (deliver = resolve)));
    client = realClient();
    client.start();
    client.stop();
    deliver(ticketReply('zu-spaet'));
    await settle();
    expect(FakeSocket.all).toHaveLength(0);
  });
});
