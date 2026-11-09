// Ereignisse: Filter aus der Adresse und das Anhängen neuer Logzeilen aus dem Live-Strom.

import type { LogEntry } from '../../../api/settings-more';

export const LEVELS = ['debug', 'info', 'warn', 'error'] as const;

/** Mehr ist in einer Ansicht nicht zu lesen; das Backend deckelt ebenfalls. */
export const MAX_LINES = 2000;

export interface LogFilter {
  level: string;
  query: string;
  follow: boolean;
}

export function parseLogFilter(query: URLSearchParams): LogFilter {
  const level = query.get('level') ?? '';
  return {
    level: (LEVELS as readonly string[]).includes(level) ? level : '',
    query: query.get('q') ?? '',
    follow: query.get('follow') === '1',
  };
}

export function appendLines(lines: LogEntry[], added: LogEntry[]): LogEntry[] {
  const next = lines.concat(added);
  return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
}

export const LEVEL_TONES: Record<string, 'crit' | 'warn' | 'info' | 'neutral'> = { ERROR: 'crit', WARN: 'warn', INFO: 'info', DEBUG: 'neutral' };

export function levelTone(level: string): 'crit' | 'warn' | 'info' | 'neutral' {
  return LEVEL_TONES[level.toUpperCase()] ?? 'neutral';
}

const clocks = new Map<string, Intl.DateTimeFormat>();

/** Uhrzeit einer Logzeile mit Sekunden; `lib/format` kennt nur Datum mit Uhrzeit. */
export function formatClock(value: string, language: string): string {
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return value;
  let clock = clocks.get(language);
  if (!clock) {
    clock = new Intl.DateTimeFormat(language, { timeStyle: 'medium' });
    clocks.set(language, clock);
  }
  return clock.format(time);
}

/**
 * Text einer Zeile: die Meldung samt ihren Feldern (`request method=GET path=…`). `msg` allein wäre oft nur
 * „request“; Zeit und Stufe stehen schon in eigenen Spalten.
 */
export function lineText(entry: LogEntry): string {
  const rest = entry.raw.replace(/^time=\S+\s+level=\S+\s+/, '');
  if (rest === entry.raw) return entry.msg || entry.raw;
  return rest.replace(/^msg=("((?:[^"\\]|\\.)*)"|\S+)/, (_, whole: string, quoted?: string) => quoted ?? whole);
}
