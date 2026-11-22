import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-card';
import '../../../components/lcm-choice';
import '../../../components/lcm-explainer';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import '../endpoints/acme-card';
import '../endpoints/endpoint-dialog';
import './proxy-card';
import '../system/copy-field';
import type { ChoiceOption } from '../../../components/lcm-choice';
import type { IconName } from '../../../components/icons';
import { baseStyles, emit } from '../../../components/shared';
import type { EndpointName } from '../../../api/endpoints';
import type { GlobalSettings } from '../../../api/settings';
import { auth } from '../../../lib/auth';
import { I18nController } from '../../../lib/controllers';
import { t } from '../../../lib/i18n';
import { settingsStyles } from '../system/settings-styles';
import type { Draft, FieldErrors } from '../system/form-state';
import { addressHelp } from './address-help';
import {
  ADDRESS_MODES,
  ENABLED_KEY,
  HOST_KEY,
  MODE_KEY,
  PORT_KEY,
  SERVICES,
  SHARED_PORT_KEY,
  URL_KEY,
  certificateNames,
  cleanEntry,
  enabledOf,
  entryOf,
  hostOf,
  isLocked,
  portDefaults,
  readMode,
  sharedNames,
  sharedSwitchOff,
  shownUrls,
  uiPortChange,
  urlOfEntry,
  type AddressMode,
} from './address-model';
import { serviceSwitch } from './service-switch';

const MODE_ICON: Record<AddressMode, IconName> = { separate: 'server', one_name: 'globe', shared_port: 'firewall' };

type FieldOptions = { label: string; width?: 'xs' | 's' | 'm' | 'l'; placeholder?: string; hint?: string; port?: boolean };

/**
 * <lcm-address-block .settings .draft .errors> – der Block „Öffentliche Adressen“ in Einstellungen → Allgemein:
 * Adressmodus, Adressen und Ports je Modus, die Schalter für Agent und MCP und Let's Encrypt. Die Felder gehören dem
 * Entwurf der Seite; der Block meldet jede Eingabe als `lcm-address-change` { values } und speichert nichts selbst
 * (Let's Encrypt hat einen eigenen Endpunkt und speichert für sich). Am Zahnrad einer Adresse steht „Zertifikat &
 * Sicherheit“ mit den Netzbereichen der Schnittstelle. Mit `wizard` (Einrichtungsassistent) entfallen Zahnrad, Let's Encrypt,
 * Reverse-Proxy, die Hilfe (sie steht dort in der Spalte des Assistenten) und die Schalter für Agent und MCP; die Vorschau
 * der Adressen hat dort keine Kopierknöpfe.
 */
