import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-card';
import '../../../components/lcm-choice';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import '../../../components/lcm-select';
import '../../../components/lcm-status';
import '../../../components/lcm-switch';
import './acme-dns-form';
import type { ChoiceOption } from '../../../components/lcm-choice';
import type { Option } from '../../../components/lcm-select';
import { toast } from '../../../components/lcm-toaster';
import { baseStyles, emit } from '../../../components/shared';
import { saveBlob } from '../../../lib/download';
import { ApiError } from '../../../lib/api';
import { ACME_URL, endpointsApi, readAcmeState, type AcmeCertificate, type AcmeState } from '../../../api/endpoints';
import { I18nController } from '../../../lib/controllers';
import { formatDate, formatDateTime, formatRelative } from '../../../lib/format';
import { errorText, t } from '../../../lib/i18n';
import { LiveData } from '../../../lib/live-data';
import { unsaved } from '../../../lib/unsaved';
import { LIMITS } from '../../../lib/validate';
import { settingsStyles } from '../system/settings-styles';
import { fieldKey, missingDnsField, type DnsDraft } from './acme-dns-model';
import { bodyOf, chainFileName, coversNames, directoryHost, directoryOf, draftOf, isDirty, listenerNeedsCapability, needsTerms, problemOf, statusOf, termsUrl, type AcmeCa, type AcmeDraft, type AcmeProblem } from './acme-model';

/** Wie oft der Zustand nachgeladen wird, solange ein Lauf aktiv ist. */
const POLL_MS = 2000;

/**
 * <lcm-settings-acme .names=${[…]} ?names-pending> – Abschnitt „Let's Encrypt“ im Block Öffentliche Adressen:
 * Zertifikate für die öffentlichen Adressen holt und erneuert LCM selbst bei einer ACME-CA, immer eines für alle
 * Namen. Die Namen kommen von außen (aus den gespeicherten Adressen) und sind hier nur zu sehen; `names-pending`
 * sagt, dass die Adressen in der Seite schon anders stehen. Eingeklappt, bis der Benutzer es öffnet (`open` öffnet
 * ihn von Anfang an, im Einrichtungsassistenten). Ungespeicherte Eingaben meldet der Abschnitt im Register
 * `unsaved`. Der Abschnitt lädt und speichert für sich (eigener Endpunkt) und hat deshalb seine eigenen Knöpfe im Fuß.
 * Mit `wizard` (Einrichtungsassistent) ist der Hauptknopf „Zertifikat anfordern“: Er speichert, stößt die Ausstellung an
 * (wo das Speichern sie nicht schon startete) und zeigt Fortschritt, Ergebnis oder Fehler. Jede Änderung des Stands
 * meldet der Abschnitt als `lcm-acme-state` { state }, damit der Schritt weiß, ob ein Zertifikat da ist.
 */
