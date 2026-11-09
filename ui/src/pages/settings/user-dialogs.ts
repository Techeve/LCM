import { html, type TemplateResult } from 'lit';
import '../../components/lcm-dialog';
import type { Impact } from '../../components/lcm-dialog';
import type { LoginUser } from '../../api/settings';
import { t } from '../../lib/i18n';

/** Was alle Bestätigungsfenster der Benutzerverwaltung teilen: Zustand, Fehler und Abbrechen. */
export interface UserDialogState {
  busy: boolean;
  error: string;
  onCancel: () => void;
}

/** Passwort setzen: `fields` sind die Eingabefelder, die der Aufrufer verwaltet. */
export function passwordDialog(user: LoginUser, fields: TemplateResult, state: UserDialogState, onConfirm: () => void): TemplateResult {
  const impacts: Impact[] = [
    { label: t('settings.users.password.impactSessions'), value: t('settings.users.password.impactSessionsValue'), tone: 'warn' },
    { label: t('settings.users.password.impactNext'), value: t('settings.users.password.impactNextValue') },
  ];
  return html`<lcm-dialog
    open
    heading=${t('settings.users.password.title', { name: user.username })}
    message=${t('settings.users.password.message')}
    .impacts=${impacts}
    confirm-label=${t('settings.users.password.confirm')}
    cancel-label=${t('settings.form.cancel')}
    ?busy=${state.busy}
    error=${state.error}
    @lcm-confirm=${onConfirm}
    @lcm-cancel=${state.onCancel}
  >
    <div class="fields">${fields}</div>
  </lcm-dialog>`;
}

/** Konto sperren (`block`) oder wieder freigeben. */
export function blockDialog(user: LoginUser, block: boolean, state: UserDialogState, onConfirm: () => void): TemplateResult {
  const impacts: Impact[] = block
    ? [
        { label: t('settings.users.block.impactLogin'), value: t('settings.users.block.impactLoginValue'), tone: 'warn' },
        { label: t('settings.users.block.impactSessions'), value: t('settings.users.block.impactSessionsValue'), tone: 'warn' },
        { label: t('settings.users.block.impactReversible'), value: t('settings.users.block.impactReversibleValue'), tone: 'ok' },
      ]
    : [{ label: t('settings.users.unblock.impactLogin'), value: t('settings.users.unblock.impactLoginValue') }];
  return html`<lcm-dialog
    open
    tone=${block ? 'danger' : 'default'}
    heading=${t(block ? 'settings.users.block.title' : 'settings.users.unblock.title', { name: user.username })}
    message=${t(block ? 'settings.users.block.message' : 'settings.users.unblock.message')}
    .impacts=${impacts}
    confirm-label=${t(block ? 'settings.users.block.confirm' : 'settings.users.unblock.confirm')}
    cancel-label=${t('settings.form.cancel')}
    ?busy=${state.busy}
    error=${state.error}
    @lcm-confirm=${onConfirm}
    @lcm-cancel=${state.onCancel}
  ></lcm-dialog>`;
}

export function deleteDialog(user: LoginUser, state: UserDialogState, onConfirm: () => void): TemplateResult {
  const impacts: Impact[] = [
    { label: t('settings.users.delete.impactAccount'), value: t('settings.users.delete.impactAccountValue'), tone: 'crit' },
    { label: t('settings.users.delete.impactSessions'), value: t('settings.users.delete.impactSessionsValue'), tone: 'warn' },
    { label: t('settings.users.delete.impactReversible'), value: t('settings.users.delete.impactReversibleValue'), tone: 'crit' },
  ];
  return html`<lcm-dialog
    open
    tone="danger"
    heading=${t('settings.users.delete.title', { name: user.username })}
    message=${t('settings.users.delete.message')}
    .impacts=${impacts}
    expect=${user.username}
    expect-label=${t('settings.users.delete.expect', { name: user.username })}
    confirm-label=${t('settings.users.delete.confirm')}
    cancel-label=${t('settings.form.cancel')}
    ?busy=${state.busy}
    error=${state.error}
    @lcm-confirm=${onConfirm}
    @lcm-cancel=${state.onCancel}
  ></lcm-dialog>`;
}
