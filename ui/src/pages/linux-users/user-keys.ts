import { LitElement, html } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-input';
import { baseStyles } from '../../components/shared';
import { linuxUsersApi, type LinuxUser, type UserServer } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import '../../components/lcm-textarea';
import { buildImpacts } from './impacts';
import { sectionStyles } from './lu-styles';
import './lu-action';
import './private-key-dialog';
import { keyLabel, keySummary, parsePublicKey } from './ssh-key';
import { EXAMPLES } from './examples';

/**
 * <lcm-lu-user-keys .user=${…} .servers=${…} can-write> – SSH-Schlüssel eines Benutzers: auflisten, hinzufügen,
 * erzeugen, entfernen. Jede Änderung geht sofort auf die Server des Benutzers (`servers`), deshalb mit Wirkung im Dialog.
 */
export class LcmLuUserKeys extends LitElement {
  static override properties = {
    user: { attribute: false },
    servers: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _name: { state: true },
    _key: { state: true },
    _private: { state: true },
    _touched: { state: true },
  };
  static override styles = [baseStyles, sectionStyles];

  declare user: LinuxUser;
  declare servers: UserServer[];
  declare canWrite: boolean;
  declare _name: string;
  declare _key: string;
  declare _private: string;
  declare _touched: boolean;

  constructor() {
    super();
    this.servers = [];
    this.canWrite = false;
    this._name = '';
    this._key = '';
    this._private = '';
    this._touched = false;
    new I18nController(this);
  }

  get #keyError(): string {
    const key = this._key.trim();
    if (!this._touched || !key) return '';
    return parsePublicKey(key) ? '' : t('linuxUsers.keys.invalid');
  }

  #impacts(effect: string, reversible: boolean) {
    return buildImpacts({ servers: this.servers, effect, reversible });
  }

  async #add(): Promise<void> {
    await linuxUsersApi.addKey(this.user.id, this._name.trim(), this._key.trim());
    this._name = this._key = '';
    this._touched = false;
  }

  async #generate(): Promise<void> {
    const { private_key } = await linuxUsersApi.generateKey(this.user.id, this._name.trim());
    this._private = private_key;
    this._name = '';
  }

  #row(key: LinuxUser['ssh_keys'][number]) {
    return html`<li>
      <div class="grow">
        <span class="name">${keyLabel(key)}</span>
        <code class="mono" title=${key.public_key}>${keySummary(key.public_key)}</code>
      </div>
      ${this.canWrite
        ? html`<lcm-lu-action
            label=${t('linuxUsers.keys.remove.label')}
            help=${t('linuxUsers.keys.remove.help')}
            meaning=${t('linuxUsers.keys.remove.meaning')}
            icon="trash"
            variant="danger"
            confirm-heading=${t('linuxUsers.keys.remove.heading', { name: keyLabel(key) })}
            confirm-label=${t('linuxUsers.keys.remove.confirm')}
            danger
            done=${t('linuxUsers.keys.remove.done')}
            .impacts=${this.#impacts(t('linuxUsers.keys.remove.effect'), false)}
            .run=${() => linuxUsersApi.removeKey(key.id)}
          ></lcm-lu-action>`
        : ''}
    </li>`;
  }

  #addForm() {
    const hasKey = this._key.trim() !== '';
    const confirmless = this.servers.length === 0;
    return html`<div class="inline-form">
      <lcm-input
        label=${t('linuxUsers.keys.name')}
        width="full"
        maxlength=${LIMITS.name}
        placeholder=${t('linuxUsers.keys.namePlaceholder')}
        .value=${this._name}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._name = e.detail.value)}
      ></lcm-input>
      <lcm-textarea
        label=${t('linuxUsers.keys.publicKey')}
        rows="1"
        mono
        maxlength=${LIMITS.script}
        placeholder=${EXAMPLES.publicKey}
        .value=${this._key}
        .error=${this.#keyError}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => {
          this._key = e.detail.value;
          this._touched = true;
        }}
      ></lcm-textarea>
      <div class="actions">
        <lcm-lu-action
          label=${t('linuxUsers.keys.add.label')}
          help=${t('linuxUsers.keys.add.help')}
          meaning=${t('linuxUsers.keys.add.meaning')}
          icon="plus"
          variant="primary"
          ?disabled=${!hasKey || this.#keyError !== ''}
          ?no-confirm=${confirmless}
          confirm-label=${t('linuxUsers.keys.add.confirm')}
          done=${t('linuxUsers.keys.add.done')}
          .impacts=${this.#impacts(t('linuxUsers.keys.add.effect'), true)}
          .run=${() => this.#add()}
        ></lcm-lu-action>
        <lcm-lu-action
          label=${t('linuxUsers.keys.generate.label')}
          help=${t('linuxUsers.keys.generate.help')}
          meaning=${t('linuxUsers.keys.generate.meaning')}
          icon="key"
          ?disabled=${hasKey}
          ?no-confirm=${confirmless}
          confirm-label=${t('linuxUsers.keys.generate.confirm')}
          .impacts=${this.#impacts(t('linuxUsers.keys.generate.effect'), true)}
          .run=${() => this.#generate()}
        ></lcm-lu-action>
      </div>
    </div>`;
  }

  override render() {
    return html`<section>
        <h3>${t('linuxUsers.keys.title')} <lcm-badge>${this.user.ssh_keys.length}</lcm-badge></h3>
        ${this.user.ssh_keys.length === 0
          ? html`<p class="empty">${t('linuxUsers.keys.empty')}</p>`
          : html`<ul class="rows">${this.user.ssh_keys.map((key) => this.#row(key))}</ul>`}
        ${this.canWrite ? this.#addForm() : ''}
      </section>
      <lcm-lu-private-key .privateKey=${this._private} username=${this.user.username} @lcm-close=${() => (this._private = '')}></lcm-lu-private-key>`;
  }
}
customElements.define('lcm-lu-user-keys', LcmLuUserKeys);
