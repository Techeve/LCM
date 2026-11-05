// Kanal zwischen den Tabs. Es gibt genau zwei Meldungen, Daten laufen hier nie.
// Auch der Service Worker sendet über diesen Kanal, deshalb darf das Modul
// nichts aus dem DOM voraussetzen.

export const CHANNEL_NAME = 'lcm';

export type BroadcastMessage = { type: 'logout' } | { type: 'new-version' };

let channel: BroadcastChannel | undefined;

function open(): BroadcastChannel {
  channel ??= new BroadcastChannel(CHANNEL_NAME);
  return channel;
}

function isMessage(data: unknown): data is BroadcastMessage {
  const type = (data as { type?: unknown } | null)?.type;
  return type === 'logout' || type === 'new-version';
}

/** Sendet an alle anderen Tabs; der eigene Kontext bekommt die Meldung nicht. */
export function broadcast(message: BroadcastMessage): void {
  open().postMessage(message);
}

export function onBroadcast(handler: (message: BroadcastMessage) => void): () => void {
  const listener = (event: MessageEvent) => {
    if (isMessage(event.data)) handler(event.data);
  };
  open().addEventListener('message', listener);
  return () => open().removeEventListener('message', listener);
}
