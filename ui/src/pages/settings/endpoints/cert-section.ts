import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../../components/lcm-badge';
import '../../../components/lcm-button';
import '../../../components/lcm-dialog';
import '../../../components/lcm-notice';
import type { Impact } from '../../../components/lcm-dialog';
import { baseStyles, emit } from '../../../components/shared';
import { endpointsApi, type Certificate, type EndpointName, type TlsStatus } from '../../../api/endpoints';
import { copyWithToast } from '../../../lib/clipboard';
import { I18nController } from '../../../lib/controllers';
import { formatDate } from '../../../lib/format';
import { errorText, t } from '../../../lib/i18n';
import { settingsStyles } from '../system/settings-styles';
import { certificateOf, expiryOf, fingerprintText, otherCertificates, servedBy, SOURCE_KEY, uploadOutcome, warningKey, type UploadOutcome } from './cert-model';

/**
 * <lcm-cert-section .status=${…} endpoint="ui"> – das Zertifikat, das LCM für den Namen der Adresse dieses Dienstes
 * ausliefert, mit Hochladen und Entfernen. Der Stand kommt von außen; nach jeder Änderung meldet die Sektion
 * `lcm-tls-changed` { status } mit dem neuen. `unavailable` heißt: LCM läuft ohne TLS (--dev).
 */
