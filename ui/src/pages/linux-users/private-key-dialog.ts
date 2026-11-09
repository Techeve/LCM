import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import { baseStyles, emit } from '../../components/shared';
import { copyWithToast } from '../../lib/clipboard';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import './key-guide';
import { downloadText, privateKeyFile } from './ssh-key';

/**
 * <lcm-lu-private-key .privateKey=${…} username="anna"> – zeigt einen frisch erzeugten privaten Schlüssel genau
 * einmal: herunterladen oder kopieren, dazu die Einrichtung unter Linux, macOS und Windows. Der Schlüssel steht
 * nur in diesem Element und verschwindet mit `lcm-close`. Meldet `lcm-close`.
 */
export class LcmLuPrivateKey extends LitElement {
  static override properties = {
    privateKey: { attribute: false },
    username: { type: String },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .actions {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-start: var(--space-3);
      }
      .guide {
        margin-block-start: var(--space-4);
        padding-block-start: var(--space-3);
        border-block-start: 1px solid var(--line);
      }
    `,
  ];

  declare privateKey: string;
  declare username: string;

  constructor() {
    super();
    this.privateKey = '';
    this.username = '';
    new I18nController(this);
  }

  async #copy(): Promise<void> {
    await copyWithToast(this.privateKey, { copied: t('linuxUsers.privateKey.copied'), failed: t('linuxUsers.privateKey.copyFailed') });
  }

  override render() {
    const open = this.privateKey !== '';
    return html`<lcm-dialog
      ?open=${open}
      heading=${t('linuxUsers.privateKey.title', { name: this.username })}
      message=${t('linuxUsers.privateKey.message')}
      confirm-label=${t('linuxUsers.privateKey.download')}
      cancel-label=${t('linuxUsers.action.close')}
      @lcm-confirm=${() => downloadText(privateKeyFile(this.username), this.privateKey)}
      @lcm-cancel=${() => emit(this, 'lcm-close')}
    >
      ${open
        ? html`<div class="actions">
              <lcm-button variant="outline" icon="copy" @click=${this.#copy}>${t('linuxUsers.privateKey.copy')}</lcm-button>
            </div>
            <div class="guide">
              <strong>${t('linuxUsers.keyGuide.title')}</strong>
              <lcm-lu-key-guide username=${this.username}></lcm-lu-key-guide>
            </div>`
        : nothing}
    </lcm-dialog>`;
  }
}
customElements.define('lcm-lu-private-key', LcmLuPrivateKey);