export class LcmSettingsAcme extends LitElement {
  static override properties = {
    names: { attribute: false },
    namesPending: { type: Boolean, attribute: 'names-pending' },
    open: { type: Boolean },
    wizard: { type: Boolean },
    _draft: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _fieldErrors: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      h3 {
        margin: 0;
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
      }
      p {
        margin: 0;
      }
      .block {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line-muted);
      }
      .terms {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .terms a {
        align-self: flex-start;
        color: var(--accent-text);
        font-size: var(--fs-s);
      }
      /* Die Meldung im Fuß füllt die Zeile: Als Flex-Kind ohne Breite schrumpfte sie auf die Mindestbreite eines Zeichens. */
      .row > lcm-notice {
        flex: 1 1 100%;
      }
      .cert {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        box-shadow: inset 0 0 0 1px var(--line);
      }
    `,
  ];

  declare names: string[];
  declare namesPending: boolean;
  declare open: boolean;
  declare wizard: boolean;
  declare _draft: AcmeDraft | undefined;
  declare _busy: boolean;
  declare _error: string;
  /** Fehler der API zu einem Feld des DNS-Anbieters, je Feldname. */
  declare _fieldErrors: Record<string, string>;

  readonly #state = new LiveData<AcmeState>(this, { url: () => ACME_URL, transform: readAcmeState });
  #source: AcmeState | undefined;
  /** Der letzte Stand aus dem Datenspeicher, den die Karte übernommen hat. */
  #received: AcmeState | undefined;
  #timer = 0;

  constructor() {
    super();
    this.names = [];
    this.namesPending = false;
    this.open = false;
    this.wizard = false;
    this._busy = false;
    this._error = '';
    this._fieldErrors = {};
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopPolling();
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    const data = this.#state.data;
    // `#source` wechselt auch beim eigenen Speichern; verglichen wird mit dem, was schon verarbeitet ist. Sonst
    // holte jedes Neuzeichnen nach dem Speichern den alten Stand zurück und überschriebe Entwurf und Stand damit.
    if (!data || data === this.#received) return;
    this.#received = data;
    const wasDirty = this.#source !== undefined && this._draft !== undefined && isDirty(this._draft, this.#source);
    this.#source = data;
    emit(this, 'lcm-acme-state', { state: data });
    if (!this._draft || !wasDirty) {
      // Im Assistenten heißt „Let's Encrypt gewählt“: einschalten.
      const enabled = this.wizard || data.enabled;
      this._draft = { ...draftOf(data), enabled, acceptTerms: this._draft?.acceptTerms ?? false };
    }
  }

  override updated(changed: PropertyValues): void {
    super.updated(changed);
    unsaved.set(this, !!this._draft && !!this.#source && isDirty(this._draft, this.#source));
    if (this.#source?.running) this.#startPolling();
    else this.#stopPolling();
  }

  #startPolling(): void {
    if (!this.#timer) this.#timer = window.setInterval(() => this.#state.reload(), POLL_MS);
  }

  #stopPolling(): void {
    window.clearInterval(this.#timer);
    this.#timer = 0;
  }

  #set(patch: Partial<AcmeDraft>): void {
    if (this._draft) this._draft = { ...this._draft, ...patch };
    this._fieldErrors = {};
  }

  #discard(): void {
    if (this.#source) this._draft = draftOf(this.#source);
    this._error = '';
    this._fieldErrors = {};
  }

  /** Ein fehlendes Feld des DNS-Anbieters zeigt die Karte am Feld, alles andere als Meldung. */
  #fail(error: unknown): void {
    const field = error instanceof ApiError && error.code === 'acme.field_missing' ? String(error.params.field ?? '') : '';
    if (field.startsWith('dns_values.')) this._fieldErrors = { [field.slice('dns_values.'.length)]: t('settings.acme.dns.missing') };
    else this._error = errorText(error);
  }

  async #save(): Promise<void> {
    const draft = this._draft;
    const state = this.#source;
    if (!draft || !state || this._busy || problemOf(draft, state, this.names) || !isDirty(draft, state)) return;
    this._busy = true;
    this._error = '';
    this._fieldErrors = {};
    try {
      const saved = await endpointsApi.updateAcme(bodyOf(draft, state, this.names));
      this.#source = saved;
      this._draft = draftOf(saved);
      toast({ tone: 'ok', title: t('settings.acme.saved') });
    } catch (error) {
      this.#fail(error);
    } finally {
      this._busy = false;
    }
  }

