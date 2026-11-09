import { describe, expect, it } from 'vitest';
import { createSseParser, type SseEvent } from './sse';

function parse(...chunks: string[]): SseEvent[] {
  const events: SseEvent[] = [];
  const parser = createSseParser((event) => events.push(event));
  chunks.forEach((chunk) => parser.push(chunk));
  return events;
}

describe('createSseParser', () => {
  it('reads named events with data', () => {
    expect(parse('event: log\ndata: {"x":1}\n\n')).toEqual([{ event: 'log', data: '{"x":1}' }]);
  });

  it('defaults the name to message and joins several data lines', () => {
    expect(parse('data: a\ndata: b\n\n')).toEqual([{ event: 'message', data: 'a\nb' }]);
  });

  it('handles chunks that cut a line or the blank line in half', () => {
    expect(parse('event: lo', 'g\nda', 'ta: 1\n', '\n', 'data: 2\n\n')).toEqual([
      { event: 'log', data: '1' },
      { event: 'message', data: '2' },
    ]);
  });

  it('ignores comments and events without data, and accepts CRLF', () => {
    expect(parse(': ping\n\nevent: leer\n\ndata: ok\r\n\r\n')).toEqual([{ event: 'message', data: 'ok' }]);
  });

  it('does not emit an incomplete event', () => {
    expect(parse('event: log\ndata: 1\n')).toEqual([]);
  });
});