export class LcmAddressBlock extends LitElement {
  static override properties = {
    settings: { attribute: false },
    draft: { attribute: false },
    errors: { attribute: false },
    wizard: { type: Boolean, reflect: true },
    _endpoint: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      .address {
        display: flex;
        flex-wrap: wrap;
        align-items: flex-start;
        gap: var(--space-4);
      }
      .address > span:empty {
        display: none;
      }
      /* Das Zahnrad steht auf der Höhe des Eingabefelds, unter dessen Beschriftung. */
      .address lcm-button {
        flex: none;
        margin-block-start: calc(var(--fs-s) * 1.5 + var(--space-1));
      }
      /* Je Schnittstelle eine Zeile (Unterraster): Name, Adresse, Zahnrad; alle drei Zellen sind immer da. */
      ul.derived {
        display: grid;
        grid-template-columns: max-content minmax(0, 1fr) auto;
        align-items: center;
        gap: var(--space-3) var(--space-4);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      ul.derived li {
        display: grid;
        grid-column: 1 / -1;
        grid-template-columns: subgrid;
        align-items: center;
      }
      .service {
        color: var(--text-2);
        font-size: var(--fs-s);
        white-space: nowrap;
      }
      .inactive {
        font-weight: var(--fw-medium);
      }
      .url {
        min-inline-size: 0;
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        overflow-wrap: anywhere;
      }
      /* Schmal: Name und Zahnrad in einer Zeile, die Adresse darunter in voller Breite. */
      @media (max-width: 45rem) {
        ul.derived {
          grid-template-columns: minmax(0, 1fr) auto;
          row-gap: var(--space-4);
        }
        ul.derived li {
          row-gap: var(--space-1);
        }
        ul.derived li > :nth-child(1) {
          grid-area: 1 / 1;
        }
        ul.derived li > :nth-child(2) {
          grid-area: 2 / 1 / 3 / 3;
        }
        ul.derived li > :nth-child(3) {
          grid-area: 1 / 2;
        }
      }
      h3 {
        margin: 0;
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
      }
      /* Auf breiten Bildschirmen steht die Hilfe in der Spalte der Seite, darunter hier im Block. */
      .inline-help {
        inline-size: auto;
        position: static;
      }
      @media (min-width: 75.01rem) {
        .inline-help {
          display: none;
        }
      }
    `,
  ];

  declare settings: GlobalSettings;
  declare draft: Draft;
  /** Fehlertexte je Feld, fertig übersetzt. */
  declare errors: FieldErrors;
  /** Einrichtungsassistent: nur Modus, Adressen und Ports, ohne Zertifikatsfenster, Let's Encrypt und Schalter. */
  declare wizard: boolean;
  /** Die Schnittstelle, deren Fenster für das Zertifikat offen ist. */
  declare _endpoint: EndpointName | undefined;
  /** Was im Namensfeld getippt wurde, solange es zur Adresse im Entwurf passt (`:443` verschwände sonst beim Tippen). */
  #typed: Record<string, string> = {};

  constructor() {
    super();
    this.settings = {};
    this.draft = {};
    this.errors = {};
    this.wizard = false;
    new I18nController(this);
  }

  get #mode(): AddressMode {
    return readMode(this.draft[MODE_KEY]);
  }

  #value(key: string): string {
    return String(this.draft[key] ?? '');
  }

  #change(values: Record<string, string | boolean>): void {
    emit(this, 'lcm-address-change', { values });
  }

  #setMode(mode: AddressMode): void {
    const values: Record<string, string> = { [MODE_KEY]: mode, ...portDefaults(this.draft, mode) };
    const switches: Record<string, boolean> = {};
    if (mode === 'shared_port') {
      const names = sharedNames(this.draft);
      Object.assign(values, names);
      Object.assign(switches, sharedSwitchOff(this.draft, names));
    }
    this.#change({ ...values, ...switches });
  }

  get #modes(): ChoiceOption[] {
    return ADDRESS_MODES.map((mode) => ({
      id: mode,
      label: t(`settings.general.address.mode.${mode}.title`),
      description: t(`settings.general.address.mode.${mode}.text`),
      icon: MODE_ICON[mode],
    }));
  }

  #input(key: string, options: FieldOptions, value = this.#value(key), set: (value: string) => void = (next) => this.#change({ [key]: next })): TemplateResult {
    const locked = options.port === true && isLocked(this.settings, key);
    return html`<lcm-input
      data-key=${key}
      label=${options.label}
      width=${options.width ?? 'm'}
      placeholder=${options.placeholder ?? ''}
      hint=${locked ? t('settings.general.address.locked') : (options.hint ?? '')}
      error=${this.errors[key] ?? ''}
      autocomplete="off"
      maxlength=${options.port ? 5 : nothing}
      ?digits=${options.port === true}
      ?mono=${true}
      ?disabled=${locked}
      .value=${value}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => set(e.detail.value)}
    ></lcm-input>`;
  }

  /** Das Zahnrad; ohne Recht und im Assistenten eine leere Zelle, damit das Raster der Vorschau stimmt. */
  #gear(service: EndpointName): TemplateResult {
    if (this.wizard || !auth.can('system:endpoints')) return html`<span></span>`;
    const name = t(`settings.general.address.interface.${service}`);
    return html`<lcm-button
      variant="ghost"
      icon="settings"
      icon-only
      data-endpoint=${service}
      label=${t('settings.general.address.configure', { name })}
      @click=${() => (this._endpoint = service)}
    ></lcm-button>`;
  }

  #address(service: EndpointName, ...fields: TemplateResult[]): TemplateResult {
    return html`<div class="address" role="group" aria-label=${t(`settings.general.address.interface.${service}`)}>${fields} ${this.#gear(service)}</div>`;
  }

  /** Im Einrichtungsassistenten haben Agent und MCP einen eigenen Schritt. */
  #switch(service: 'agent' | 'mcp'): TemplateResult | typeof nothing {
    if (this.wizard) return nothing;
    return serviceSwitch({
      service,
      draft: this.draft,
      settings: this.settings,
      onChange: (key, checked) => this.#change({ [key]: checked }),
      noNameHint: t('settings.general.address.noDomain'),
    });
  }

