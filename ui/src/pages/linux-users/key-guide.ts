import { LitElement, css, html } from 'lit';
import { baseStyles } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { privateKeyFile } from './ssh-key';

/** Die Befehle und Konfigurationszeilen der Anleitung; sie sind in jeder Sprache gleich. */
export function guideCommands(username: string, file: string) {
  return {
    chmod: `chmod 600 ~/.ssh/${file}`,
    connect: `ssh -i ~/.ssh/${file} ${username}@<server>`,
    linuxConfig: `Host <alias>\n  HostName <server-ip>\n  User ${username}\n  IdentityFile ~/.ssh/${file}`,
    windowsConfig: `Host *\n  IdentityFile C:\\Users\\<name>\\.ssh\\${file}`,
    windowsConnect: `ssh ${username}@<server>`,
  };
}

/**
 * <lcm-lu-key-guide username="anna"> – Anleitung, wie der frisch erzeugte private Schlüssel unter Linux/macOS und
 * Windows abgelegt und benutzt wird. Erscheint dort, wo LCM einen privaten Schlüssel einmalig ausgibt.
 */
export class LcmLuKeyGuide extends LitElement {
  static override properties = { username: { type: String } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        font-size: var(--fs-s);
      }
      h4 {
        margin: var(--space-4) 0 var(--space-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
      }
      p {
        margin: 0 0 var(--space-2);
        color: var(--text-2);
      }
      ol {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.3rem;
      }
      li {
        margin-block-end: var(--space-2);
      }
      code,
      pre {
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
      pre {
        margin: var(--space-1) 0 0;
        padding: var(--space-2) var(--space-3);
        overflow-x: auto;
        border-radius: var(--radius-s);
        background: var(--surface-1);
      }
    `,
  ];

  declare username: string;

  constructor() {
    super();
    this.username = '';
    new I18nController(this);
  }

  override render() {
    const file = privateKeyFile(this.username);
    const code = guideCommands(this.username, file);
    return html`
      <h4>${t('linuxUsers.keyGuide.linuxTitle')}</h4>
      <ol>
        <li>${t('linuxUsers.keyGuide.linuxSave', { file })}</li>
        <li>${t('linuxUsers.keyGuide.linuxPermissions')}<pre>${code.chmod}</pre></li>
        <li>${t('linuxUsers.keyGuide.connect')}<pre>${code.connect}</pre></li>
      </ol>
      <p>${t('linuxUsers.keyGuide.linuxConfig')}</p>
      <pre>${code.linuxConfig}</pre>
      <h4>${t('linuxUsers.keyGuide.windowsTitle')}</h4>
      <ol>
        <li>${t('linuxUsers.keyGuide.windowsSave', { file })}</li>
        <li>${t('linuxUsers.keyGuide.windowsDefault')}<pre>${code.windowsConfig}</pre></li>
        <li>${t('linuxUsers.keyGuide.windowsConnect')}<pre>${code.windowsConnect}</pre></li>
      </ol>
      <p>${t('linuxUsers.keyGuide.windowsPermissions')} ${t('linuxUsers.keyGuide.putty')}</p>
    `;
  }
}
customElements.define('lcm-lu-key-guide', LcmLuKeyGuide);
