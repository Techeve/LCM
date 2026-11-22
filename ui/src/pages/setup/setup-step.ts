import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-notice';
import '../../components/lcm-explainer';
import { baseStyles, emit } from '../../components/shared';
import { settingsApi, type GlobalSettings } from '../../api/settings';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { isSkippable, previousStep, type WizardStep } from '../../lib/setup-steps';
import { unsaved } from '../../lib/unsaved';
import { errorParams, type Draft, type FieldErrors, type FieldSpec } from '../settings/system/form-state';
import { formStyles } from '../account/form-styles';
import { completeStep, goBack, goToStep } from './setup-model';

export interface StepDone {
  settings: GlobalSettings;
}

/** Alle Schritte teilen sich eine Hilfe-Spalte: Wer sie einklappt, hat sie in jedem Schritt eingeklappt. */
const HELP_TOPIC = 'setup-wizard';

/**
 * Grundlage der Schritte des Einrichtungsassistenten: Fuß mit „Zurück“ (außer im ersten Schritt), „Speichern und weiter“
 * (und „Überspringen“, wo erlaubt), Fehleranzeige, optional die Hilfe-Spalte rechts daneben (`help()`) und die Meldung
 * `lcm-step-done` { settings }, mit der die Seite zum nächsten oder vorigen Schritt geht. Ein Schritt liefert
 * `body()` und in `save()` seine Prüfung und Speicherung; `advance(daten)` schließt ihn mit seinen Daten ab. Beschriftung
 * und Sperre des Hauptknopfs kann er über `saveLabel()` und `saveDisabled()` ändern (Zertifikat).
 * Schritte mit Eingaben, die sich selbst speichern (Netzbereiche, Let's Encrypt), setzen `warnUnsaved`: Ist beim
 * Weitergehen noch eine Eingabe offen, warnt der Schritt einmal, und erst der zweite Klick geht ohne sie weiter.
 */
export abstract class SetupStep extends LitElement {
  static override properties = {
    settings: { attribute: false },
    _busy: { state: true },
    _error: { state: true },
    _warned: { state: true },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      /* Wie auf den Einstellungsseiten: Hilfe rechts, darunter auf schmalen Bildschirmen (dort klappt sie nach oben ein). */
      .layout {
        display: grid;
        grid-template-columns: minmax(0, var(--content-w, 48rem));
        gap: var(--gap);
        align-items: start;
      }
      .layout.explain {
        grid-template-columns: minmax(0, var(--content-w, 48rem)) auto;
      }
      @media (max-width: 75rem) {
        .layout.explain {
          grid-template-columns: minmax(0, var(--content-w, 48rem));
        }
      }
      lcm-explainer p {
        margin: 0 0 var(--space-3);
      }
      .step {
        display: grid;
        gap: var(--space-5);
        min-inline-size: 0;
      }
      .skip-hint {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  abstract readonly step: WizardStep;
  /** Die gespeicherten globalen Einstellungen; die Seite lädt sie. */
  declare settings: GlobalSettings;
  declare _busy: boolean;
  declare _error: string;
  declare _warned: boolean;

  /** Meldet der Schritt Eingaben, die sich selbst speichern, und warnt vor dem Weitergehen mit offenen Änderungen? */
  protected readonly warnUnsaved: boolean = false;
  #stopWatching: (() => void) | undefined;

  constructor() {
    super();
    this.settings = {};
    this._busy = false;
    this._error = '';
    this._warned = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (this.warnUnsaved) this.#stopWatching = unsaved.subscribe(() => this.#unsavedChanged());
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopWatching?.();
  }

  protected abstract body(): TemplateResult;

  /** Die Hilfe zum Schritt für die Spalte rechts; ohne sie steht der Schritt allein. */
  protected help(): TemplateResult | typeof nothing {
    return nothing;
  }

  /** Beschriftung des Hauptknopfs. */
  protected saveLabel(): string {
    return t('setup.save');
  }

  /** Ist der Hauptknopf gesperrt, weil der Schritt noch nicht weiter darf? */
  protected saveDisabled(): boolean {
    return false;
  }

  /** Prüft die Eingaben und schließt den Schritt ab; ein Fehler (geworfen oder in `_error`) hält ihn auf. */
  protected abstract save(): Promise<void>;

  /** Die Fehlertexte aus Übersetzungsschlüsseln, mit Grenzen und ungültigen Einträgen des Felds. */
  protected errorTexts(draft: Draft, errors: FieldErrors, fields: FieldSpec[]): FieldErrors {
    const entries = Object.entries(errors).map(([key, message]) => [key, t(message, errorParams(fields.find((field) => field.key === key), String(draft[key] ?? '')))]);
    return Object.fromEntries(entries);
  }

  /** Speichert die Daten des Schritts und rückt zum nächsten vor, ohne die Seite zu benachrichtigen. */
  protected patch(data: Record<string, unknown> = {}): Promise<GlobalSettings> {
    return completeStep(settingsApi, this.step, data);
  }

  /** Meldet der Seite, dass der Schritt erledigt ist, mit den Einstellungen, wie das Backend sie nach dem Speichern meldete. */
  protected done(settings: GlobalSettings): void {
    emit<StepDone>(this, 'lcm-step-done', { settings });
  }

  protected async advance(data: Record<string, unknown> = {}): Promise<void> {
    this.done(await this.patch(data));
  }

  /** Ist alles gespeichert oder verworfen, gilt die nächste offene Eingabe wieder als neu: Die Warnung kommt erneut. */
  #unsavedChanged(): void {
    if (!unsaved.has()) this._warned = false;
    this.requestUpdate();
  }

  get #unsavedPending(): boolean {
    return this.warnUnsaved && this._warned && unsaved.has();
  }

