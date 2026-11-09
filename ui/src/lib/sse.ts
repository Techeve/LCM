// Server-Sent Events von Hand lesen: `fetch` liefert den Strom in beliebigen Stücken, und
// `EventSource` scheidet aus, weil es keine Kopfzeile mitschicken kann (siehe api.ts).

export interface SseEvent {
  /** Name aus `event:`; ohne Angabe „message“. */
  event: string;
  data: string;
}

export interface SseParser {
  push(chunk: string): void;
}

/** Zerlegt den Textstrom in Ereignisse; ein Ereignis endet mit einer Leerzeile. Kommentarzeilen (`:`) zählen nicht. */
export function createSseParser(onEvent: (event: SseEvent) => void): SseParser {
  let buffer = '';
  let event = '';
  let data: string[] = [];

  function line(text: string): void {
    if (text === '') {
      if (data.length > 0) onEvent({ event: event || 'message', data: data.join('\n') });
      event = '';
      data = [];
      return;
    }
    if (text.startsWith(':')) return;
    const colon = text.indexOf(':');
    const field = colon === -1 ? text : text.slice(0, colon);
    const value = colon === -1 ? '' : text.slice(colon + 1).replace(/^ /, '');
    if (field === 'event') event = value;
    if (field === 'data') data.push(value);
  }

  return {
    push(chunk: string): void {
      buffer += chunk;
      let end: number;
      while ((end = buffer.indexOf('\n')) !== -1) {
        line(buffer.slice(0, end).replace(/\r$/, ''));
        buffer = buffer.slice(end + 1);
      }
    },
  };
}
