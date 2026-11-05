// Cron-Ausdruck in Worten („Täglich um 03:00“). Erkannt werden die geläufigen Muster;
// alles andere bleibt dem Ausdruck selbst überlassen (`null`).

import { i18n, t } from '../../lib/i18n';

export interface CronDescription {
  key: string;
  params: Record<string, string | number>;
}

const pad = (value: number) => String(value).padStart(2, '0');
const isNumber = (field: string, max: number) => /^\d+$/.test(field) && Number(field) <= max;
const step = (field: string) => /^\*\/([1-9]\d*)$/.exec(field)?.[1];

/** Wochentage eines Feldes wie `1,3,5` oder `1-5`, Sonntag als 0; `null`, wenn das Muster nicht passt. */
export function weekdaysOf(field: string): number[] | null {
  const days = new Set<number>();
  for (const part of field.split(',')) {
    const range = /^([0-7])(?:-([0-7]))?$/.exec(part);
    if (!range) return null;
    const from = Number(range[1]);
    const to = range[2] === undefined ? from : Number(range[2]);
    if (from > to) return null;
    for (let day = from; day <= to; day++) days.add(day % 7);
  }
  return [...days].sort((a, b) => a - b);
}

function weekdayName(day: number): string {
  // 7. Januar 2024 war ein Sonntag.
  return new Intl.DateTimeFormat(i18n.language, { weekday: 'short' }).format(new Date(2024, 0, 7 + day));
}

const WORKDAYS = [1, 2, 3, 4, 5];

export function describeCron(expression: string): CronDescription | null {
  const fields = expression.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const [minute, hour, day, month, weekday] = fields as [string, string, string, string, string];
  if (month !== '*') return null;

  const minuteStep = step(minute);
  if (minuteStep && hour === '*' && day === '*' && weekday === '*') {
    return { key: 'cron.describe.minutes', params: { count: Number(minuteStep) } };
  }
  const hourStep = step(hour);
  if (isNumber(minute, 59) && hourStep && day === '*' && weekday === '*') {
    return { key: 'cron.describe.hours', params: { count: Number(hourStep), minute: pad(Number(minute)) } };
  }
  if (isNumber(minute, 59) && hour === '*' && day === '*' && weekday === '*') {
    return { key: 'cron.describe.hourly', params: { minute: pad(Number(minute)) } };
  }
  if (!isNumber(minute, 59) || !isNumber(hour, 23)) return null;
  const time = `${pad(Number(hour))}:${pad(Number(minute))}`;

  if (day === '*' && weekday === '*') return { key: 'cron.describe.daily', params: { time } };
  if (day === '*') {
    const days = weekdaysOf(weekday);
    if (!days || days.length === 0) return null;
    if (days.length === WORKDAYS.length && days.every((entry, index) => entry === WORKDAYS[index])) {
      return { key: 'cron.describe.workdays', params: { time } };
    }
    return { key: 'cron.describe.weekly', params: { days: days.map(weekdayName).join(', '), time } };
  }
  if (weekday === '*' && isNumber(day, 31) && Number(day) >= 1) {
    return { key: 'cron.describe.monthly', params: { day: Number(day), time } };
  }
  return null;
}

/** Der Ausdruck in Worten in der aktuellen Sprache; leer, wenn kein Muster passt. */
export function cronText(expression: string): string {
  const description = describeCron(expression);
  return description ? t(description.key, description.params) : '';
}