  /**
   * Im Assistenten: speichert, was geändert ist, und stößt die Ausstellung an, wenn das Speichern sie nicht schon gestartet
   * hat (LCM startet sie von selbst, wenn kein Zertifikat alle Namen trägt). Den Lauf zeigt der Stand, den die Karte nachlädt.
   */
  async #request(): Promise<void> {
    const draft = this._draft;
    let state = this.#source;
    if (!draft || !state || this._busy || problemOf(draft, state, this.names)) return;
    this._busy = true;
    this._error = '';
    this._fieldErrors = {};
    try {
      if (isDirty(draft, state)) {
        state = await endpointsApi.updateAcme(bodyOf(draft, state, this.names));
        this.#source = state;
        this._draft = draftOf(state);
      }
      if (!state.running && !coversNames(state, this.names)) await endpointsApi.issueAcme();
      this.#state.reload();
    } catch (error) {
      this.#fail(error);
    } finally {
      this._busy = false;
    }
  }

  /** Bestätigt den Hinweis zum Umstieg; die Karte übernimmt den neuen Stand, ohne Entwurf und Eingaben anzufassen. */
  async #dismissNotice(): Promise<void> {
    try {
      await endpointsApi.ackAcmeNamesNotice();
      this.#state.reload();
    } catch (error) {
      this._error = errorText(error);
    }
  }

  async #issue(): Promise<void> {
    this._busy = true;
    this._error = '';
    try {
      await endpointsApi.issueAcme();
      toast({ tone: 'ok', title: t('settings.acme.issueStarted') });
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #cas(): Option[] {
    return [
      { value: 'letsencrypt', label: t('settings.acme.caLetsEncrypt') },
      { value: 'staging', label: t('settings.acme.caStaging') },
      { value: 'custom', label: t('settings.acme.caCustom') },
    ];
  }

  get #challenges(): ChoiceOption[] {
    return [
      { id: 'http-01', label: t('settings.acme.challengeHttp'), description: t('settings.acme.challengeText'), icon: 'globe' },
      { id: 'dns-01', label: t('settings.acme.challengeDns'), description: t('settings.acme.challengeDnsText'), icon: 'server' },
    ];
  }

  /**
   * Der Listener für HTTP-01 ist nur während einer Prüfung offen. Ließ er sich beim letzten Mal nicht öffnen,
   * steht der Grund hier; sonst steht ein Satz, wann der Port aufgeht.
   */
  #listener(state: AcmeState): TemplateResult | typeof nothing {
    const address = state.httpListen;
    if (state.challenge !== 'http-01' || !state.enabled) return nothing;
    if (state.listenerError) {
      return html`<lcm-notice tone="crit">
        ${t('settings.acme.listener.error', { address, error: state.listenerError })}
        ${listenerNeedsCapability(state) ? html`<br />${t('settings.acme.listener.capability')}` : nothing}
      </lcm-notice>`;
    }
    return html`<p class="muted">${t('settings.acme.listener.onDemand', { address })}</p>`;
  }

  /** Der einmalige Hinweis, wenn die Namen aus den Adressen von denen abweichen, für die ältere Versionen ausstellten. */
  #namesNotice(state: AcmeState): TemplateResult | typeof nothing {
    const notice = state.namesChangedNotice;
    if (!notice) return nothing;
    return html`<lcm-notice
      tone="info"
      data-field="names-notice"
      heading=${t('settings.acme.namesNotice.title')}
      dismissible
      close-label=${t('settings.acme.namesNotice.dismiss')}
      @lcm-dismiss=${() => void this.#dismissNotice()}
    >
      ${t('settings.acme.namesNotice.text', { old: notice.old.join(', '), new: notice.new.length > 0 ? notice.new.join(', ') : t('settings.acme.namesNotice.none') })}
      ${notice.wasPerName ? t('settings.acme.namesNotice.perName') : nothing}
    </lcm-notice>`;
  }

  #challenge(draft: AcmeDraft, state: AcmeState): TemplateResult {
    const dns = draft.challenge === 'dns-01';
    return html`<div class="block">
      <lcm-choice
        label=${t('settings.acme.challenge')}
        variant="cards"
        stacked
        .options=${this.#challenges}
        .value=${draft.challenge}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ challenge: e.detail.value === 'dns-01' ? 'dns-01' : 'http-01' })}
      ></lcm-choice>
      ${dns
        ? html`<lcm-acme-dns
            .draft=${draft}
            .saved=${state}
            .names=${this.names}
            .errors=${this._fieldErrors}
            @lcm-dns-change=${(e: CustomEvent<DnsDraft>) => this.#set({ dns: e.detail })}
          ></lcm-acme-dns>`
        : this.#listener(state)}
    </div>`;
  }

  #terms(draft: AcmeDraft, state: AcmeState): TemplateResult | typeof nothing {
    if (!needsTerms(draft, state)) return nothing;
    const host = directoryHost(directoryOf(draft));
    const link = termsUrl(draft);
    return html`<div class="terms">
      <lcm-switch
        data-field="terms"
        label=${t('settings.acme.terms', { host })}
        description=${t('settings.acme.termsHint')}
        on-label=${t('settings.form.on')}
        off-label=${t('settings.form.off')}
        .checked=${draft.acceptTerms}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ acceptTerms: e.detail.checked })}
      ></lcm-switch>
      ${link ? html`<a data-field="terms-link" href=${link} target="_blank" rel="noopener noreferrer">${t('settings.acme.termsLink', { host })}</a>` : nothing}
    </div>`;
  }

  /** Die Namen des Zertifikats, nur zum Lesen: Sie stammen aus den Adressen oben. */
  #nameList(): TemplateResult {
    if (this.names.length === 0) return html`<lcm-notice tone="warn" data-field="names">${t('settings.acme.namesNone')}</lcm-notice>`;
    return html`<div data-field="names">
      <p>${t('settings.acme.names')}: <span class="mono">${this.names.join(', ')}</span></p>
      <p class="muted">${t('settings.acme.namesHint')}</p>
      ${this.namesPending ? html`<lcm-notice tone="info">${t('settings.acme.namesPending')}</lcm-notice>` : nothing}
    </div>`;
  }

  #fields(draft: AcmeDraft, state: AcmeState): TemplateResult {
    const off = !draft.enabled;
    return html`<div class="fields">
      ${this.wizard
        ? nothing
        : html`<lcm-switch
        data-field="enabled"
        label=${t('settings.acme.enable')}
        on-text=${t('settings.acme.enableOn')}
        off-text=${t('settings.acme.enableOff')}
        on-label=${t('settings.form.on')}
        off-label=${t('settings.form.off')}
        .checked=${draft.enabled}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ enabled: e.detail.checked })}
      ></lcm-switch>`}
      ${this.#namesNotice(state)} ${this.#nameList()}
      <lcm-input
        data-field="email"
        label=${t('settings.acme.email')}
        width="l"
        type="email"
        maxlength=${LIMITS.sender}
        placeholder=${'admin@example.com'}
        hint=${t('settings.acme.emailHint')}
        ?disabled=${off}
        .value=${draft.email}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ email: e.detail.value })}
      ></lcm-input>
      <lcm-select
        data-field="ca"
        label=${t('settings.acme.ca')}
        width="m"
        .options=${this.#cas}
        .value=${draft.ca}
        ?disabled=${off}
        hint=${draft.ca === 'staging' ? t('settings.acme.caStagingHint') : ''}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ ca: e.detail.value as AcmeCa })}
      ></lcm-select>
      ${draft.ca === 'custom'
        ? html`<lcm-input
            data-field="directory"
            label=${t('settings.acme.directory')}
            width="l"
            mono
            maxlength=${LIMITS.path}
            placeholder=${'https://ca.example.com/acme/directory'}
            hint=${t('settings.acme.directoryHint')}
            ?disabled=${off}
            .value=${draft.customUrl}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ customUrl: e.detail.value })}
          ></lcm-input>`
        : nothing}
      ${this.#terms(draft, state)}
    </div>`;
  }

  #certificate(cert: AcmeCertificate): TemplateResult {
    return html`<div class="cert">
      <dl class="facts">
        <dt>${t('settings.endpoint.cert.names')}</dt>
        <dd>${cert.names.join(', ')}</dd>
        <dt>${t('settings.endpoint.cert.issuer')}</dt>
        <dd>${cert.issuer}</dd>
        <dt>${t('settings.endpoint.cert.validUntil')}</dt>
        <dd>${formatDate(cert.notAfter)}</dd>
        <dt>${t('settings.acme.renewAt')}</dt>
        <dd>${formatDate(cert.renewAt)}</dd>
      </dl>
      <div class="row">
        <lcm-button size="small" icon="download" data-field="download" ?disabled=${this._busy} title=${t('settings.acme.downloadHint')} @click=${() => void this.#download(cert)}>
          ${t('settings.acme.download')}
        </lcm-button>
        <span class="muted">${t('settings.acme.downloadHint')}</span>
      </div>
    </div>`;
  }

  async #download(cert: AcmeCertificate): Promise<void> {
    try {
      saveBlob(chainFileName(cert.names), await endpointsApi.downloadCertificate(`acme-${cert.id}`));
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    }
  }

  #runState(state: AcmeState): TemplateResult {
    return html`<div class="block">
      <h3>${t('settings.acme.state')}</h3>
      <dl class="facts">
        <dt>${t('settings.acme.lastRun')}</dt>
        <dd>${state.lastRunAt ? html`${formatDateTime(state.lastRunAt)} (${formatRelative(state.lastRunAt)})` : t('settings.acme.never')}</dd>
      </dl>
      ${state.lastError ? html`<lcm-notice tone="crit" heading=${t('settings.acme.lastError')}>${state.lastError}</lcm-notice>` : nothing}
      ${state.certificates.length > 0 ? state.certificates.map((cert) => this.#certificate(cert)) : html`<p class="muted">${t('settings.acme.noCertificates')}</p>`}
    </div>`;
  }

  #problemText(problem: AcmeProblem, draft: AcmeDraft, state: AcmeState): string {
    const missing = missingDnsField(draft.dns, state);
    const field = missing === null ? '' : t(`${fieldKey(draft.dns.provider, missing)}.label`);
    return t(`settings.acme.problem.${problem}`, { field });
  }

  /** Im Assistenten: Läuft die Ausstellung oder ist sie geglückt, sagt es diese Zeile; Fehler stehen im Stand darunter. */
  #progress(state: AcmeState): TemplateResult | typeof nothing {
    if (state.running) return html`<lcm-status data-field="acme-progress" level="running" label=${t('settings.acme.wizard.running')}></lcm-status>`;
    if (!coversNames(state, this.names)) return nothing;
    const cert = state.certificates.find((candidate) => coversNames({ certificates: [candidate] }, this.names));
    return html`<lcm-notice tone="ok" data-field="acme-done">${t('settings.acme.wizard.done', { names: this.names.join(', '), date: cert ? formatDate(cert.notAfter) : '' })}</lcm-notice>`;
  }

  /** Der Fuß im Assistenten: „Zertifikat anfordern“, solange kein gültiges Zertifikat alle Namen trägt (ändern sich die Namen, wieder). */
  #wizardFooter(draft: AcmeDraft, state: AcmeState): TemplateResult {
    const problem = problemOf(draft, state, this.names);
    // Mit dem Zertifikat ist nichts mehr anzufordern; erst andere Namen machen den Knopf wieder frei.
    const covered = coversNames(state, this.names);
    const running = state.running || this._busy;
    return html`<div slot="footer" class="row">
      ${this.#progress(state)}
      ${covered
        ? nothing
        : html`<lcm-button variant="primary" icon="lock" data-action="request" ?busy=${running} ?disabled=${!!problem || running} @click=${() => void this.#request()}>${t('settings.acme.wizard.request')}</lcm-button>`}
      ${problem && !covered ? html`<span class="muted">${this.#problemText(problem, draft, state)}</span>` : nothing}
    </div>`;
  }

  #footer(draft: AcmeDraft, state: AcmeState): TemplateResult {
    if (this.wizard) return this.#wizardFooter(draft, state);
    const dirty = isDirty(draft, state);
    const problem = problemOf(draft, state, this.names);
    return html`<div slot="footer" class="row">
      <lcm-button variant="primary" ?busy=${this._busy} ?disabled=${!dirty || !!problem} @click=${() => void this.#save()}>${t('settings.acme.save')}</lcm-button>
      <lcm-button variant="ghost" ?disabled=${!dirty || this._busy} @click=${() => this.#discard()}>${t('settings.acme.discard')}</lcm-button>
      ${dirty && problem ? html`<span class="muted">${this.#problemText(problem, draft, state)}</span>` : nothing}
    </div>`;
  }

  override render() {
    const state = this.#source;
    const draft = this._draft;
    if (!state || !draft) return nothing;
    const status = statusOf(state);
    const canIssue = state.enabled && !state.running && !isDirty(draft, state);
    return html`<lcm-card level="3" variant="inset" collapsible ?open=${this.open} heading=${t('settings.acme.title')} subtitle=${t('settings.acme.subtitle')}>
      <div slot="actions" class="row">
        ${this.wizard && !state.enabled ? nothing : html`<lcm-status level=${status.level} label=${t(status.labelKey)}></lcm-status>`}
        ${this.wizard ? nothing : html`<lcm-button icon="refresh" ?busy=${state.running || this._busy} ?disabled=${!canIssue} @click=${() => void this.#issue()}>${t('settings.acme.issue')}</lcm-button>`}
      </div>
      <div class="fields">
        ${this.#fields(draft, state)} ${this.#challenge(draft, state)} ${this.#runState(state)}
        ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      </div>
      ${this.#footer(draft, state)}
    </lcm-card>`;
  }
}
customElements.define('lcm-settings-acme', LcmSettingsAcme);
