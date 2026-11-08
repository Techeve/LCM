// Anzeigezustand des Abos: Ampel und Text je Vertragsstatus, Warnschwelle für das Ablaufdatum.

import type { StatusLevel } from '../../../components/shared';
import type { AptChannel, AptChannels, Subscription } from '../../../api/settings';

/** Ab so vielen Resttagen warnt die Ampel. */
export const EXPIRY_WARNING_DAYS = 30;

export interface StateView {
  level: StatusLevel;
  /** Übersetzungsschlüssel; `days` ist als Parameter gedacht. */
  key: string;
}

export function subscriptionState(subscription: Subscription): StateView {
  switch (subscription.status) {
    case 'active':
      return subscription.daysLeft !== null && subscription.daysLeft <= EXPIRY_WARNING_DAYS
        ? { level: 'warn', key: 'settings.subscription.state.expiring' }
        : { level: 'great', key: 'settings.subscription.state.active' };
    case 'expired':
      return { level: 'crit', key: 'settings.subscription.state.expired' };
    case 'revoked':
      return { level: 'crit', key: 'settings.subscription.state.revoked' };
    case 'unreachable':
      return { level: 'unknown', key: 'settings.subscription.state.unreachable' };
    default:
      return { level: 'unknown', key: 'settings.subscription.state.unknown' };
  }
}

/** Major- und Feature-Nummer einer Version („2.0.0~beta.12“ → [2, 0]); unlesbar gilt als 0.0. */
function featureOf(version: string): [number, number] {
  const parts = version.replace(/^v/, '').split(/[-~]/)[0].split('.');
  // Wie im Backend: Ist ein Teil keine Zahl, gilt die ganze Version als 0.0.
  if (parts.some((part) => !/^\d+$/.test(part))) return [0, 0];
  return [Number(parts[0]), Number(parts[1] ?? 0)];
}

/** Liegt target bei Major- oder Feature-Nummer hinter installed? Patch und Vorabversion zählen nicht. */
export function featureBehind(target: string, installed: string): boolean {
  const [tMajor, tMinor] = featureOf(target);
  const [iMajor, iMinor] = featureOf(installed);
  return tMajor !== iMajor ? tMajor < iMajor : tMinor < iMinor;
}

/** Die Feature-Version zum Anzeigen: „2.0“. */
export function featureLabel(version: string): string {
  return featureOf(version).join('.');
}

/** apt schreibt Vorabversionen mit Tilde (2.0.0~beta.12), die Oberfläche zeigt sie wie überall mit Bindestrich. */
export function displayVersion(version: string): string {
  return version.replace('~', '-');
}

/**
 * Ist der Weg zurück nach dem Wechsel versperrt? Das ist so, sobald der Zielkanal eine höhere
 * Feature-Version bringt als der bisherige Kanal: Nach dem Update läge der bisherige dann zurück.
 */
export function returnBlocked(view: AptChannels | undefined, active: AptChannel, target: AptChannel): boolean {
  const latest = (channel: AptChannel) => view?.channels.find((c) => c.channel === channel)?.latest ?? '';
  const from = latest(active);
  const to = latest(target);
  return from !== '' && to !== '' && featureBehind(from, to);
}
