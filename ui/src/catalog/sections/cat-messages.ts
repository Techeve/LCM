import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button.js';
import '../../components/lcm-input.js';
import '../../components/lcm-modal.js';
import '../../components/lcm-notice.js';
import '../../components/lcm-save-bar.js';
import type { SaveBarField, SaveBarLabels } from '../../components/lcm-save-bar.js';
import type { ToastOptions } from '../../components/lcm-toaster.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';

function toast(detail: ToastOptions): void {
  window.dispatchEvent(new CustomEvent('lcm-toast', { detail }));
}

const RULES = [
  ['Ergebnis einer Aktion', 'Toast unten rechts', 'Erfolg 5 s, Fehler bleibt'],
  ['Hintergrundereignis', 'Toast mit Link zum Objekt', '8 s, kritisch bleibt'],
  ['Eingabefehler', 'Am Feld und in der Speicherleiste mit „Zum Feld“', 'bis korrigiert'],
  ['Fehler beim Speichern', 'Speicherleiste neben dem Knopf; im Fenster über dem Fuß', 'bis zum nächsten Versuch'],
  ['Zustand eines Objekts', 'Band am Objekt (Kopf der Detailseite)', 'solange der Zustand dauert'],
  ['Systemhinweis', 'Band unter dem Hauptmenü', 'solange der Zustand dauert; Info wegklickbar'],
];

const LABELS: SaveBarLabels = {
  region: 'Änderungen speichern',
  unsaved: 'Du hast ungespeicherte Änderungen',
  hint: 'Strg/Cmd+S speichert',
  save: 'Speichern',
  discard: 'Verwerfen',
  jump: 'Zum Feld',
  fieldError: 'Nicht gespeichert: „{field}“ prüfen',
};

type SaveState = 'dirty' | 'field' | 'server';

