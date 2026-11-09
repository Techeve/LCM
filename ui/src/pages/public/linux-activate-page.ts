import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../components/lcm-button';
import '../../components/lcm-choice';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import type { ChoiceOption } from '../../components/lcm-choice';
import { baseStyles } from '../../components/shared';
import { consumeLinuxActivation, type LinuxActivationResult, type LinuxKeyMode } from '../../api/provisioning';
import { copyWithToast } from '../../lib/clipboard';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { router } from '../../lib/router';
import { formStyles } from '../account/form-styles';
import '../account/new-password';
import type { NewPasswordDetail } from '../account/new-password';
import { EXAMPLES } from '../linux-users/examples';
import '../linux-users/key-guide';
import { downloadText, parsePublicKey, privateKeyFile } from '../linux-users/ssh-key';
import '../../components/lcm-textarea';
import './public-frame';

export interface LinuxActivationDraft {
  password: string;
  passwordValid: boolean;
  keyMode: LinuxKeyMode;
  publicKey: string;
}

/** Fehler zum eingefügten Schlüssel (leer, solange nichts eingefügt ist oder er gut aussieht). */
export function publicKeyProblem(draft: LinuxActivationDraft): 'missing' | 'invalid' | '' {
  if (draft.keyMode !== 'paste') return '';
  if (!draft.publicKey.trim()) return 'missing';
  return parsePublicKey(draft.publicKey) ? '' : 'invalid';
}

/** Mindestens Passwort oder Schlüssel; ein eingegebenes Passwort muss die Regeln erfüllen. */
export function canActivate(draft: LinuxActivationDraft): boolean {
  if (draft.password && !draft.passwordValid) return false;
  if (draft.keyMode === 'paste') return publicKeyProblem(draft) === '';
  return draft.keyMode === 'generate' || draft.password !== '';
}

/**
 * Öffentliche Seite für den Aktivierungslink eines Linux-Benutzers (`/linux-activate?token=…`): Passwort und/oder
 * SSH-Schlüssel festlegen. Erzeugt LCM das Schlüsselpaar, steht der private Schlüssel genau einmal auf dieser Seite.
 */
