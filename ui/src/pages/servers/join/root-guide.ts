import { LitElement, css, html } from 'lit';
import '../../../components/lcm-command';
import '../../../components/lcm-notice';
import { baseStyles } from '../../../components/shared';
import { I18nController } from '../../../lib/controllers';
import { t } from '../../../lib/i18n';
import { formStyles } from '../../account/form-styles';

const DROP_IN = '/etc/ssh/sshd_config.d/00-root-onboarding.conf';

/** Die Befehle der Anleitung; in jeder Sprache gleich. */
export const ROOT_GUIDE_COMMANDS = {
  passwd: 'sudo passwd root',
  allow: `echo 'PermitRootLogin yes' | sudo tee ${DROP_IN}`,
  restartDebian: 'sudo systemctl restart ssh',
  restartRedHat: 'sudo systemctl restart sshd',
  remove: `sudo rm ${DROP_IN}`,
} as const;

/**
 * <lcm-join-root-guide> – aufklappbare Anleitung, wie man den Root-Login per SSH für die Aufnahme kurz öffnet (und
 * danach wieder schließt). Alternative ohne all das: ein sudo-Benutzer als Login-Benutzer.
 */
export class LcmJoinRootGuide extends LitElement {
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
        font-size: var(--fs-s);
      }
      summary {
        color: var(--accent-text);
        font-weight: var(--fw-medium);
        cursor: pointer;
      }
      .body {
        display: grid;
        gap: var(--space-3);
        margin-block-start: var(--space-3);
        padding: var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-1);
        line-height: 1.55;
      }
      ol {
        display: grid;
        gap: var(--space-3);
        margin: 0;
        padding-inline-start: 1.2rem;
      }
      li {
        display: grid;
        gap: var(--space-2);
      }
      p {
        margin: 0;
      }
    `,
  ];

  constructor() {
    super();
    new I18nController(this);
  }

  #command(value: string) {
    return html`<lcm-command .value=${value} copy-label=${t('joinWizard.copy')} copied-label=${t('joinWizard.copied')}></lcm-command>`;
  }

  override render() {
    const c = ROOT_GUIDE_COMMANDS;
    return html`<details>
      <summary>${t('joinWizard.rootGuide.title')}</summary>
      <div class="body">
        <p>${t('joinWizard.rootGuide.intro')}</p>
        <ol>
          <li>${t('joinWizard.rootGuide.console')}</li>
          <li><span>${t('joinWizard.rootGuide.password')}</span>${this.#command(c.passwd)}</li>
          <li>
            <span>${t('joinWizard.rootGuide.allow')}</span>${this.#command(c.allow)}
            <span>${t('joinWizard.rootGuide.allowOld', { dir: '/etc/ssh/sshd_config.d', file: '/etc/ssh/sshd_config', line: 'PermitRootLogin yes' })}</span>
          </li>
          <li>
            <span>${t('joinWizard.rootGuide.restart')}</span>${this.#command(c.restartDebian)}
            <span>${t('joinWizard.rootGuide.restartRedHat')}</span>${this.#command(c.restartRedHat)}
          </li>
          <li>${t('joinWizard.rootGuide.join')}</li>
        </ol>
        <lcm-notice tone="warn">${t('joinWizard.rootGuide.after')}</lcm-notice>
        ${this.#command(c.remove)}
      </div>
    </details>`;
  }
}
customElements.define('lcm-join-root-guide', LcmJoinRootGuide);
