import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-input';
import '../../components/lcm-switch';
import { settingsApi } from '../../api/settings';
import { t } from '../../lib/i18n';
import { BACKUP_FIELDS, scheduleBody } from '../settings/system/backup-model';
import type { Draft, FieldErrors } from '../settings/system/form-state';
import { passwordField } from '../settings/system/password-field';
import { backupChanged, backupDraft, backupErrors, needsPassphrase } from './backup-step-model';
import { SetupStep } from './setup-step';

/**
 * Schritt „Automatisches Backup“: an/aus, Intervall, Uhrzeit und Anzahl der Aufbewahrungen, über denselben Endpunkt wie
 * Einstellungen → Backups. Geplante Backups sind verschlüsselt: Hat LCM noch keinen Schlüssel, fragt der Schritt nach
 * einer Passphrase. Lässt sich überspringen.
 */
export class SetupBackup extends SetupStep {
  static override properties = { ...SetupStep.properties, _draft: { state: true }, _errors: { state: true } };
  static override styles = [
    ...SetupStep.styles,
    css`
      .block {
        display: grid;
        gap: var(--space-4);
      }
      p {
        margin: 0;
      }
    `,
  ];

  readonly step = 'backup' as const;
  declare _draft: Draft | undefined;
  declare _errors: FieldErrors;
  #base: Draft | undefined;

  constructor() {
    super();
    this._errors = {};
  }

  override willUpdate(): void {
    if (this._draft) return;
    this.#base = backupDraft(this.settings);
    this._draft = { ...this.#base };
  }

  #set(key: string, value: string | boolean): void {
    if (this._draft) this._draft = { ...this._draft, [key]: value };
    this._errors = {};
  }

  protected override async save(): Promise<void> {
    const draft = this._draft;
    if (!draft || !this.#base) return;
    const invalid = backupErrors(draft, this.settings);
    if (Object.keys(invalid).length > 0) {
      this._errors = this.errorTexts(draft, invalid, BACKUP_FIELDS);
      this._error = t('settings.form.fixErrors');
      return;
    }
    if (backupChanged(draft, this.#base)) await settingsApi.configureBackups(scheduleBody(draft));
    await this.advance();
  }

  #field(key: string, label: string, options: { type?: string; width: 'xs' | 's'; hint?: string }): TemplateResult {
    const value = String((this._draft ?? {})[key] ?? '');
    return html`<lcm-input
      data-field=${key}
      label=${label}
      type=${options.type ?? 'text'}
      width=${options.width}
      hint=${options.hint ?? ''}
      error=${this._errors[key] ?? ''}
      autocomplete="off"
      .value=${value}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set(key, e.detail.value)}
    ></lcm-input>`;
  }

  #passphrase(draft: Draft): TemplateResult | typeof nothing {
    if (!needsPassphrase(draft, this.settings)) return nothing;
    return passwordField(t('settings.backups.keys.passphrase'), String(draft.backup_passphrase ?? ''), (value) => this.#set('backup_passphrase', value), this._errors.backup_passphrase ?? t('setup.backup.passphraseHint'));
  }

  protected override body(): TemplateResult {
    const draft = this._draft ?? {};
    return html`<div class="block">
      <lcm-switch
        data-field="backup_enabled"
        label=${t('settings.backups.schedule.enable')}
        on-label=${t('settings.form.on')}
        off-label=${t('settings.form.off')}
        on-text=${t('settings.backups.schedule.enableOn')}
        off-text=${t('settings.backups.schedule.enableOff')}
        .checked=${draft.backup_enabled === true}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set('backup_enabled', e.detail.checked)}
      ></lcm-switch>
      ${this.#field('backup_interval_hours', t('settings.backups.schedule.interval'), { width: 'xs', hint: t('settings.backups.schedule.intervalHint') })}
      ${this.#field('backup_time', t('settings.backups.schedule.time'), { type: 'time', width: 's', hint: t('settings.backups.schedule.timeHint') })}
      ${this.#field('backup_retention', t('settings.backups.schedule.retention'), { width: 'xs' })} ${this.#passphrase(draft)}
      <p class="muted">${t('setup.backup.updateHint')}</p>
    </div>`;
  }
}
customElements.define('lcm-setup-backup', SetupBackup);
