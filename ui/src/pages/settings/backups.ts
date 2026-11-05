import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-explainer';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../../components/lcm-server-action';
import '../../components/lcm-status';
import type { Column, Row } from '../../components/lcm-data-table';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { BACKUPS_URL, readBackups, settingsApi, type Backup, type BackupRecipientPair, type GlobalSettings, type StagedRestore } from '../../api/settings';
import { auth } from '../../lib/auth';
import { saveBlob } from '../../lib/download';
import { formatBytes, formatDateTime } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { BACKUP_FIELDS, addRecipient, isBackupFile, scheduleBlocked, scheduleBody, secretState, type SecretState } from './system/backup-model';
import './system/copy-field';
import type { Draft, FieldSpec } from './system/form-state';
import { helpLabels } from './system/help';
import { passwordField } from './system/password-field';
import { SettingsForm } from './system/settings-form';
import { tableLabels } from './system/table-labels';

type Dialog = { kind: 'create' } | { kind: 'restore'; backup: Backup } | { kind: 'delete'; backup: Backup } | { kind: 'upload' };

const RESTART_LOGOUT_MS = 4000;

const CREATE_ENCRYPTION: Record<SecretState, string> = {
  recipients: 'settings.backups.create.encryption.recipients',
  passphrase: 'settings.backups.create.encryption.passphrase',
  missing: 'settings.backups.create.encryption.missing',
};

const SECRET_LABEL: Record<SecretState, string> = {
  recipients: 'settings.backups.state.recipients',
  passphrase: 'settings.backups.state.passphrase',
  missing: 'settings.backups.state.missing',
};

/** Einstellungen → Backups: Zeitplan und Verschlüsselung, Liste mit Download und Wiederherstellung, Wiederherstellung aus Datei. */
export class SettingsBackups extends SettingsForm {
  static override properties = {
    ...SettingsForm.properties,
    _generated: { state: true },
    _dialog: { state: true },
    _secret: { state: true },
    _dialogBusy: { state: true },
    _dialogError: { state: true },
    _file: { state: true },
    _staged: { state: true },
    _downloading: { state: true },
  };
  static override styles = [
    ...SettingsForm.styles,
    css`
      .file {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
      }
      .file input {
        display: none;
      }
    `,
  ];

  declare _generated: BackupRecipientPair | undefined;
  declare _dialog: Dialog | undefined;
  declare _secret: string;
  declare _dialogBusy: boolean;
  declare _dialogError: string;
  declare _file: File | undefined;
  declare _staged: StagedRestore | undefined;
  declare _downloading: string;

  protected readonly fields: FieldSpec[] = BACKUP_FIELDS;
  readonly #backups = new LiveData<Backup[]>(this, { url: () => BACKUPS_URL, transform: readBackups });
  #generating = false;
  #logoutTimer = 0;

  constructor() {
    super();
    this._generated = undefined;
    this._dialog = undefined;
    this._secret = '';
    this._dialogBusy = false;
    this._dialogError = '';
    this._file = undefined;
    this._staged = undefined;
    this._downloading = '';
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    window.clearTimeout(this.#logoutTimer);
  }

  protected override request(_body: Record<string, unknown>, draft: Draft): Promise<void> {
    return settingsApi.configureBackups(scheduleBody(draft));
  }

  protected override async afterSave(): Promise<void> {
    this._generated = undefined;
  }

  protected override fieldForCode(code: string): string | undefined {
    if (code === 'backup.weak_passphrase' || code === 'settings.backup_needs_passphrase') return 'backup_passphrase';
    if (code === 'backup.invalid_recipient') return 'backup_recipients';
    return undefined;
  }

  async #generate(): Promise<void> {
    if (this.#generating) return;
    this.#generating = true;
    try {
      const pair = await settingsApi.generateRecipient();
      this._generated = pair;
      this.set('backup_recipients', addRecipient(this.value('backup_recipients'), pair.publicKey));
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    } finally {
      this.#generating = false;
    }
  }

  #open(dialog: Dialog): void {
    this._secret = '';
    this._dialogError = '';
    this._dialog = dialog;
  }