export class LinuxActivatePage extends LitElement {
  static override properties = {
    _draft: { state: true },
    _keyName: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _result: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
      section {
        display: grid;
        gap: var(--space-3);
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
      section:first-of-type {
        padding-block-start: 0;
        border: 0;
      }
      .key {
        display: grid;
        gap: var(--space-3);
        padding: var(--space-4);
        border-radius: var(--radius-l);
        background: var(--warn-soft);
      }
      .guide {
        padding-block-start: var(--space-3);
        border-block-start: 1px solid var(--line);
      }
    `,
  ];

  declare _draft: LinuxActivationDraft;
  declare _keyName: string;
  declare _busy: boolean;
  declare _error: string;
  declare _result: LinuxActivationResult | null;
  #token = '';

  constructor() {
    super();
    this._draft = { password: '', passwordValid: false, keyMode: 'none', publicKey: '' };
    this._keyName = '';
    this._busy = false;
    this._error = '';
    this._result = null;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override willUpdate(): void {
    const token = router.state.query.get('token');
    if (token) this.#token = token;
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this._result = null; // der private Schlüssel bleibt nicht im Speicher liegen
  }

  #set(patch: Partial<LinuxActivationDraft>): void {
    this._draft = { ...this._draft, ...patch };
  }

  async #submit(): Promise<void> {
    if (!canActivate(this._draft) || this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      this._result = await consumeLinuxActivation({
        token: this.#token,
        password: this._draft.password,
        keyMode: this._draft.keyMode,
        keyName: this._keyName || t('linuxActivate.defaultKeyName'),
        publicKey: this._draft.publicKey,
      });
      this.#token = '';
      router.setQuery({ token: null }, { replace: true });
      await this.updateComplete;
      this.renderRoot.querySelector<HTMLElement>('.success')?.focus();
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #copy(text: string): Promise<void> {
    await copyWithToast(text, { copied: t('linuxActivate.copied'), failed: t('linuxActivate.copyFailed') });
  }

  #keyModes(): ChoiceOption[] {
    return [
      { id: 'none', label: t('linuxActivate.key.none'), description: t('linuxActivate.key.noneText') },
      { id: 'paste', label: t('linuxActivate.key.paste'), icon: 'key', description: t('linuxActivate.key.pasteText') },
      { id: 'generate', label: t('linuxActivate.key.generate'), icon: 'plus', description: t('linuxActivate.key.generateText') },
    ];
  }

  #keyError(): string {
    const problem = publicKeyProblem(this._draft);
    return problem === 'invalid' ? t('linuxActivate.key.invalid') : '';
  }

  #form() {
    const draft = this._draft;
    return html`<div class="stack">
      <section>
        <h2>${t('linuxActivate.passwordTitle')}</h2>
        <p class="lead">${t('linuxActivate.passwordText')}</p>
        <lcm-account-new-password
          label=${t('linuxActivate.password')}
          @lcm-input=${(event: CustomEvent<NewPasswordDetail>) => this.#set({ password: event.detail.value, passwordValid: event.detail.valid })}
        ></lcm-account-new-password>
      </section>
      <section>
        <h2>${t('linuxActivate.keyTitle')}</h2>
        <p class="lead">${t('linuxActivate.keyText')} <a href="/docs/ssh-schluessel" target="_blank" rel="noopener">${t('linuxActivate.keyDocs')}</a></p>
        <lcm-choice
          label=${t('linuxActivate.key.label')}
          .options=${this.#keyModes()}
          value=${draft.keyMode}
          @lcm-change=${(event: CustomEvent<{ value: LinuxKeyMode }>) => this.#set({ keyMode: event.detail.value })}
        ></lcm-choice>
        ${draft.keyMode === 'none'
          ? nothing
          : html`<lcm-input width="full"
              maxlength=${LIMITS.name}
              label=${t('linuxActivate.key.name')}
              placeholder=${t('linuxActivate.defaultKeyName')}
              .value=${live(this._keyName)}
              @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._keyName = event.detail.value)}
            ></lcm-input>`}
        ${draft.keyMode === 'paste'
          ? html`<lcm-textarea
              label=${t('linuxActivate.key.publicKey')}
              rows="3"
              mono
              maxlength=${LIMITS.script}
              placeholder=${EXAMPLES.publicKey}
              hint=${t('linuxActivate.key.publicKeyHint')}
              .value=${draft.publicKey}
              .error=${this.#keyError()}
              @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ publicKey: event.detail.value })}
            ></lcm-textarea>`
          : nothing}
        ${draft.keyMode === 'generate' ? html`<lcm-notice>${t('linuxActivate.key.generateNote')}</lcm-notice>` : nothing}
      </section>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <lcm-button variant="primary" block ?disabled=${!canActivate(draft)} ?busy=${this._busy} @click=${() => void this.#submit()}
        >${t('linuxActivate.submit')}</lcm-button
      >
    </div>`;
  }

  #done(result: LinuxActivationResult) {
    const privateKey = result.private_key ?? '';
    return html`<div class="stack">
      <lcm-notice tone="ok" class="success" tabindex="-1">${t('linuxActivate.done', { username: result.username })}</lcm-notice>
      ${privateKey
        ? html`<div class="key">
            <h2>${t('linuxActivate.privateKey.title')}</h2>
            <p class="lead">${t('linuxActivate.privateKey.text')}</p>
            <div class="actions">
              <lcm-button variant="primary" icon="download" @click=${() => downloadText(privateKeyFile(result.username), privateKey)}
                >${t('linuxActivate.privateKey.download')}</lcm-button
              >
              <lcm-button variant="outline" icon="copy" @click=${() => void this.#copy(privateKey)}>${t('linuxActivate.privateKey.copy')}</lcm-button>
            </div>
            <div class="guide">
              <h3>${t('linuxActivate.privateKey.guide')}</h3>
              <lcm-lu-key-guide username=${result.username}></lcm-lu-key-guide>
            </div>
          </div>`
        : nothing}
    </div>`;
  }

  #body() {
    if (this._result) return this.#done(this._result);
    if (!this.#token) return html`<lcm-notice tone="warn">${t('linuxActivate.noToken')}</lcm-notice>`;
    return this.#form();
  }

  override render() {
    return html`<lcm-public-frame wide heading=${t('linuxActivate.title')} subtitle=${this._result ? '' : t('linuxActivate.intro')}
      >${this.#body()}</lcm-public-frame
    >`;
  }
}
customElements.define('lcm-page-linux-activate', LinuxActivatePage);
