import { LitElement, css, html, nothing } from 'lit';
import { live } from 'lit/directives/live.js';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-input';
import '../../components/lcm-save-bar';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { updateProfile } from '../../api/account';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { SAVE_EVENT } from '../../lib/hotkeys';
import { errorText, t } from '../../lib/i18n';
import { saveBarLabels } from '../../lib/save-bar';
import { unsaved } from '../../lib/unsaved';
import { LIMITS } from '../../lib/validate';
import { draftOf, emailChanged, profileDirty, profileErrors, type ProfileDraft } from './account-model';
import { formStyles } from './form-styles';

/** <lcm-account-profile> – Name und E-Mail-Adresse des eigenen Kontos; Strg/Cmd+S speichert. */
export class LcmAccountProfile extends LitElement {
  static override properties = {
    _draft: { state: true },
    _password: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _tried: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
      .stack {
        max-inline-size: 36rem;
      }
      dl {
        display: grid;
        grid-template-columns: max-content minmax(0, 1fr);
        gap: var(--space-2) var(--space-5);
        margin: 0;
      }
      dt {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      dd {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-1);
        margin: 0;
      }
    `,
  ];

  declare _draft: ProfileDraft;
  declare _password: string;
  declare _busy: boolean;
  declare _error: string;
  declare _tried: boolean;

  constructor() {
    super();
    this._draft = draftOf(auth.user);
    this._password = '';
    this._busy = false;
    this._error = '';
    this._tried = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(SAVE_EVENT, this.#onSave);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener(SAVE_EVENT, this.#onSave);
    unsaved.set(this, false);
  }

  get #saved(): ProfileDraft {
    return draftOf(auth.user);
  }

  get #dirty(): boolean {
    return profileDirty(this._draft, this.#saved);
  }

  get #needsPassword(): boolean {
    return emailChanged(this._draft, this.#saved);
  }

  override willUpdate(): void {
    unsaved.set(this, this.#dirty);
  }

  #onSave = (event: Event): void => {
    if (!this.#dirty) return;
    event.preventDefault();
    void this.#save();
  };

  #set(patch: Partial<ProfileDraft>): void {
    this._draft = { ...this._draft, ...patch };
  }

  async #save(): Promise<void> {
    const user = auth.user;
    if (!user || this._busy || !this.#dirty) return;
    this._tried = true;
    if (Object.keys(profileErrors(this._draft)).length > 0) return;
    if (this.#needsPassword && !this._password) {
      this._error = t('account.profile.passwordMissing');
      return;
    }
    this._busy = true;
    this._error = '';
    try {
      await updateProfile(user.id, {
        email: this._draft.email.trim(),
        first_name: this._draft.firstName.trim(),
        last_name: this._draft.lastName.trim(),
        current_password: this.#needsPassword ? this._password : '',
      });
      this._password = '';
      this._tried = false;
      await auth.refresh();
      this._draft = draftOf(auth.user);
      toast({ tone: 'ok', title: t('account.profile.saved') });
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement)) return;
    event.preventDefault();
    void this.#save();
  }

  #identity() {
    const user = auth.user;
    return html`<dl>
      <dt>${t('account.profile.username')}</dt>
      <dd class="mono">${user?.username ?? ''}</dd>
      <dt>${t('account.profile.roles')}</dt>
      <dd>${(user?.roles ?? []).map((role) => html`<lcm-badge>${role}</lcm-badge>`)}</dd>
    </dl>`;
  }

  #fields(errors: ReturnType<typeof profileErrors>) {
    const shown = (key: string | undefined, max = 0) => (this._tried && key ? t(key, { max }) : '');
    return html`<div class="pair">
        <lcm-input
          label=${t('account.profile.firstName')}
          autocomplete="given-name"
          maxlength=${LIMITS.personName}
          .error=${shown(errors.firstName, LIMITS.personName)}
          .value=${live(this._draft.firstName)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ firstName: event.detail.value })}
        ></lcm-input>
        <lcm-input
          label=${t('account.profile.lastName')}
          autocomplete="family-name"
          maxlength=${LIMITS.personName}
          .error=${shown(errors.lastName, LIMITS.personName)}
          .value=${live(this._draft.lastName)}
          @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ lastName: event.detail.value })}
        ></lcm-input>
      </div>
      <lcm-input
        label=${t('account.profile.email')}
        width="l"
        type="email"
        autocomplete="email"
        maxlength=${LIMITS.email}
        .error=${shown(errors.email)}
        hint=${t('account.profile.emailHint')}
        .value=${live(this._draft.email)}
        @lcm-input=${(event: CustomEvent<{ value: string }>) => this.#set({ email: event.detail.value })}
      ></lcm-input>
      ${this.#needsPassword
        ? html`<lcm-input
            label=${t('account.profile.currentPassword')}
            type="password"
            autocomplete="current-password"
            maxlength=${LIMITS.password}
            hint=${t('account.profile.currentPasswordHint')}
            .value=${live(this._password)}
            @lcm-input=${(event: CustomEvent<{ value: string }>) => (this._password = event.detail.value)}
          ></lcm-input>`
        : nothing}`;
  }

  override render() {
    return html`<lcm-card level="2" heading=${t('account.profile.title')}>
      <div class="stack" @keydown=${this.#onKeyDown}>
        ${this.#identity()} ${this.#fields(profileErrors(this._draft))}
        <lcm-save-bar
          always
          ?dirty=${this.#dirty}
          ?busy=${this._busy}
          error=${this._error}
          .labels=${saveBarLabels(t('account.profile.save'), t('account.profile.discard'))}
          @lcm-save=${() => void this.#save()}
          @lcm-discard=${() => ((this._draft = this.#saved), (this._error = ''))}
        ></lcm-save-bar>
      </div>
    </lcm-card>`;
  }
}
customElements.define('lcm-account-profile', LcmAccountProfile);
