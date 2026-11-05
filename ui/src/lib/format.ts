// Datum, Zahlen, Größen und Zeiten in der gewählten Sprache, ausschließlich über
// Intl. Die Formatierer werden je Sprache einmal gebaut, das ist teuer.

import { i18n } from './i18n';

const NO_VALUE = '–';
const formatters = new Map<string, unknown>();

function formatter<T>(kind: string, create: (language: string) => T): T {
  const language = i18n.language;
  const key = `${language}|${kind}`;
  if (!formatters.has(key)) formatters.set(key, create(language));
  return formatters.get(key) as T;
}

function toTime(value: Date | string | number | null | undefined): number {
  if (value === null || value === undefined) return NaN;
  return new Date(value).getTime();
}

export function formatDate(value: Date | string | number | null | undefined): string {
  const time = toTime(value);
  if (Number.isNaN(time)) return NO_VALUE;
  return formatter('date', (l) => new Intl.DateTimeFormat(l, { dateStyle: 'medium' })).format(time);
}

export function formatDateTime(value: Date | string | number | null | undefined): string {
  const time = toTime(value);
  if (Number.isNaN(time)) return NO_VALUE;
  return formatter('datetime', (l) => new Intl.DateTimeFormat(l, { dateStyle: 'medium', timeStyle: 'short' })).format(time);
}

export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return formatter(`number${JSON.stringify(options ?? {})}`, (l) => new Intl.NumberFormat(l, options)).format(value);
}

// Jeder Schritt: Einheit und wie viele davon in die nächstgrößere gehen.
const RELATIVE_STEPS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['second', 60],
  ['minute', 60],
  ['hour', 24],
  ['day', 30],
  ['month', 12],
];

/** „vor 3 Minuten“ / „in 2 Stunden“; `now` nur für Tests. */
export function formatRelative(value: Date | string | number | null | undefined, now: number = Date.now()): string {
  const time = toTime(value);
  if (Number.isNaN(time)) return NO_VALUE;
  const format = formatter('relative', (l) => new Intl.RelativeTimeFormat(l, { numeric: 'auto' }));
  let amount = (time - now) / 1000;
  for (const [unit, step] of RELATIVE_STEPS) {
    if (Math.abs(amount) < step) return format.format(Math.round(amount) || 0, unit);
    amount /= step;
  }
  return format.format(Math.round(amount), 'year');
}

const BYTE_UNITS = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte', 'petabyte'];

/** Größen mit Basis 1024 und den kurzen Einheitennamen der Sprache. */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes)) return NO_VALUE;
  let amount = Math.abs(bytes);
  let index = 0;
  while (amount >= 1024 && index < BYTE_UNITS.length - 1) {
    amount /= 1024;
    index += 1;
  }
  const unit = BYTE_UNITS[index];
  const digits = index === 0 ? 0 : 1;
  const format = formatter(
    `bytes-${unit}`,
    (l) => new Intl.NumberFormat(l, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: digits }),
  );
  return format.format(bytes < 0 ? -amount : amount);
}

const DURATION_UNITS: Array<[string, number]> = [
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
  ['second', 1],
];

/** Dauer in Sekunden mit den zwei größten Einheiten, z. B. „1 Std. 5 Min.“. */
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) return NO_VALUE;
  let rest = Math.max(0, Math.round(seconds));
  const amounts = DURATION_UNITS.map(([unit, size]) => {
    const amount = Math.floor(rest / size);
    rest -= amount * size;
    return { unit, amount };
  });
  const first = amounts.findIndex(({ amount }) => amount > 0);
  if (first < 0) return formatUnit('second', 0);
  const parts = amounts
    .slice(first, first + 2)
    .filter(({ amount }) => amount > 0)
    .map(({ unit, amount }) => formatUnit(unit, amount));
  return formatter('list', (l) => new Intl.ListFormat(l, { style: 'narrow', type: 'unit' })).format(parts);
}

function formatUnit(unit: string, amount: number): string {
  return formatter(
    `unit-${unit}`,
    (l) => new Intl.NumberFormat(l, { style: 'unit', unit, unitDisplay: 'short' }),
  ).format(amount);
}
