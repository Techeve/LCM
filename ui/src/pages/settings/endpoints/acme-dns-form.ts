import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import '../../../components/lcm-select';
import '../../../components/lcm-switch';
import '../../../components/lcm-textarea';
import type { Option } from '../../../components/lcm-select';
import { baseStyles, emit } from '../../../components/shared';
import { endpointsApi, type AcmeState, type DnsField, type DnsProvider, type DnsTestBody } from '../../../api/endpoints';
import { I18nController } from '../../../lib/controllers';
import { errorText, i18n, t } from '../../../lib/i18n';
import { settingsStyles } from '../system/settings-styles';
import type { AcmeDraft } from './acme-model';
import {
  MAX_NAMESERVERS,
  dnsBodyOf,
  fieldKey,
  fieldWidth,
  missingDnsField,
  parseNameservers,
  providerOf,
  runsSlowly,
  secretStored,
  switchProvider,
  type DnsDraft,
} from './acme-dns-model';

type TestPhase = 'idle' | 'running' | 'ok' | 'failed';

/**
 * <lcm-acme-dns> – Eingaben zu DNS-01 in der Karte „Let's Encrypt“: Anbieter, die Felder des Anbieters, Prüf-
 * Nameserver und die Probe. Das Formular entsteht aus den Feldern, die die API je Anbieter liefert. Jede Änderung
 * meldet `lcm-dns-change` mit dem neuen `DnsDraft`; gespeichert wird in der Karte.
 */
