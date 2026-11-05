import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-input';
import '../../components/lcm-select';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit } from '../../components/shared';
import type { Option } from '../../components/lcm-select';
import { profilesApi, type Block, type Profile } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import './form-modal';
import '../../components/lcm-textarea';
import './profile-rules';
import {
  MAX_PROFILE_SLUG,
  createProfileBody,
  emptyProfileForm,
  isProfileValid,
  profileBody,
  profileFormOf,
  sameProfileForm,
  slugFromName,
  validateProfileForm,
  type ProfileForm,
} from './profile-model';

type ProfileErrors = ReturnType<typeof validateProfileForm>;

/**
 * <lcm-lu-profile-modal open .profile=${…} .blocks=${…} can-write> – Berechtigungsprofil anlegen (ohne `profile`),
 * bearbeiten oder, bei mitgelieferten Profilen, nur ansehen (dort bleibt „Kopie anlegen“). Meldet `lcm-saved`,
 * `lcm-clone` (mit dem Profil) und `lcm-close`.
 */
export class LcmLuProfileModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    profile: { attribute: false },
    blocks: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _attempted: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .chips {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
    `,
  ];

  declare open: boolean;
  declare profile: Profile | null;
  declare blocks: Block[];
  declare canWrite: boolean;
  declare _form: ProfileForm;
  declare _busy: boolean;
  declare _error: string;
  declare _attempted: boolean;
  #initial: ProfileForm = emptyProfileForm();
  #key = '';

  constructor() {
    super();
    this.open = false;
    this.profile = null;
    this.blocks = [];
    this.canWrite = false;
    this._form = this.#initial;
    this._busy = false;
    this._error = '';
    this._attempted = false;
    new I18nController(this);
  }

  override willUpdate(): void {
    const key = this.open ? `${this.profile?.id ?? 'new'}` : '';
    if (key === this.#key) return;
    this.#key = key;
    if (!this.open) return;
    this.#initial = this.profile ? profileFormOf(this.profile) : emptyProfileForm();
    this._form = this.#initial;
    this._error = '';
    this._attempted = false;
  }

  get #readOnly(): boolean {
    return !this.canWrite || (this.profile?.builtin ?? false);
  }

  #set(patch: Partial<ProfileForm>): void {
    this._form = { ...this._form, ...patch };
  }

  async #save(): Promise<void> {
    this._attempted = true;
    if (!isProfileValid(validateProfileForm(this._form, this.profile === null, this.blocks))) return;
    this._busy = true;
    this._error = '';
    try {
      const name = this._form.name.trim();
      if (this.profile) {
        await profilesApi.update(this.profile.id, profileBody(this._form));
        toast({ tone: 'ok', title: t('profiles.notice.saved', { name }) });
      } else {
        await profilesApi.create(createProfileBody(this._form));
        toast({ tone: 'ok', title: t('profiles.notice.created', { name }) });
      }
      this._form = this.#initial;
      emit(this, 'lcm-saved');
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #accountOptions(): Option[] {
    return [
      { value: 'shell', label: t('profiles.account.shell') },
      { value: 'sftp', label: t('profiles.account.sftp') },
    ];
  }

  #fields(errors: ProfileErrors) {
    return html`${this.#nameFields(errors)} ${this.#descriptionFields(errors)}
      <lcm-lu-profile-rules
        .form=${this._form}
        .errors=${errors}
        .blocks=${this.blocks}
        ?attempted=${this._attempted}
        ?read-only=${this.#readOnly}
        @lcm-change=${(e: CustomEvent<Partial<ProfileForm>>) => this.#set(e.detail)}
      ></lcm-lu-profile-rules>`;
  }

  #nameFields(errors: ProfileErrors) {
    const slug = this._form.slug || slugFromName(this._form.name);
    return html`<lcm-input
        label=${t('profiles.form.name')}
        half
        width="full"
        required
        maxlength=${LIMITS.name}
        placeholder=${t('profiles.form.namePlaceholder')}
        .value=${this._form.name}
        .error=${this._attempted && errors.name ? t(errors.name, { max: LIMITS.name }) : ''}
        ?disabled=${this.#readOnly}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ name: e.detail.value })}
      ></lcm-input>
      ${this.profile === null
        ? html`<lcm-input
            label=${t('profiles.form.slug')}
            half
            width="full"
            mono
            maxlength=${MAX_PROFILE_SLUG}
            placeholder=${slug}
            .value=${this._form.slug}
            .error=${this._attempted && errors.slug ? t(errors.slug) : ''}
            hint=${t('profiles.form.slugHint', { slug: slug || '…' })}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ slug: e.detail.value })}
          ></lcm-input>`
        : html`<lcm-input label=${t('profiles.form.slug')} half width="full" mono disabled .value=${this._form.slug} hint=${t('profiles.form.slugFixed')}></lcm-input>`}`;
  }

  #descriptionFields(errors: ProfileErrors) {
    return html`<lcm-textarea
        label=${t('profiles.form.description')}
        half
        rows="2"
        maxlength=${LIMITS.description}
        .value=${this._form.description}
        .error=${this._attempted && errors.description ? t(errors.description, { max: LIMITS.description }) : ''}
        ?disabled=${this.#readOnly}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ description: e.detail.value })}
      ></lcm-textarea>
      <lcm-select
        label=${t('profiles.form.accountType')}
        half
        width="full"
        .options=${this.#accountOptions}
        .value=${this._form.account_type}
        hint=${t('profiles.form.accountHint')}
        ?disabled=${this.#readOnly}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ account_type: e.detail.value === 'sftp' ? 'sftp' : 'shell' })}
      ></lcm-select>
      ${this.profile?.grants_full_root ? html`<div class="chips"><lcm-badge tone="warn">${t('profiles.fullRoot')}</lcm-badge><span>${t('profiles.fullRootHint')}</span></div>` : nothing}`;
  }

  override render() {
    const errors = validateProfileForm(this._form, this.profile === null, this.blocks);
    const profile = this.profile;
    return html`<lcm-lu-form-modal
      ?open=${this.open}
      heading=${profile ? profile.name : t('profiles.form.newTitle')}
      subtitle=${profile ? (profile.builtin ? t('profiles.form.builtinSubtitle') : profile.slug) : t('profiles.form.newSubtitle')}
      save-label=${profile ? t('linuxUsers.action.save') : t('linuxUsers.action.create')}
      ?dirty=${!this.#readOnly && !sameProfileForm(this._form, this.#initial)}
      ?busy=${this._busy}
      ?read-only=${this.#readOnly}
      error=${this._error}
      @lcm-submit=${() => void this.#save()}
    >
      ${this.#fields(errors)}
      ${profile && this.canWrite
        ? html`<lcm-button slot="footer-start" variant="outline" icon="copy" @click=${() => emit(this, 'lcm-clone', profile)}>${t('profiles.clone')}</lcm-button>`
        : nothing}
    </lcm-lu-form-modal>`;
  }
}
customElements.define('lcm-lu-profile-modal', LcmLuProfileModal);
