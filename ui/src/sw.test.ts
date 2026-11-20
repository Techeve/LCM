import { beforeAll, describe, expect, it, vi } from 'vitest';

vi.mock('./lib/broadcast', () => ({ broadcast: vi.fn() }));

const ORIGIN = 'https://lcm.example';

// Caches je Name, in Anlage-Reihenfolge wie `caches.keys()`; Schlüssel ist der Pfad.
const stores = new Map<string, Map<string, string>>();
const listeners = new Map<string, (event: Record<string, unknown>) => void>();

function cacheOf(name: string) {
  if (!stores.has(name)) stores.set(name, new Map());
  const entries = stores.get(name) as Map<string, string>;
  return {
    addAll: async (urls: string[]) => urls.forEach((url) => entries.set(url, `neu ${url}`)),
    match: async (request: Request) => body(entries.get(new URL(request.url).pathname)),
  };
}

const body = (text: string | undefined) => (text === undefined ? undefined : new Response(text));

/** Löst ein Ereignis des Service Workers aus und liefert, worauf er wartet bzw. womit er antwortet. */
function fire(type: string, extra: Record<string, unknown> = {}): Promise<unknown> | undefined {
  let result: Promise<unknown> | undefined;
  const capture = (promise: Promise<unknown>) => (result = promise);
  listeners.get(type)?.({ waitUntil: capture, respondWith: capture, ...extra });
  return result;
}

const get = (path: string) => fire('fetch', { request: new Request(`${ORIGIN}${path}`) }) as Promise<Response> | undefined;

describe('Service Worker nach einem Update', () => {
  beforeAll(async () => {
    stores.set('lcm-aelter', new Map([['/assets/seite-aelter.js', 'ganz alt']]));
    stores.set('lcm-vorher', new Map([['/assets/seite-vorher.js', 'vorher']]));
    vi.stubGlobal('__BUILD_ID__', 'neu');
    vi.stubGlobal('__ASSETS__', ['/assets/index-neu.js']);
    vi.stubGlobal('caches', {
      keys: async () => [...stores.keys()],
      open: async (name: string) => cacheOf(name),
      delete: async (name: string) => stores.delete(name),
      match: async (request: Request) => body([...stores.values()].map((entries) => entries.get(new URL(request.url).pathname)).find((text) => text !== undefined)),
    });
    vi.stubGlobal('fetch', vi.fn(async () => new Response('aus dem Netz', { status: 404 })));
    vi.stubGlobal('self', {
      addEventListener: (type: string, listener: (event: Record<string, unknown>) => void) => listeners.set(type, listener),
      skipWaiting: async () => {},
      clients: { claim: async () => {} },
      location: { origin: ORIGIN },
    });
    await import('./sw');
    await fire('install');
    await fire('activate');
  });

  it('behält den Cache des vorigen Builds und räumt ältere weg', () => {
    expect([...stores.keys()]).toEqual(['lcm-vorher', 'lcm-neu']);
  });

  it('liefert einem Tab mit dem vorigen Build dessen Dateien weiter', async () => {
    expect(await (await get('/assets/seite-vorher.js'))?.text()).toBe('vorher');
  });

  it('liefert die Dateien des neuen Builds aus dem neuen Cache', async () => {
    expect(await (await get('/assets/index-neu.js'))?.text()).toBe('neu /assets/index-neu.js');
  });

  it('lässt Anfragen außerhalb der Build-Dateien ans Netz', () => {
    expect(get('/api/v1/servers')).toBeUndefined();
  });
});