export class LcmAcmeDns extends LitElement {
  static override properties = {
    draft: { attribute: false },
    saved: { attribute: false },
    names: { attribute: false },
    errors: { attribute: false },
    _phase: { state: true },
    _result: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      .secret {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .stored {
        gap: var(--space-2);
      }
      details.more > summary {
        inline-size: fit-content;
        margin-block-end: var(--space-3);
        color: var(--accent-text);
        font-weight: var(--fw-medium);
        cursor: pointer;
      }
    `,
  ];

  declare draft: AcmeDraft;
  /** Der gespeicherte Stand: Er liefert die Anbieter und sagt, welche Geheimnisse schon gesetzt sind. */
  declare saved: AcmeState;
  /** Die Namen aus den gespeicherten Adressen, für die das Zertifikat ausgestellt wird. */
  declare names: string[];
  /** Fehlermeldungen der API je Feldname. */
  declare errors: Record<string, string>;
  declare _phase: TestPhase;
  declare _result: string;

  constructor() {
    super();
    this.names = [];
    this.errors = {};
    this._phase = 'idle';
    this._result = '';
    new I18nController(this);
  }

  #testedKey = '';

  /** Ein Ergebnis gilt nur für die Eingaben, mit denen die Probe lief. */
  override willUpdate(changed: PropertyValues): void {
    if (changed.has('draft') && this._phase !== 'running' && this.#testKey() !== this.#testedKey) this._phase = 'idle';
  }

  get #dns(): DnsDraft {
    return this.draft.dns;
  }

  #patch(patch: Partial<DnsDraft>): void {
    emit(this, 'lcm-dns-change', { ...this.#dns, ...patch });
  }

  #setValue(name: string, value: string): void {
    this.#patch({ values: { ...this.#dns.values, [name]: value } });
  }

  /** Leer heißt: nichts eingegeben, der gespeicherte Wert bleibt. */
  #setSecret(name: string, value: string): void {
    this.#patch({ secrets: { ...this.#dns.secrets, [name]: value === '' ? undefined : value } });
  }

  #deleteSecret(name: string, deleted: boolean): void {
    this.#patch({ secrets: { ...this.#dns.secrets, [name]: deleted ? '' : undefined } });
  }

  get #providers(): Option[] {
    const known = this.saved.dnsProviders.map((provider) => ({ value: provider.id, label: i18n.tryT(`settings.acme.dns.providers.${provider.id}`) ?? provider.id }));
    return [{ value: '', label: t('settings.acme.dns.providerChoose') }, ...known];
  }

  #labelOf(provider: string, field: DnsField): string {
    const label = t(`${fieldKey(provider, field.name)}.label`);
    return field.required ? label : t('settings.acme.dns.optional', { label });
  }

  #selectOptions(field: DnsField): Option[] {
    return field.options.map((value) => ({ value, label: field.name === 'mode' ? t(`settings.acme.dns.option.${value}`) : value }));
  }

  #secret(provider: string, field: DnsField): TemplateResult {
    const stored = secretStored(this.#dns, this.saved, field.name);
    const entered = this.#dns.secrets[field.name];
    const deleted = entered === '';
    return html`<div class="secret">
      <lcm-input
        data-field=${field.name}
        label=${this.#labelOf(provider, field)}
        width=${fieldWidth(field)}
        type="password"
        autocomplete="new-password"
        mono
        ?required=${field.required}
        placeholder=${stored ? t('settings.acme.dns.secretPlaceholder') : ''}
        hint=${t(`${fieldKey(provider, field.name)}.hint`)}
        error=${this.errors[field.name] ?? ''}
        ?disabled=${deleted}
        .value=${entered ?? ''}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setSecret(field.name, e.detail.value)}
      ></lcm-input>
      ${stored
        ? html`<div class="row stored">
            <span class="muted">${t(deleted ? 'settings.acme.dns.secretCleared' : 'settings.acme.dns.secretStored')}</span>
            <lcm-button size="small" variant="ghost" @click=${() => this.#deleteSecret(field.name, !deleted)}>
              ${t(deleted ? 'settings.acme.dns.secretKeep' : 'settings.acme.dns.secretDelete')}
            </lcm-button>
          </div>`
        : nothing}
    </div>`;
  }

  #field(provider: string, field: DnsField): TemplateResult {
    const key = fieldKey(provider, field.name);
    const value = this.#dns.values[field.name] ?? '';
    switch (field.type) {
      case 'secret':
        return this.#secret(provider, field);
      case 'bool':
        return html`<lcm-switch
          data-field=${field.name}
          label=${this.#labelOf(provider, field)}
          description=${t(`${key}.hint`)}
          on-label=${t('settings.form.on')}
          off-label=${t('settings.form.off')}
          .checked=${value === 'true'}
          @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#setValue(field.name, String(e.detail.checked))}
        ></lcm-switch>`;
      case 'select':
        return html`<lcm-select
          data-field=${field.name}
          label=${this.#labelOf(provider, field)}
          width="m"
          hint=${t(`${key}.hint`)}
          .options=${this.#selectOptions(field)}
          .value=${value || field.default}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#setValue(field.name, e.detail.value)}
        ></lcm-select>`;
      default:
        return html`<lcm-input
          data-field=${field.name}
          label=${this.#labelOf(provider, field)}
          width=${fieldWidth(field)}
          ?digits=${field.type === 'number'}
          maxlength=${field.type === 'number' ? 4 : nothing}
          ?required=${field.required}
          placeholder=${field.default}
          hint=${t(`${key}.hint`)}
          error=${this.errors[field.name] ?? ''}
          .value=${value}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setValue(field.name, e.detail.value)}
        ></lcm-input>`;
    }
  }

  #notes(): TemplateResult[] {
    const provider = this.#dns.provider;
    const notes: TemplateResult[] = [];
    if (provider === 'acmedns') notes.push(html`<lcm-notice>${t('settings.acme.dns.note.acmedns')}</lcm-notice>`);
    if (provider === 'exec') notes.push(html`<lcm-notice>${t('settings.acme.dns.note.exec')}</lcm-notice>`);
    if (runsSlowly(provider, this.names.length)) notes.push(html`<lcm-notice>${t('settings.acme.dns.note.slow')}</lcm-notice>`);
    return notes;
  }

  /** Die Probe braucht einen Anbieter mit allen Pflichtfeldern und einen Namen (aus den Adressen oder gespeichert). */
  get #testBlocked(): boolean {
    const names = this.names.length + this.saved.names.length;
    const nameservers = parseNameservers(this.#dns.nameservers).length;
    return this._phase === 'running' || this.saved.running || this.#dns.provider === '' || names === 0 || nameservers > MAX_NAMESERVERS || missingDnsField(this.#dns, this.saved) !== null;
  }

  /** Ohne Namen aus den Adressen gelten die gespeicherten: Die API nimmt sie dann selbst. */
  #testBody(): DnsTestBody {
    return { ...dnsBodyOf(this.#dns, this.saved), ...(this.names.length > 0 ? { names: this.names } : {}) };
  }

  #testKey(): string {
    return JSON.stringify(this.#testBody());
  }

  async #test(): Promise<void> {
    const key = this.#testKey();
    this.#testedKey = key;
    this._phase = 'running';
    try {
      await endpointsApi.testAcmeDns(this.#testBody());
      this.#finishTest(key, 'ok', t('settings.acme.dns.testOk'));
    } catch (error) {
      this.#finishTest(key, 'failed', errorText(error));
    }
  }

  #finishTest(key: string, phase: TestPhase, result: string): void {
    this._phase = this.#testKey() === key ? phase : 'idle';
    this._result = result;
  }

  #testResult(): TemplateResult | typeof nothing {
    if (this._phase === 'ok') return html`<lcm-notice tone="ok">${this._result}</lcm-notice>`;
    if (this._phase === 'failed') return html`<lcm-notice tone="crit">${this._result}</lcm-notice>`;
    return nothing;
  }

  /** Die Felder des Anbieters ohne Pflicht stehen mit den Prüf-Nameservern unter „Weitere Einstellungen“. */
  #more(provider: DnsProvider | undefined): TemplateResult {
    const optional = provider?.fields.filter((field) => !field.required) ?? [];
    return html`<details class="more" data-field="dns-more">
      <summary>${t('settings.acme.dns.more')}</summary>
      <div class="fields">
        ${optional.map((field) => this.#field(provider!.id, field))}
        <lcm-textarea
          data-field="dns-nameservers"
          label=${t('settings.acme.dns.nameservers')}
          width="m"
          mono
          rows=${MAX_NAMESERVERS}
          placeholder=${'ns1.example.com'}
          hint=${t('settings.acme.dns.nameserversHint', { max: MAX_NAMESERVERS })}
          .value=${this.#dns.nameservers}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ nameservers: e.detail.value })}
        ></lcm-textarea>
      </div>
    </details>`;
  }

  override render() {
    const dns = this.#dns;
    const provider = providerOf(this.saved, dns.provider);
    const running = this._phase === 'running';
    return html`<div class="fields">
      <lcm-select
        data-field="dns-provider"
        label=${t('settings.acme.dns.provider')}
        width="l"
        hint=${t('settings.acme.dns.providerHint')}
        .options=${this.#providers}
        .value=${dns.provider}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => emit(this, 'lcm-dns-change', switchProvider(dns, this.saved, e.detail.value))}
      ></lcm-select>
      ${provider?.fields.filter((field) => field.required).map((field) => this.#field(provider.id, field))} ${this.#notes()} ${this.#more(provider)}
      <div class="row">
        <lcm-button icon="play" ?busy=${running} ?disabled=${this.#testBlocked} @click=${() => void this.#test()}>${t('settings.acme.dns.test')}</lcm-button>
        <span class="muted" role="status">${running ? t('settings.acme.dns.testRunning') : t('settings.acme.dns.testHint')}</span>
      </div>
      ${this.#testResult()}
    </div>`;
  }
}
customElements.define('lcm-acme-dns', LcmAcmeDns);
