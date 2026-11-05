// Anzeigezustand des Abos: Ampel und Text je Vertragsstatus, Warnschwelle für das Ablaufdatum.

import type { StatusLevel } from '../../../components/shared';
import type { Subscription } from '../../../api/settings';

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