export class LcmCertSection extends LitElement {
  static override properties = {
    status: { attribute: false },
    endpoint: { type: String },
    unavailable: { type: Boolean },
    _form: { state: true },
    _certFile: { state: true },
    _keyFile: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _outcome: { state: true },
    _removed: { state: true },
    _remove: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      section {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      h3 {
        margin: 0;
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
      }
      h4 {
        margin: 0;
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
      }
      p {
        margin: 0;
      }
      .cert {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding: var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        box-shadow: inset 0 0 0 1px var(--line);
      }
      .file {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
      }
      .file > span {
        min-inline-size: 0;
      }
      input[type='file'] {
        display: none;
      }
      .upload {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .label {
        display: block;
        margin-block-end: var(--space-1);
        font-size: var(--fs-s);
        color: var(--text-2);
      }
      ul.others {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      ul.others li {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
      }
      ul.others .names {
        flex: 1 1 14rem;
        min-inline-size: 0;
        overflow-wrap: anywhere;
      }
    `,
  ];

  declare status: TlsStatus | undefined;
  declare endpoint: EndpointName;
  declare unavailable: boolean;
  declare _form: boolean;
  declare _certFile: File | undefined;
  declare _keyFile: File | undefined;
  declare _busy: boolean;
  declare _error: string;
  declare _outcome: UploadOutcome | undefined;
  /** Das Entfernen ist gelungen; die Meldung steht hier, weil ein Hinweis außerhalb des Fensters bis zu dessen Schließen wartet. */
  declare _removed: boolean;
  /** Das Zertifikat, dessen Entfernen gerade bestätigt wird. */
  declare _remove: Certificate | undefined;

  constructor() {
    super();
    this.endpoint = 'ui';
    this.unavailable = false;
    this._form = false;
    this._busy = false;
    this._removed = false;
    this._error = '';
    new I18nController(this);
  }

  #changed(status: TlsStatus): void {
    emit(this, 'lcm-tls-changed', { status });
  }

  #pick(kind: 'cert' | 'key', event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    if (kind === 'cert') this._certFile = file;
    else this._keyFile = file;
  }

  #closeForm(): void {
    this._form = false;
    this._certFile = this._keyFile = undefined;
    this._error = '';
  }

  async #upload(): Promise<void> {
    if (!this._certFile || !this._keyFile || this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      const result = await endpointsApi.uploadCertificate(this._certFile, this._keyFile);
      this._outcome = uploadOutcome(result.status, result.id, this.endpoint);
      this._removed = false;
      this.#closeForm();
      this.#changed(result.status);
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  async #confirmRemove(): Promise<void> {
    const cert = this._remove;
    if (!cert) return;
    this._busy = true;
    this._error = '';
    try {
      const status = await endpointsApi.removeCertificate(cert.id);
      this._remove = undefined;
      this._outcome = undefined;
      this._removed = true;
      this.#changed(status);
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #copyFingerprint(cert: Certificate): Promise<void> {
    return copyWithToast(fingerprintText(cert.fingerprint, false), {
      copied: t('settings.endpoint.cert.copied'),
      failed: t('settings.endpoint.cert.copyFailed'),
    });
  }

  #validUntil(cert: Certificate): TemplateResult {
    const expiry = expiryOf(cert);
    const date = formatDate(cert.notAfter);
    if (expiry.state === 'expired') return html`<dd class="crit">${t('settings.endpoint.cert.expiredOn', { date })}</dd>`;
    const left = expiry.days === 0 ? t('settings.endpoint.cert.expiresToday') : t('settings.endpoint.cert.expiresIn', { count: expiry.days });
    return html`<dd class=${expiry.state === 'soon' ? 'warn' : ''}>${date}${expiry.state === 'soon' ? html` (${left})` : nothing}</dd>`;
  }

  #warnings(cert: Certificate): TemplateResult[] {
    return cert.warnings.map((warning) => {
      const key = warningKey(warning);
      return html`<lcm-notice tone="warn">${key ? t(key) : warning}</lcm-notice>`;
    });
  }

  #shared(cert: Certificate): string {
    const others = servedBy(this.status!, cert.id).filter((name) => name !== this.endpoint);
    return others.map((name) => t(`settings.endpoint.name.${name}`)).join(', ');
  }

  #facts(cert: Certificate): TemplateResult {
    const also = this.#shared(cert);
    return html`<dl class="facts">
      <dt>${t('settings.endpoint.cert.sourceLabel')}</dt>
      <dd><lcm-badge tone=${cert.source === 'lcm-ca' ? '' : 'accent'}>${t(SOURCE_KEY[cert.source])}</lcm-badge></dd>
      <dt>${t('settings.endpoint.cert.names')}</dt>
      <dd>${cert.names.join(', ')}</dd>
      <dt>${t('settings.endpoint.cert.issuer')}</dt>
      <dd>${cert.issuer}</dd>
      <dt>${t('settings.endpoint.cert.validUntil')}</dt>
      ${this.#validUntil(cert)}
      <dt>${t('settings.endpoint.cert.fingerprint')}</dt>
      <dd>
        <span class="row">
          <span class="mono" title=${fingerprintText(cert.fingerprint, false)}>${fingerprintText(cert.fingerprint, true)}</span>
          <lcm-button variant="ghost" size="small" icon="copy" label=${t('settings.endpoint.cert.copyLabel')} @click=${() => void this.#copyFingerprint(cert)}>
            ${t('settings.endpoint.cert.copy')}
          </lcm-button>
        </span>
      </dd>
      ${also ? html`<dt>${t('settings.endpoint.cert.alsoServes')}</dt><dd>${also}</dd>` : nothing}
    </dl>`;
  }

  #sourceNote(cert: Certificate, host: string): TemplateResult | typeof nothing {
    switch (cert.source) {
      case 'config':
        return html`<lcm-notice tone="info">${t('settings.endpoint.cert.fixedByConfig')}</lcm-notice>`;
      case 'acme':
        return html`<lcm-notice tone="info">${t('settings.endpoint.cert.fixedByAcme')}</lcm-notice>`;
      case 'lcm-ca':
        return html`<lcm-notice tone="warn">${t('settings.endpoint.cert.noOwn', { host })}</lcm-notice>`;
      default:
        return nothing;
    }
  }

  #served(cert: Certificate, host: string): TemplateResult {
    const expiry = expiryOf(cert);
    return html`<div class="cert">
      <p class="muted">${t('settings.endpoint.cert.servedFor', { host })}</p>
      ${this.#facts(cert)}
      ${expiry.state === 'soon' && cert.source !== 'lcm-ca' ? html`<lcm-notice tone="warn">${t('settings.endpoint.cert.soonNote')}</lcm-notice>` : nothing}
      ${this.#warnings(cert)} ${this.#sourceNote(cert, host)}
    </div>`;
  }

  #filePicker(kind: 'cert' | 'key'): TemplateResult {
    const file = kind === 'cert' ? this._certFile : this._keyFile;
    const label = t(kind === 'cert' ? 'settings.endpoint.upload.certFile' : 'settings.endpoint.upload.keyFile');
    return html`<div>
      <span class="label">${label}</span>
      <div class="file">
        <lcm-button icon="upload" @click=${(e: Event) => ((e.currentTarget as HTMLElement).parentElement!.querySelector('input') as HTMLInputElement).click()}>
          ${t('settings.endpoint.upload.choose')}
        </lcm-button>
        <input type="file" data-field=${kind === 'cert' ? 'certificate' : 'key'} aria-label=${label} @change=${(e: Event) => this.#pick(kind, e)} />
        <span class="mono">${file ? file.name : t('settings.endpoint.upload.none')}</span>
      </div>
    </div>`;
  }

  #uploadForm(): TemplateResult {
    const agentHost = certificateOf(this.status!, 'agent')?.host ?? '';
    return html`<div class="upload">
      <h4>${t('settings.endpoint.cert.upload')}</h4>
      <p class="muted">${t('settings.endpoint.upload.hint')}</p>
      ${agentHost ? html`<lcm-notice tone="info">${t('settings.endpoint.upload.agentName', { host: agentHost })}</lcm-notice>` : nothing}
      ${this.#filePicker('cert')} ${this.#filePicker('key')}
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="row">
        <lcm-button variant="primary" icon="upload" ?busy=${this._busy} ?disabled=${!this._certFile || !this._keyFile} @click=${() => void this.#upload()}>
          ${t('settings.endpoint.upload.submit')}
        </lcm-button>
        <lcm-button variant="ghost" @click=${() => this.#closeForm()}>${t('settings.endpoint.upload.cancel')}</lcm-button>
      </div>
    </div>`;
  }

  #outcomeNotes(host: string): TemplateResult | typeof nothing {
    const outcome = this._outcome;
    if (!outcome?.certificate) return nothing;
    const own = certificateOf(this.status!, this.endpoint)?.cert;
    const names = outcome.certificate.names.join(', ');
    const heading = t('settings.endpoint.upload.done');
    let result: TemplateResult;
    if (outcome.served) result = html`<lcm-notice tone="ok" heading=${heading}>${t('settings.endpoint.upload.served', { host })}</lcm-notice>`;
    else if (outcome.covers) {
      result = html`<lcm-notice tone="warn" heading=${heading}>${t('settings.endpoint.upload.precedence', { host, source: own ? t(SOURCE_KEY[own.source]) : '' })}</lcm-notice>`;
    } else result = html`<lcm-notice tone="warn" heading=${heading}>${t('settings.endpoint.upload.notServed', { host, names })}</lcm-notice>`;
    return html`${result}
    ${outcome.agentKeepsLcmCertificate ? html`<lcm-notice tone="info">${t('settings.endpoint.upload.agentKeeps', { host: outcome.agentKeepsLcmCertificate })}</lcm-notice>` : nothing}`;
  }

  #others(): TemplateResult | typeof nothing {
    const shown = certificateOf(this.status!, this.endpoint)?.cert;
    const others = otherCertificates(this.status!, shown?.id);
    if (others.length === 0) return nothing;
    return html`<div class="form-stack">
      <h4>${t('settings.endpoint.cert.others')}</h4>
      <ul class="others">
        ${others.map(
          (cert) => html`<li>
            <span class="names mono">${cert.names.join(', ')}</span>
            <lcm-badge>${t(SOURCE_KEY[cert.source])}</lcm-badge>
            <span class="muted">${formatDate(cert.notAfter)}</span>
            ${cert.source === 'upload'
              ? html`<lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('settings.endpoint.remove.label', { names: cert.names.join(', ') })} @click=${() => (this._remove = cert)}></lcm-button>`
              : nothing}
          </li>`,
        )}
      </ul>
    </div>`;
  }

  #removeDialog(): TemplateResult {
    const cert = this._remove;
    const impacts: Impact[] = cert
      ? [
          { label: t('settings.endpoint.cert.names'), value: cert.names.join(', ') },
          { label: t('settings.endpoint.cert.validUntil'), value: formatDate(cert.notAfter) },
          { label: t('settings.endpoint.remove.undo'), value: t('settings.endpoint.remove.undoNo'), tone: 'warn' },
        ]
      : [];
    const also = cert ? servedBy(this.status!, cert.id).map((name) => t(`settings.endpoint.name.${name}`)) : [];
    if (also.length > 0) impacts.push({ label: t('settings.endpoint.remove.serves'), value: also.join(', ') });
    return html`<lcm-dialog
      ?open=${!!cert}
      tone="danger"
      heading=${t('settings.endpoint.remove.heading')}
      message=${t('settings.endpoint.remove.message')}
      confirm-label=${t('settings.endpoint.remove.confirm')}
      cancel-label=${t('settings.endpoint.remove.keep')}
      .impacts=${impacts}
      ?busy=${this._busy}
      error=${this._remove ? this._error : ''}
      @lcm-confirm=${() => void this.#confirmRemove()}
      @lcm-cancel=${() => ((this._remove = undefined), (this._error = ''))}
    ></lcm-dialog>`;
  }

  #actions(own: Certificate | null | undefined): TemplateResult {
    return html`<div class="row">
      <lcm-button icon="upload" ?disabled=${this._form} @click=${() => ((this._form = true), (this._outcome = undefined), (this._removed = false))}>${t('settings.endpoint.cert.upload')}</lcm-button>
      ${own?.source === 'upload'
        ? html`<lcm-button variant="danger" icon="trash" @click=${() => (this._remove = own)}>${t('settings.endpoint.cert.remove')}</lcm-button>`
        : nothing}
    </div>`;
  }

  override render() {
    const heading = html`<h3>${t('settings.endpoint.cert.title')}</h3>`;
    if (this.unavailable) return html`<section>${heading}<lcm-notice tone="info">${t('settings.endpoint.cert.unavailable')}</lcm-notice></section>`;
    const status = this.status;
    if (!status) return nothing;
    const own = certificateOf(status, this.endpoint);
    if (!own?.host) {
      return html`<section>${heading}<lcm-notice tone="info">${t('settings.endpoint.cert.noAddress')}</lcm-notice></section>`;
    }
    const cert = own.cert;
    return html`<section>
      ${heading} ${cert ? this.#served(cert, own.host) : nothing} ${this.#actions(cert)}
      ${this._form ? this.#uploadForm() : nothing} ${this.#outcomeNotes(own.host)}
      ${this._removed ? html`<lcm-notice tone="ok">${t('settings.endpoint.remove.done')}</lcm-notice>` : nothing} ${this.#others()}
      ${this.#removeDialog()}
    </section>`;
  }
}
customElements.define('lcm-cert-section', LcmCertSection);