  #close(): void {
    this._dialog = undefined;
    this._secret = '';
    this._dialogError = '';
  }

  async #dialogRun(action: () => Promise<void>): Promise<void> {
    this._dialogBusy = true;
    this._dialogError = '';
    try {
      await action();
      this.#close();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._dialogBusy = false;
    }
  }

  #createBackup(): Promise<void> {
    return this.#dialogRun(async () => {
      await settingsApi.createBackup(this._secret);
      toast({ tone: 'ok', title: t('settings.backups.created') });
    });
  }

  #deleteBackup(backup: Backup): Promise<void> {
    return this.#dialogRun(async () => {
      await settingsApi.deleteBackup(backup.fileName);
      toast({ tone: 'ok', title: t('settings.backups.deleted', { name: backup.fileName }) });
    });
  }

  #restore(backup: Backup): Promise<void> {
    return this.#dialogRun(async () => this.#staged(await settingsApi.restoreBackup(backup.fileName, this._secret)));
  }

  #upload(file: File): Promise<void> {
    return this.#dialogRun(async () => {
      this.#staged(await settingsApi.restoreUpload(file, this._secret));
      this._file = undefined;
    });
  }

  /** Bei automatischem Neustart endet die Sitzung; wer sich dann gleich abmeldet, läuft nicht in einen Fehler. */
  #staged(result: StagedRestore): void {
    this._staged = result;
    if (result.restart === 'auto') this.#logoutTimer = window.setTimeout(() => void auth.logout(), RESTART_LOGOUT_MS);
  }

  #pickFile(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (file) this._file = file;
  }

  #schedule(settings: GlobalSettings): TemplateResult {
    const generated = this._generated;
    return html`<lcm-card level="2" heading=${t('settings.backups.schedule.title')} subtitle=${t('settings.backups.schedule.subtitle')}>
      <lcm-status slot="actions" level=${secretState(settings) === 'missing' ? 'warn' : 'great'} label=${t(SECRET_LABEL[secretState(settings)])}></lcm-status>
      <div class="fields">
        ${scheduleBlocked(settings) ? html`<lcm-notice tone="warn">${t('settings.backups.schedule.blocked')}</lcm-notice>` : nothing}
        ${this.flag('backup_enabled', t('settings.backups.schedule.enable'), { on: t('settings.backups.schedule.enableOn'), off: t('settings.backups.schedule.enableOff') })}
        ${this.input('backup_interval_hours', t('settings.backups.schedule.interval'), { hint: t('settings.backups.schedule.intervalHint') })}
        ${this.input('backup_time', t('settings.backups.schedule.time'), { type: 'time', width: 's', hint: t('settings.backups.schedule.timeHint') })}
        ${this.input('backup_retention', t('settings.backups.schedule.retention'))}
        ${this.input('backup_dir', t('settings.backups.schedule.dir'), { mono: true, width: 'l', placeholder: t('settings.backups.schedule.dirPlaceholder'), hint: t('settings.backups.schedule.dirHint') })}
      </div>
    </lcm-card>
    <lcm-card level="2" heading=${t('settings.backups.keys.title')} subtitle=${t('settings.backups.keys.subtitle')}>
      <div class="fields">
        ${this.area('backup_recipients', t('settings.backups.keys.recipients'), { rows: 3, placeholder: 'age1…', hint: t('settings.backups.keys.recipientsHint') })}
        <div class="row">
          <lcm-server-action
            label=${t('settings.backups.keys.generate')}
            icon="key"
            help=${t('settings.backups.keys.generateHelp')}
            meaning=${t('settings.backups.keys.generateMeaning')}
            .labels=${helpLabels()}
            @click=${() => void this.#generate()}
          ></lcm-server-action>
        </div>
        ${generated
          ? html`<div class="fields">
              <lcm-notice tone="warn" heading=${t('settings.backups.keys.generatedTitle')}>${t('settings.backups.keys.generatedText')}</lcm-notice>
              <lcm-copy-field block value=${generated.privateKey}></lcm-copy-field>
            </div>`
          : nothing}
        ${passwordField(t('settings.backups.keys.passphrase'), this.value('backup_passphrase'), (value) => this.set('backup_passphrase', value), this._errors.backup_passphrase ?? t('settings.backups.keys.passphraseHint'))}
        ${this.flag('restore_auto_restart', t('settings.backups.keys.autoRestart'), { on: t('settings.backups.keys.autoRestartOn'), off: t('settings.backups.keys.autoRestartOff') })}
      </div>
    </lcm-card>`;
  }

  get #columns(): Column[] {
    return [
      { key: 'file', label: t('settings.backups.col.file'), kind: 'mono', sortable: true, locked: true },
      { key: 'size', label: t('settings.backups.col.size'), sortable: true, align: 'end', render: (row) => formatBytes(row.size as number) },
      { key: 'trigger', label: t('settings.backups.col.trigger'), render: (row) => (row.trigger === 'scheduler' ? t('settings.backups.trigger.scheduler') : String(row.trigger)) },
      {
        key: 'encryption',
        label: t('settings.backups.col.encryption'),
        render: (row) => html`<lcm-badge tone=${row.encryption === 'recipients' ? 'info' : ''}>${row.encryption === 'recipients' ? t('settings.backups.enc.recipients') : t('settings.backups.enc.passphrase')}</lcm-badge>`,
      },
      { key: 'created', label: t('settings.backups.col.time'), sortable: true, sortValue: (row) => String(row.createdAt ?? ''), render: (row) => formatDateTime(row.createdAt as string | null) },
      { key: 'actions', label: t('settings.backups.col.actions'), align: 'end', locked: true, render: (row) => this.#rowActions(row.backup as Backup) },
    ];
  }

  async #download(backup: Backup): Promise<void> {
    this._downloading = backup.fileName;
    try {
      saveBlob(backup.fileName, await settingsApi.downloadBackup(backup.fileName));
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.backups.download'), message: errorText(error) });
    } finally {
      this._downloading = '';
    }
  }

  #rowActions(backup: Backup): TemplateResult {
    return html`<span class="row end">
      <lcm-button variant="ghost" size="small" icon="download" ?busy=${this._downloading === backup.fileName} @click=${() => void this.#download(backup)}>${t('settings.backups.download')}</lcm-button>
      <lcm-button variant="danger" size="small" icon="undo" @click=${() => this.#open({ kind: 'restore', backup })}>${t('settings.backups.restore.label')}</lcm-button>
      <lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('settings.backups.delete.label', { name: backup.fileName })} @click=${() => this.#open({ kind: 'delete', backup })}></lcm-button>
    </span>`;
  }

  #list(): TemplateResult {
    const backups = this.#backups.data ?? [];
    const rows: Row[] = backups.map((backup) => ({
      id: String(backup.id),
      file: backup.fileName,
      size: backup.sizeBytes,
      trigger: backup.trigger,
      encryption: backup.encryption,
      createdAt: backup.createdAt,
      backup,
    }));
    return html`<lcm-card level="2" heading=${t('settings.backups.list.title')} subtitle=${t('settings.backups.list.subtitle')}>
      <lcm-server-action
        slot="actions"
        variant="primary"
        icon="disk"
        label=${t('settings.backups.create.label')}
        help=${t('settings.backups.create.help')}
        meaning=${t('settings.backups.create.meaning')}
        .labels=${helpLabels()}
        @click=${() => this.#open({ kind: 'create' })}
      ></lcm-server-action>
      <lcm-data-table density="compact" no-toolbar no-chooser .columns=${this.#columns} .rows=${rows} .labels=${tableLabels(t('settings.backups.list.title'), t('settings.backups.list.empty'))}></lcm-data-table>
    </lcm-card>`;
  }

  #fromFile(): TemplateResult {
    const file = this._file;
    return html`<lcm-card level="2" heading=${t('settings.backups.upload.title')} subtitle=${t('settings.backups.upload.subtitle')}>
      <div class="fields">
        <div class="file">
          <lcm-button icon="plus" @click=${(e: Event) => ((e.currentTarget as HTMLElement).parentElement?.querySelector('input') as HTMLInputElement).click()}>${t('settings.backups.upload.choose')}</lcm-button>
          <input type="file" accept=".lcmbak" @change=${(e: Event) => this.#pickFile(e)} />
          <span class="mono">${file ? file.name : t('settings.backups.upload.none')}</span>
          ${file && !isBackupFile(file.name) ? html`<span class="muted">${t('settings.backups.upload.wrongType')}</span>` : nothing}
        </div>
        <div class="row">
          <lcm-button variant="danger" icon="undo" ?disabled=${!file} @click=${() => this.#open({ kind: 'upload' })}>${t('settings.backups.upload.action')}</lcm-button>
        </div>
      </div>
    </lcm-card>`;
  }

  #stagedNote(): TemplateResult | typeof nothing {
    const staged = this._staged;
    if (!staged) return nothing;
    return html`<lcm-notice tone="ok" heading=${t('settings.backups.staged.title')}
      >${staged.restart === 'auto' ? t('settings.backups.staged.auto') : t('settings.backups.staged.manual')}</lcm-notice
    >`;
  }

  #restartText(): string {
    return this.settings.restore_auto_restart === true ? t('settings.backups.restore.restartAuto') : t('settings.backups.restore.restartManual');
  }

  #secretField(): TemplateResult {
    return html`<lcm-input
      label=${t('settings.backups.restore.secret')}
      type="password"
      mono
      maxlength=${LIMITS.password}
      autocomplete="new-password"
      .value=${this._secret}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._secret = e.detail.value)}
    ></lcm-input>`;
  }

  #destructiveImpacts(since: string): Impact[] {
    return [
      { label: t('settings.backups.restore.impactReplaced'), value: t('settings.backups.restore.impactReplacedValue'), tone: 'crit' },
      { label: t('settings.backups.restore.impactLost'), value: t('settings.backups.restore.impactLostValue', { since }), tone: 'crit' },
      { label: t('settings.backups.restore.impactRestart'), value: this.#restartText(), tone: 'warn' },
      { label: t('settings.backups.restore.impactSessions'), value: t('settings.backups.restore.impactSessionsValue'), tone: 'warn' },
      { label: t('settings.backups.restore.impactReversible'), value: t('settings.backups.restore.impactReversibleValue'), tone: 'crit' },
    ];
  }

  #dialogView(): TemplateResult | typeof nothing {
    const dialog = this._dialog;
    if (!dialog) return nothing;
    if (dialog.kind === 'create') return this.#createDialog();
    if (dialog.kind === 'delete') return this.#deleteDialog(dialog.backup);
    return this.#restoreDialog(dialog);
  }

  #createDialog(): TemplateResult {
    const state = secretState(this.settings);
    const impacts: Impact[] = [
      { label: t('settings.backups.create.impactEncryption'), value: t(CREATE_ENCRYPTION[state]), tone: state === 'missing' ? 'warn' : undefined },
      { label: t('settings.backups.create.impactPlace'), value: String(this.settings.backup_dir ?? '') || t('settings.backups.schedule.dirPlaceholder') },
    ];
    return html`<lcm-dialog
      open
      heading=${t('settings.backups.create.title')}
      message=${t('settings.backups.create.message')}
      .impacts=${impacts}
      confirm-label=${t('settings.backups.create.confirm')}
      cancel-label=${t('settings.form.cancel')}
      ?busy=${this._dialogBusy}
      error=${this._dialogError}
      @lcm-confirm=${() => void this.#createBackup()}
      @lcm-cancel=${() => this.#close()}
    >
      ${passwordField(state === 'missing' ? t('settings.backups.create.passphraseRequired') : t('settings.backups.create.passphraseOptional'), this._secret, (value) => (this._secret = value))}
    </lcm-dialog>`;
  }

  #deleteDialog(backup: Backup): TemplateResult {
    const impacts: Impact[] = [
      { label: t('settings.backups.delete.impactFile'), value: backup.fileName },
      { label: t('settings.backups.delete.impactRestore'), value: t('settings.backups.delete.impactRestoreValue'), tone: 'crit' },
    ];
    return html`<lcm-dialog
      open
      tone="danger"
      heading=${t('settings.backups.delete.title')}
      message=${t('settings.backups.delete.message')}
      .impacts=${impacts}
      confirm-label=${t('settings.backups.delete.confirm')}
      cancel-label=${t('settings.form.cancel')}
      ?busy=${this._dialogBusy}
      error=${this._dialogError}
      @lcm-confirm=${() => void this.#deleteBackup(backup)}
      @lcm-cancel=${() => this.#close()}
    ></lcm-dialog>`;
  }

  /** Wiederherstellen aus der Liste oder aus einer hochgeladenen Datei. */
  #restoreDialog(dialog: Extract<Dialog, { kind: 'restore' | 'upload' }>): TemplateResult {
    const isUpload = dialog.kind === 'upload';
    const name = isUpload ? (this._file?.name ?? '') : dialog.backup.fileName;
    const since = isUpload ? name : formatDateTime(dialog.backup.createdAt);
    return html`<lcm-dialog
      open
      tone="danger"
      heading=${t('settings.backups.restore.title')}
      message=${t('settings.backups.restore.message', { name })}
      .impacts=${this.#destructiveImpacts(since)}
      expect=${name}
      expect-label=${t('settings.backups.restore.expect', { name })}
      confirm-label=${t('settings.backups.restore.confirm')}
      cancel-label=${t('settings.form.cancel')}
      ?busy=${this._dialogBusy}
      error=${this._dialogError}
      @lcm-confirm=${() => (isUpload ? void this.#upload(this._file as File) : void this.#restore(dialog.backup))}
      @lcm-cancel=${() => this.#close()}
    >
      ${this.#secretField()}
    </lcm-dialog>`;
  }

  protected override explainer(): TemplateResult {
    return html`<lcm-explainer
      topic="settings-backups"
      heading=${t('settings.backups.explainer.title')}
      collapse-label=${t('settings.form.collapse')}
      expand-label=${t('settings.form.expand')}
    >
      <p>${t('settings.backups.explainer.what')}</p>
      <ul>
        <li><strong>${t('settings.backups.explainer.recipientsTitle')}</strong> ${t('settings.backups.explainer.recipients')}</li>
        <li><strong>${t('settings.backups.explainer.passphraseTitle')}</strong> ${t('settings.backups.explainer.passphrase')}</li>
      </ul>
      <p>${t('settings.backups.explainer.restore')}</p>
    </lcm-explainer>`;
  }

  protected override overlays(): TemplateResult | typeof nothing {
    return this.#dialogView();
  }

  protected renderForm(settings: GlobalSettings): TemplateResult {
    return html`${this.#stagedNote()} ${this.#schedule(settings)} ${this.#list()} ${this.#fromFile()}`;
  }
}
customElements.define('lcm-settings-backups', SettingsBackups);
