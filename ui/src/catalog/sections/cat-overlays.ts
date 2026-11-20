import { LitElement, css, html } from 'lit';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-dialog.js';
import '../../components/lcm-modal.js';
import '../../components/lcm-input.js';
import '../../components/lcm-select.js';
import '../../components/lcm-switch.js';
import '../../components/lcm-toaster.js';
import type { ToastOptions } from '../../components/lcm-toaster.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { followChange } from '../follow-change.js';

function toast(detail: ToastOptions): void {
  window.dispatchEvent(new CustomEvent('lcm-toast', { detail }));
}

export class CatOverlays extends LitElement {
  static override properties = { modal: { state: true }, confirm: { state: true }, destroy: { state: true }, busy: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .form {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
    `,
  ];
  declare modal: boolean;
  declare confirm: boolean;
  declare destroy: boolean;
  declare busy: boolean;

  constructor() {
    super();
    this.modal = false;
    this.confirm = false;
    this.destroy = false;
    this.busy = false;
  }

  private runUpdate(): void {
    this.busy = true;
    window.setTimeout(() => {
      this.busy = false;
      this.confirm = false;
      toast({ tone: 'ok', title: 'Updates werden eingespielt', message: 'Job für web03 gestartet. Den Fortschritt siehst du unter „Jobs“.', actionLabel: 'Zu den Jobs' });
    }, 700);
  }

  override render() {
    return html`<section>
      <h2>Fenster, Dialoge, Toasts</h2>
      <p class="lead">Die Fenster lässt den Kontext sichtbar. Bestätigungen sagen, was passiert, statt „Sind Sie sicher?“ zu fragen. Zerstörende Aktionen verlangen den getippten Servernamen. Alles ist per Tastatur bedienbar: Fokus bleibt im Dialog, Esc schließt, der Fokus kehrt zurück.</p>
      <div class="row">
        <lcm-button id="open-modal" icon="edit" @click=${() => (this.modal = true)}>Server bearbeiten (Fenster)</lcm-button>
        <lcm-button id="open-confirm" variant="primary" icon="bolt" @click=${() => (this.confirm = true)}>Updates einspielen (Bestätigung)</lcm-button>
        <lcm-button id="open-destroy" variant="danger" icon="trash" @click=${() => (this.destroy = true)}>Server entfernen (zerstörend)</lcm-button>
      </div>
      <h3 class="sub">Toasts</h3>
      <div class="row">
        <lcm-button @click=${() => toast({ tone: 'ok', title: 'Gespeichert', message: 'Die Einstellungen von web03 sind übernommen.' })}>Erfolg</lcm-button>
        <lcm-button @click=${() => toast({ tone: 'info', title: 'Neue Version verfügbar', message: 'LCM lädt neu, sobald nichts Ungespeichertes offen ist.' })}>Info</lcm-button>
        <lcm-button @click=${() => toast({ tone: 'warn', title: 'web03 ist belegt', message: 'Updates werden eingespielt. Die Aktion wartet.' })}>Warnung</lcm-button>
        <lcm-button @click=${() => toast({ tone: 'crit', title: 'Verbindung verloren', message: 'backup01 antwortet seit 3 Stunden nicht.', actionLabel: 'Erneut versuchen' })}>Fehler (bleibt stehen)</lcm-button>
      </div>

      <lcm-modal id="modal" ?open=${this.modal} heading="Server bearbeiten" subtitle="web03 · Produktion" close-label="Schließen" @lcm-close=${() => (this.modal = false)}>
        <div class="form">
          <lcm-input label="Name" value="web03"></lcm-input>
          <lcm-input label="Hostname oder IP" mono value="web03.prod.techeve.de"></lcm-input>
          <lcm-select label="Gruppe" value="prod" .options=${[{ value: 'prod', label: 'Produktion' }, { value: 'db', label: 'Datenbanken' }]} @lcm-change=${followChange}></lcm-select>
          <lcm-switch label="Neustart bei Bedarf erlauben" description="Nur im Wartungsfenster." checked @lcm-change=${followChange}></lcm-switch>
          <lcm-switch label="In Berichten ausblenden" @lcm-change=${followChange}></lcm-switch>
        </div>
        <lcm-button slot="footer" variant="ghost" @click=${() => (this.modal = false)}>Abbrechen</lcm-button>
        <lcm-button slot="footer" variant="primary" @click=${() => ((this.modal = false), toast({ tone: 'ok', title: 'Gespeichert' }))}>Speichern</lcm-button>
      </lcm-modal>

      <lcm-dialog
        id="confirm"
        ?open=${this.confirm}
        ?busy=${this.busy}
        heading="Updates auf web03 einspielen?"
        message="27 Pakete werden aktualisiert, darunter ein Kernel und openssl."
        .impacts=${[
          { label: 'Betroffener Server', value: 'web03 (Produktion)' },
          { label: 'Dienste', value: 'nginx und php-fpm starten neu', tone: 'warn' },
          { label: 'Ausfallzeit', value: 'etwa 5 Sekunden', tone: 'warn' },
          { label: 'Neustart nötig', value: 'Ja, wegen des neuen Kernels', tone: 'warn' },
          { label: 'Umkehrbar', value: 'Nein, aber Snapshot vorhanden', tone: 'ok' },
        ]}
        confirm-label="Updates einspielen"
        cancel-label="Abbrechen"
        @lcm-cancel=${() => (this.confirm = false)}
        @lcm-confirm=${this.runUpdate}
      ></lcm-dialog>

      <lcm-dialog
        id="destroy"
        ?open=${this.destroy}
        tone="danger"
        heading="web03 aus LCM entfernen?"
        message="Der Agent und die Zugangsschlüssel werden vom Server gelöscht. Der Server selbst läuft weiter, wird aber nicht mehr verwaltet."
        .impacts=${[
          { label: 'Betroffene Server', value: '1' },
          { label: 'Verlauf', value: '212 Jobs und 14 Monate Statistik gehen verloren', tone: 'crit' },
          { label: 'Umkehrbar', value: 'Nein', tone: 'crit' },
        ]}
        expect="web03"
        expect-label="Tippe den Servernamen web03 zur Bestätigung"
        confirm-label="Server entfernen"
        cancel-label="Abbrechen"
        @lcm-cancel=${() => (this.destroy = false)}
        @lcm-confirm=${() => {
          this.destroy = false;
          toast({ tone: 'ok', title: 'web03 wurde entfernt' });
        }}
      ></lcm-dialog>
    </section>`;
  }
}
customElements.define('cat-overlays', CatOverlays);