  async #run(work: () => Promise<void>): Promise<void> {
    if (this._busy) return;
    this._busy = true;
    this._error = '';
    try {
      await work();
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  /** Ein Schritt zurück; was die Schritte davor gespeichert haben, bleibt. */
  async #onBack(): Promise<void> {
    await this.#run(async () => {
      const going = goBack(settingsApi, this.step);
      if (going) this.done(await going);
    });
  }

  /** Zu einem früheren Schritt, wie „Zurück“; Gespeichertes bleibt. */
  protected async goTo(target: WizardStep): Promise<void> {
    await this.#run(async () => this.done(await goToStep(settingsApi, target)));
  }

  async #onSave(): Promise<void> {
    if (this.warnUnsaved && unsaved.has() && !this._warned) {
      this._warned = true;
      return;
    }
    await this.#run(() => this.save());
  }

  #actions(): TemplateResult {
    return html`<div class="actions">
      ${previousStep(this.step)
        ? html`<lcm-button variant="outline" icon="chevron-left" data-action="back" ?disabled=${this._busy} @click=${() => void this.#onBack()}>${t('setup.back')}</lcm-button>`
        : nothing}
      <lcm-button variant="primary" icon="check" data-action="save" ?busy=${this._busy} ?disabled=${this.saveDisabled()} @click=${() => void this.#onSave()}>
        ${this.#unsavedPending ? t('setup.continueAnyway') : this.saveLabel()}
      </lcm-button>
      ${isSkippable(this.step)
        ? html`<lcm-button variant="ghost" data-action="skip" ?disabled=${this._busy} @click=${() => void this.#run(() => this.advance())}>${t('setup.skip')}</lcm-button>
            <span class="skip-hint">${t('setup.skipHint')}</span>`
        : nothing}
    </div>`;
  }

  #helpColumn(): TemplateResult | typeof nothing {
    const help = this.help();
    if (help === nothing) return nothing;
    return html`<lcm-explainer
      data-field="help"
      topic=${HELP_TOPIC}
      heading=${t('settings.general.address.help.title')}
      collapse-label=${t('settings.form.collapse')}
      expand-label=${t('settings.form.expand')}
    >
      ${help}
    </lcm-explainer>`;
  }

  override render(): TemplateResult {
    const help = this.#helpColumn();
    return html`<div class=${help === nothing ? 'layout' : 'layout explain'}>
      <div class="step" data-step=${this.step}>
        ${this.body()}
        ${this.#unsavedPending ? html`<lcm-notice tone="warn" data-field="unsaved">${t('setup.unsaved')}</lcm-notice>` : nothing}
        ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
        ${this.#actions()}
      </div>
      ${help}
    </div>`;
  }
}
