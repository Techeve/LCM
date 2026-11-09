/** Kleiner Cron-Auswerter (5 Felder: Minute Stunde Tag Monat Wochentag) für die Vorschau „nächste Läufe“. */

interface FieldSpec {
  min: number;
  max: number;
}

const SPECS: FieldSpec[] = [
  { min: 0, max: 59 },
  { min: 0, max: 23 },
  { min: 1, max: 31 },
  { min: 1, max: 12 },
  { min: 0, max: 7 },
];

function expandPart(part: string, spec: FieldSpec): number[] | null {
  const [range, stepText] = part.split('/');
  const step = stepText === undefined ? 1 : Number(stepText);
  if (!Number.isInteger(step) || step < 1) return null;
  let from = spec.min;
  let to = spec.max;
  if (range !== '*') {
    const [a, b] = range!.split('-');
    from = Number(a);
    to = b === undefined ? (stepText === undefined ? from : spec.max) : Number(b);
  }
  if (!Number.isInteger(from) || !Number.isInteger(to) || from < spec.min || to > spec.max || from > to) return null;
  const out: number[] = [];
  for (let n = from; n <= to; n += step) out.push(n);
  return out;
}

/** Wertemengen je Feld oder null, wenn der Ausdruck ungültig ist. */
export function parseCron(expression: string): Set<number>[] | null {
  const fields = expression.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const sets: Set<number>[] = [];
  for (const [i, field] of fields.entries()) {
    const values = new Set<number>();
    for (const part of field.split(',')) {
      const expanded = expandPart(part, SPECS[i]!);
      if (!expanded) return null;
      expanded.forEach((n) => values.add(i === 4 && n === 7 ? 0 : n));
    }
    sets.push(values);
  }
  return sets;
}

/** Die nächsten `count` Läufe ab `from` (lokale Zeit). */
export function nextRuns(expression: string, from: Date, count: number): Date[] {
  const sets = parseCron(expression);
  if (!sets) return [];
  const [minutes, hours, days, months, weekdays] = sets as [Set<number>, Set<number>, Set<number>, Set<number>, Set<number>];
  const dayRestricted = expression.trim().split(/\s+/)[2] !== '*';
  const weekdayRestricted = expression.trim().split(/\s+/)[4] !== '*';
  const result: Date[] = [];
  const cursor = new Date(from.getTime());
  cursor.setSeconds(0, 0);
  cursor.setMinutes(cursor.getMinutes() + 1);
  const limit = from.getTime() + 400 * 86400000;
  while (result.length < count && cursor.getTime() < limit) {
    const dayOk =
      dayRestricted && weekdayRestricted
        ? days.has(cursor.getDate()) || weekdays.has(cursor.getDay())
        : days.has(cursor.getDate()) && weekdays.has(cursor.getDay());
    if (!months.has(cursor.getMonth() + 1)) {
      cursor.setMonth(cursor.getMonth() + 1, 1);
      cursor.setHours(0, 0, 0, 0);
    } else if (!dayOk) {
      cursor.setDate(cursor.getDate() + 1);
      cursor.setHours(0, 0, 0, 0);
    } else if (!hours.has(cursor.getHours())) {
      cursor.setHours(cursor.getHours() + 1, 0, 0, 0);
    } else if (!minutes.has(cursor.getMinutes())) {
      cursor.setMinutes(cursor.getMinutes() + 1, 0, 0);
    } else {
      result.push(new Date(cursor.getTime()));
      cursor.setMinutes(cursor.getMinutes() + 1);
    }
  }
  return result;
}