/** Meldungen nach Ansatz B: die echten Bausteine lcm-notice, lcm-toaster und lcm-save-bar. */
export class CatMessages extends LitElement {
  static override properties = { _save: { state: true }, _modal: { state: true }, _modalError: { state: true }, _info: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .rules {
        display: grid;
        grid-template-columns: max-content minmax(0, 1fr) max-content;
        gap: var(--space-2) var(--space-5);
        max-inline-size: 60rem;
        margin: 0 0 var(--space-5);
        font-size: var(--fs-s);
      }
      .rules dt {
        font-weight: var(--fw-semibold);
      }
      .rules dd {
        margin: 0;
        color: var(--text-2);
      }
      .stack {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        max-inline-size: 48rem;
      }
      .frame {
        margin-inline: calc(var(--space-4) * -1);
        border-block: 1px solid var(--line-muted);
      }
      .form {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
        max-inline-size: 48rem;
        padding: var(--space-4);
        border-radius: var(--radius-l);
        box-shadow: inset 0 0 0 1px var(--line-muted);
      }
      .row {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-end: var(--space-3);
      }
      @media (max-width: 50rem) {
        .rules {
          grid-template-columns: minmax(0, 1fr);
        }
      }
    `,
  ];

  declare _save: SaveState;
  declare _modal: boolean;
  declare _modalError: boolean;
  declare _info: boolean;

  constructor() {
    super();
    this._save = 'dirty';
    this._modal = false;
    this._modalError = false;
    this._info = true;
  }

  #field(): SaveBarField | null {
    if (this._save !== 'field') return null;
    return { label: 'SMTP-Port', element: () => this.renderRoot.querySelector<HTMLElement>('#port') };
  }

  #notices() {
    return html`<h3 class="sub">lcm-notice · line (Karten, Fenster)</h3>
      <div class="stack">
        <lcm-notice>Der Schlüssel wird nur jetzt angezeigt. Bewahre ihn sicher auf.</lcm-notice>
        <lcm-notice tone="ok" heading="Sicherung eingespielt">LCM startet in wenigen Sekunden neu.</lcm-notice>
        <lcm-notice tone="warn" heading="Docker umgeht die Firewall" action-label="Ports ansehen">Die Ports 8080 und 8443 sind von außen erreichbar.</lcm-notice>
        <lcm-notice tone="crit">Die Datenbank ist gerade schreibgeschützt, weil ein Backup läuft.</lcm-notice>
      </div>
      <h3 class="sub">lcm-notice · band (Zustand am Objekt)</h3>
      <div class="stack">
        <lcm-notice variant="band" tone="crit" heading="Nicht erreichbar">SSH: Zeitüberschreitung nach 10 s</lcm-notice>
        <lcm-notice variant="band" tone="crit" heading="LCM handlungsunfähig">Paketverwaltung oder Verbindung sind ausgefallen.</lcm-notice>
        <lcm-notice variant="band" tone="info" heading="Wartungsmodus" action-label="Einstellungen">Ist der Server nicht erreichbar, bleibt die Ampel zunächst ruhig.</lcm-notice>
        <lcm-notice variant="band" tone="warn" heading="web03 ist belegt: Updates einspielen">
          <a href="#messages">Job ansehen</a>
          <lcm-button slot="action" size="small" variant="outline" icon="x">Abbrechen</lcm-button>
        </lcm-notice>
      </div>
      <h3 class="sub">lcm-notice · bar (Systemhinweis unter dem Menü)</h3>
      <div class="stack frame">
        <lcm-notice variant="bar" tone="warn">Ändere zuerst dein Passwort. Bis dahin sind alle anderen Seiten gesperrt.</lcm-notice>
        ${this._info
          ? html`<lcm-notice variant="bar" tone="info" dismissible close-label="Hinweis schließen" @lcm-dismiss=${() => (this._info = false)}
              >LCM 1.43.0 ist verfügbar (installiert: 1.42.2).
              <lcm-button slot="action" size="small" variant="primary" icon="download">Jetzt installieren</lcm-button></lcm-notice
            >`
          : html`<div class="row"><lcm-button size="small" variant="ghost" @click=${() => (this._info = true)}>Hinweis wieder zeigen</lcm-button></div>`}
      </div>`;
  }

  #toasts() {
    return html`<h3 class="sub">lcm-toaster</h3>
      <p class="lead">Höchstens drei sichtbar, darüber „+n weitere“. Hover oder Fokus hält an. Bei offenem Fenster warten neue Toasts, kritische erscheinen sofort.</p>
      <div class="row">
        <lcm-button @click=${() => toast({ tone: 'ok', title: 'Paketlisten von web03 sind aktuell', message: '27 Updates verfügbar.' })}>Ergebnis</lcm-button>
        <lcm-button @click=${() => toast({ tone: 'crit', title: 'Paketlisten von db02 nicht geladen', message: 'Die Quelle antwortet nicht.' })}>Fehler (bleibt)</lcm-button>
        <lcm-button @click=${() => toast({ tone: 'ok', title: 'Updates auf web03 eingespielt', message: '27 Pakete in 3 min 12 s.', background: true, actionLabel: 'Job ansehen' })}
          >Hintergrund (8 s)</lcm-button
        >
        <lcm-button
          @click=${() => {
            for (const name of ['web01', 'web02', 'db01', 'db02', 'backup01']) toast({ tone: 'ok', title: `Updates auf ${name} eingespielt`, background: true });
          }}
          >Fünf auf einmal</lcm-button
        >
        <lcm-button @click=${() => (this._modal = true)}>Fenster öffnen</lcm-button>
      </div>`;
  }

  #saveBar() {
    const state = this._save;
    return html`<h3 class="sub">lcm-save-bar</h3>
      <div class="row">
        <lcm-button size="small" variant=${state === 'dirty' ? 'primary' : 'outline'} @click=${() => (this._save = 'dirty')}>Ungespeichert</lcm-button>
        <lcm-button size="small" variant=${state === 'field' ? 'primary' : 'outline'} @click=${() => (this._save = 'field')}>Feldfehler</lcm-button>
        <lcm-button size="small" variant=${state === 'server' ? 'primary' : 'outline'} @click=${() => (this._save = 'server')}>Serverfehler</lcm-button>
      </div>
      <div class="form">
        <lcm-input label="SMTP-Server" mono value="mail.techeve.de"></lcm-input>
        <lcm-input id="port" label="SMTP-Port" width="s" mono value="70000" error=${state === 'field' ? 'Erlaubt sind 1 bis 65535.' : ''}></lcm-input>
        <lcm-save-bar
          sticky
          dirty
          error=${state === 'server' ? 'Die Datenbank ist gerade schreibgeschützt, weil ein Backup läuft.' : ''}
          .field=${this.#field()}
          .labels=${LABELS}
        ></lcm-save-bar>
      </div>
      <h3 class="sub">lcm-save-bar · in einer kurzen Karte (always)</h3>
      <div class="form">
        <lcm-input label="Subscription-Schlüssel" width="l" mono value="LCM-E-12345-ABCDE"></lcm-input>
        <lcm-save-bar always dirty error="Der Schlüssel ist abgelaufen." .labels=${{ ...LABELS, save: 'Aktivieren', discard: '', unsaved: '' }}></lcm-save-bar>
      </div>`;
  }

  #modal() {
    return html`<lcm-modal ?open=${this._modal} heading="Server bearbeiten" subtitle="web03" close-label="Schließen" @lcm-close=${() => ((this._modal = false), (this._modalError = false))}>
      <lcm-input label="Hostname oder IP" mono value="web03.prod" error=${this._modalError ? 'web03.prod lässt sich nicht auflösen.' : ''}></lcm-input>
      <div class="row">
        <lcm-button size="small" variant="ghost" @click=${() => toast({ tone: 'ok', title: 'Job fertig', background: true })}>Hintergrund-Toast (wartet)</lcm-button>
        <lcm-button size="small" variant="ghost" @click=${() => toast({ tone: 'crit', title: 'backup01 ist kritisch', message: 'Datenträger / ist zu 97 % belegt.', background: true })}
          >Kritisch (sofort)</lcm-button
        >
      </div>
      ${this._modalError ? html`<lcm-notice slot="note" tone="crit" heading="Nicht gespeichert">Bitte prüfe das markierte Feld.</lcm-notice>` : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => (this._modal = false)}>Abbrechen</lcm-button>
      <lcm-button slot="footer" variant="primary" @click=${() => (this._modalError = true)}>Speichern</lcm-button>
    </lcm-modal>`;
  }

  override render() {
    return html`<section>
      <h2>Meldungen</h2>
      <p class="lead">
        Kurzes schwebt unten rechts (Toast), Behebbares steht am Auslöser (Feld, Speicherleiste, Fensterfuß), Dauerhaftes hängt am Objekt (Band). Eine Anatomie für alle: Ampel in
        Farbe und Form, Titel, ein Satz Text, höchstens eine Folgeaktion, Tonstreifen links. Konzept: <span class="mono">doc/frontend-neubau-meldungen-konzept.md</span>.
      </p>
      <dl class="rules">${RULES.map(([kind, place, stays]) => html`<dt>${kind}</dt><dd>${place}</dd><dd>${stays}</dd>`)}</dl>
      ${this.#notices()} ${this.#toasts()} ${this.#saveBar()} ${this.#modal()}
    </section>`;
  }
}
customElements.define('cat-messages', CatMessages);