  #portField(service: EndpointName, label: string): TemplateResult {
    return this.#input(PORT_KEY[service], { label, width: 'xs', port: true });
  }

  /** Der Text eines Namensfelds: was der Benutzer getippt hat, solange es zur Adresse im Entwurf passt, sonst `name[:port]` daraus. */
  #entryText(key: string, withPort: boolean): string {
    const typed = this.#typed[key];
    const url = this.#value(key);
    if (typed !== undefined && (withPort ? urlOfEntry(typed) : this.#sharedUrl(typed)) === url) return typed;
    return withPort ? entryOf(url) : hostOf(url);
  }

  #sharedUrl(entry: string): string {
    const host = hostOf(entry);
    return host === '' ? '' : `https://${host}`;
  }

  /** Ein Namensfeld: A nimmt `name` oder `name:port`, C nur `name`. Die Adresse im Entwurf ist immer `https://…`. */
  #nameField(service: EndpointName, withPort: boolean, hint = ''): TemplateResult {
    const key = URL_KEY[service];
    const label = t(`settings.general.address.name.${service}`);
    const set = (value: string): void => {
      this.#typed = { ...this.#typed, [key]: value };
      if (withPort) this.#change({ [key]: urlOfEntry(cleanEntry(value)) });
      else this.#setSharedName(service, value);
    };
    return this.#input(key, { label, width: 'l', placeholder: `${service === 'ui' ? 'lcm' : service}.example.com`, hint }, this.#entryText(key, withPort), set);
  }

  /** Modus A: je Schnittstelle der Name (bei einem Proxy auf anderem Port `name:port`) und der Port, auf dem LCM lauscht. */
  #separate(): TemplateResult {
    const hints: Record<EndpointName, string> = {
      ui: t('settings.general.address.hint'),
      agent: t('settings.general.address.agentHint'),
      mcp: t('settings.general.address.mcpHint'),
    };
    return html`${SERVICES.map(
      (service) => html`${this.#address(service, this.#nameField(service, true, hints[service]), this.#portField(service, t('settings.general.address.portLabel')))}
      ${service === 'ui' ? nothing : this.#switch(service)}`,
    )}
    ${this.#value(URL_KEY.ui).trim() === '' ? html`<lcm-notice tone="warn">${t('settings.general.address.warning')}</lcm-notice>` : nothing}`;
  }

  /** Eine Adresse der Vorschau in Modus B: Name der Schnittstelle, Adresse (im Assistenten nur Text), Zahnrad. */
  #derivedRow(service: EndpointName, url: string): TemplateResult {
    const inactive = !enabledOf(this.draft)[service];
    return html`<li>
      <span class="service">
        ${t(`settings.general.address.interface.${service}`)}${inactive ? html` <span class="inactive" data-field=${`inactive-${service}`}>${t('settings.general.address.inactive')}</span>` : nothing}
      </span>
      ${this.#derivedUrl(service, url)}
      ${this.#gear(service)}
    </li>`;
  }

  #derivedUrl(service: EndpointName, url: string): TemplateResult {
    if (!url) return html`<span class="muted">${t('settings.general.address.derivedNone')}</span>`;
    if (this.wizard) return html`<span class="url" data-field=${`url-${service}`}>${url}</span>`;
    return html`<lcm-copy-field data-field=${`url-${service}`} value=${url}></lcm-copy-field>`;
  }

  /** Modus B: ein Name, drei Ports; die Adressen daraus stehen schreibgeschützt darunter (im Assistenten ohne Kopierknöpfe). */
  #oneName(): TemplateResult {
    const urls = shownUrls(this.draft, this.settings);
    return html`${this.#input(HOST_KEY, { label: t('settings.general.address.hostLabel'), width: 'l', placeholder: 'lcm.example.com', hint: t('settings.general.address.hostHint') })}
      <div class="field-row">${SERVICES.map((service) => this.#portField(service, t(`settings.general.address.portOf.${service}`)))}</div>
      <h3>${t('settings.general.address.derived')}</h3>
      <ul class="derived">
        ${SERVICES.map((service) => this.#derivedRow(service, urls[service]))}
      </ul>
      ${this.#switch('agent')} ${this.#switch('mcp')}`;
  }

  /** Modus C, ein Name: Wer einem leeren Feld des Agenten oder von MCP einen Namen gibt, schaltet die Schnittstelle ein; ein leeres Feld schaltet sie aus. */
  #setSharedName(service: EndpointName, value: string): void {
    const key = URL_KEY[service];
    const host = hostOf(value);
    const values: Record<string, string | boolean> = { [key]: host === '' ? '' : `https://${host}` };
    if (service !== 'ui' && (host === '' || hostOf(this.#value(key)) === '')) values[ENABLED_KEY[service]] = host !== '';
    this.#change(values);
  }

  /** Modus C: drei Namen (nur der der Weboberfläche ist Pflicht, die gesetzten verschieden) und der gemeinsame Port. */
  #sharedPort(): TemplateResult {
    const name = (service: EndpointName): TemplateResult => html`${this.#address(service, this.#nameField(service, false))} ${service === 'ui' ? nothing : this.#switch(service)}`;
    return html`${name('ui')} ${name('agent')} ${name('mcp')}
    ${this.#input(SHARED_PORT_KEY, { label: t('settings.general.address.sharedPort'), width: 'xs', port: true, hint: t('settings.general.address.sharedPortHint') })}`;
  }

  #fields(): TemplateResult {
    switch (this.#mode) {
      case 'one_name':
        return this.#oneName();
      case 'shared_port':
        return this.#sharedPort();
      default:
        return this.#separate();
    }
  }

  /** Ein Wechsel des Oberflächen-Ports startet den Listener neu: Die neue Adresse steht schon vor dem Speichern da. */
  #portNotice(): TemplateResult | typeof nothing {
    const address = uiPortChange(this.draft, this.settings, window.location.href);
    return address ? html`<lcm-notice tone="warn" heading=${t('settings.general.address.portChange.title')}>${t('settings.general.address.portChange.text', { address })}</lcm-notice>` : nothing;
  }

  #help(): TemplateResult | typeof nothing {
    if (this.wizard) return nothing;
    return html`<lcm-explainer
      class="inline-help"
      topic="settings-addresses"
      heading=${t('settings.general.address.help.title')}
      collapse-label=${t('settings.form.collapse')}
      expand-label=${t('settings.form.expand')}
    >
      ${addressHelp(this.draft)}
    </lcm-explainer>`;
  }

  #letsEncrypt(): TemplateResult | typeof nothing {
    if (this.wizard || !auth.can('system:endpoints')) return nothing;
    const { saved, pending } = certificateNames(this.draft, this.settings);
    return html`<lcm-settings-acme .names=${saved} ?names-pending=${pending}></lcm-settings-acme>
      <lcm-settings-proxy .settings=${this.settings}></lcm-settings-proxy>`;
  }

  #dialog(): TemplateResult | typeof nothing {
    if (this.wizard) return nothing;
    const service = this._endpoint;
    return html`<lcm-endpoint-dialog
      ?open=${service !== undefined}
      endpoint=${service ?? 'ui'}
      draft-address=${service ? shownUrls(this.draft, this.settings)[service] : ''}
      @lcm-close=${() => (this._endpoint = undefined)}
    ></lcm-endpoint-dialog>`;
  }

  override render() {
    return html`<lcm-card level="2" heading=${t('settings.general.address.title')} subtitle=${t('settings.general.address.subtitle')}>
        <div class="fields">
          <lcm-choice
            data-field="address-mode"
            label=${t('settings.general.address.modeLabel')}
            variant="cards"
            .options=${this.#modes}
            .value=${this.#mode}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#setMode(readMode(e.detail.value))}
          ></lcm-choice>
          ${this.#fields()} ${this.#portNotice()} ${this.#letsEncrypt()} ${this.#help()}
        </div>
      </lcm-card>
      ${this.#dialog()}`;
  }
}
customElements.define('lcm-address-block', LcmAddressBlock);
