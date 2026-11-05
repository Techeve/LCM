import { LitElement, css, html } from 'lit';
import '../../../components/lcm-choice';
import '../../../components/lcm-empty-state';
import '../../../components/lcm-explainer';
import { columnStyles } from '../../../components/layout-styles';
import '../../../components/lcm-button';
import type { ChoiceOption } from '../../../components/lcm-choice';
import { baseStyles } from '../../../components/shared';
import { auth } from '../../../lib/auth';
import { I18nController, SubscribeController } from '../../../lib/controllers';
import { t } from '../../../lib/i18n';
import { router } from '../../../lib/router';
import { modeOf, reconnectIdOf, type JoinMode } from './join-model';
import './join-agent';
import './join-dsm';
import './join-routeros';
import './join-ssh';

const DOCS: Record<JoinMode, string> = {
  ssh: '/docs/serveraenderungen',
  agent: '/docs/agent',
  routeros: '/docs/routeros',
  dsm: '/docs/synology',
};

/**
 * Server hinzufügen (`/servers/join?mode=ssh|agent|routeros|dsm`) und Server neu verbinden (`?reconnect=42`):
 * Verbindungsart wählen, dann der Assistent der Art; rechts die Erklärung, was LCM auf dem Ziel tut.
 */
export class JoinPage extends LitElement {
  static override properties = { _locked: { state: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .head {
        display: grid;
        gap: var(--space-2);
        margin-block-end: var(--space-5);
      }
      .back {
        color: var(--accent-text);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .main {
        display: grid;
        gap: var(--space-5);
        min-inline-size: 0;
      }
      lcm-explainer ol {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.1rem;
      }
    `,
    columnStyles,
  ];

  declare _locked: boolean;

  constructor() {
    super();
    this._locked = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #mode(): JoinMode {
    return modeOf(router.state.query.get('mode'));
  }

  get #reconnectId(): number | null {
    return reconnectIdOf(router.state.query.get('reconnect'));
  }

  #modes(): ChoiceOption[] {
    return [
      { id: 'ssh', label: t('joinWizard.mode.ssh'), icon: 'terminal', description: t('joinWizard.mode.sshText') },
      { id: 'agent', label: t('joinWizard.mode.agent'), icon: 'bolt', description: t('joinWizard.mode.agentText') },
      { id: 'routeros', label: t('joinWizard.mode.routeros'), icon: 'firewall', description: t('joinWizard.mode.routerosText') },
      { id: 'dsm', label: t('joinWizard.mode.dsm'), icon: 'disk', description: t('joinWizard.mode.dsmText') },
    ];
  }

  #wizard(mode: JoinMode, reconnectId: number | null) {
    if (reconnectId !== null) return html`<lcm-join-ssh reconnect-id=${reconnectId}></lcm-join-ssh>`;
    switch (mode) {
      case 'agent':
        return html`<lcm-join-agent></lcm-join-agent>`;
      case 'routeros':
        return html`<lcm-join-routeros></lcm-join-routeros>`;
      case 'dsm':
        return html`<lcm-join-dsm></lcm-join-dsm>`;
      default:
        return html`<lcm-join-ssh></lcm-join-ssh>`;
    }
  }

  #explainerBody(mode: JoinMode, reconnecting: boolean) {
    if (reconnecting) {
      return html`<p>${t('joinWizard.explainer.reconnect.intro')}</p>
        <ul>
          <li>${t('joinWizard.explainer.reconnect.user')}</li>
          <li>${t('joinWizard.explainer.reconnect.key')}</li>
          <li>${t('joinWizard.explainer.reconnect.keep')}</li>
        </ul>`;
    }
    switch (mode) {
      case 'agent':
        return html`<p>${t('joinWizard.explainer.agent.intro')}</p>
          <ul>
            <li>${t('joinWizard.explainer.agent.outbound')}</li>
            <li>${t('joinWizard.explainer.agent.token')}</li>
            <li>${t('joinWizard.explainer.agent.after')}</li>
          </ul>`;
      case 'routeros':
        return html`<p>${t('joinWizard.explainer.routeros.intro')}</p>
          <ul>
            <li>${t('joinWizard.explainer.routeros.watch')}</li>
            <li>${t('joinWizard.explainer.routeros.limits')}</li>
            <li>${t('joinWizard.explainer.routeros.user')}</li>
          </ul>`;
      case 'dsm':
        return html`<p>${t('joinWizard.explainer.dsm.intro')}</p>
          <ul>
            <li>${t('joinWizard.explainer.dsm.watch')}</li>
            <li>${t('joinWizard.explainer.dsm.cert')}</li>
            <li>${t('joinWizard.explainer.dsm.account')}</li>
          </ul>`;
      default:
        return html`<p>${t('joinWizard.explainer.ssh.intro')}</p>
          <ol>
            <li>${t('joinWizard.explainer.ssh.fingerprint')}</li>
            <li>${t('joinWizard.explainer.ssh.login')}</li>
            <li>${t('joinWizard.explainer.ssh.user')}</li>
            <li>${t('joinWizard.explainer.ssh.scan')}</li>
          </ol>
          <p>${t('joinWizard.explainer.ssh.notDone')}</p>`;
    }
  }

  override render() {
    if (!auth.can('servers:write')) {
      return html`<lcm-empty-state icon="lock" level="1" heading=${t('app.forbidden.title')} text=${t('joinWizard.forbidden')}>
        <lcm-button slot="action" variant="primary" href="/servers">${t('joinWizard.toServers')}</lcm-button>
      </lcm-empty-state>`;
    }
    const mode = this.#mode;
    const reconnectId = this.#reconnectId;
    const reconnecting = reconnectId !== null;
    return html`<div class="head">
        <a class="back" href=${reconnecting ? `/servers/${reconnectId}` : '/servers'}>${reconnecting ? t('joinWizard.toServer') : t('joinWizard.toServers')}</a>
        <h1>${reconnecting ? t('joinWizard.reconnect.title') : t('joinWizard.title')}</h1>
      </div>
      <div class="layout explain">
        <div class="main" @lcm-join-lock=${(event: CustomEvent<{ locked: boolean }>) => (this._locked = event.detail.locked)}>
          ${reconnecting
            ? ''
            : html`<lcm-choice
                label=${t('joinWizard.mode.label')}
                .options=${this.#modes()}
                value=${mode}
                ?disabled=${this._locked}
                @lcm-change=${(event: CustomEvent<{ value: JoinMode }>) => router.setQuery({ mode: event.detail.value === 'ssh' ? null : event.detail.value })}
              ></lcm-choice>`}
          ${this.#wizard(mode, reconnectId)}
        </div>
        <lcm-explainer
          topic="server-join"
          heading=${reconnecting ? t('joinWizard.explainer.reconnectTitle') : t('joinWizard.explainer.title')}
          docs-href=${DOCS[reconnecting ? 'ssh' : mode]}
          docs-label=${t('joinWizard.explainer.docs')}
          collapse-label=${t('joinWizard.explainer.collapse')}
          expand-label=${t('joinWizard.explainer.expand')}
          >${this.#explainerBody(mode, reconnecting)}</lcm-explainer
        >
      </div>`;
  }
}
customElements.define('lcm-page-server-join', JoinPage);
