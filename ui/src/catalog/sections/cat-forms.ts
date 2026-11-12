import { LitElement, css, html } from 'lit';
import '../../components/lcm-card.js';
import '../../components/lcm-combobox.js';
import '../../components/lcm-cron.js';
import '../../components/lcm-input.js';
import '../../components/lcm-password.js';
import '../../components/lcm-select.js';
import '../../components/lcm-switch.js';
import type { CronLabels, CronPreset } from '../../components/lcm-cron.js';
import { formLayoutStyles } from '../../components/layout-styles.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { servers } from '../data.js';
import { followChange } from '../follow-change.js';

const PRESETS: CronPreset[] = [
  { label: 'Täglich um 3:00', value: '0 3 * * *' },
  { label: 'Sonntags um 3:30', value: '30 3 * * 0' },
  { label: 'Werktags um 2:00', value: '0 2 * * 1-5' },
  { label: 'Alle 6 Stunden', value: '0 */6 * * *' },
  { label: 'Monatlich am 1.', value: '15 5 1 * *' },
];

const CRON_LABELS: CronLabels = {
  presets: 'Vorlagen',
  fields: ['Minute', 'Stunde', 'Tag', 'Monat', 'Wochentag'],
  expression: 'Ausdruck',
  next: 'Nächste Läufe',
  invalid: 'Das ist kein gültiger Cron-Ausdruck (fünf Felder: Minute, Stunde, Tag, Monat, Wochentag).',
};

const DESCRIPTIONS: Record<string, string> = {
  '0 3 * * *': 'Jeden Tag um 03:00 Uhr.',
  '30 3 * * 0': 'Jeden Sonntag um 03:30 Uhr.',
  '0 2 * * 1-5': 'Montag bis Freitag um 02:00 Uhr.',
  '0 */6 * * *': 'Alle sechs Stunden, zur vollen Stunde.',
  '15 5 1 * *': 'Am ersten Tag jedes Monats um 05:15 Uhr.',
};

export class CatForms extends LitElement {
  static override properties = { cron: { state: true }, password: { state: true }, host: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .form {
        display: flex;
        flex-direction: column;
        gap: var(--space-5);
      }
      .spaced {
        display: block;
        margin-block-end: var(--gap);
      }
    `,
    formLayoutStyles,
  ];
  declare cron: string;
  declare password: string;
  declare host: string;

  constructor() {
    super();
    this.cron = '30 3 * * 0';
    this.password = 'Kaffee-17-Tasse';
    this.host = 'web03.prod';
  }

  override render() {
    const switchLabels = { 'on-label': 'An:', 'off-label': 'Aus:' };
    return html`<section>
      <h2>Formulare</h2>
      <p class="lead">Jedes Feld bringt Beschriftung, Hinweis und Fehlertext mit. Schalter können beim Überfahren erklären, was „an“ und „aus“ bewirkt. Felder stehen untereinander und sind so breit wie ihr Inhalt, nicht wie der Container.</p>
      <lcm-card class="spaced" heading="Feldbreiten" subtitle="Attribut width an lcm-input, lcm-select, lcm-combobox, lcm-password, lcm-cron">
        <div class="form">
          <lcm-input label="xs · 7rem · Port, kleine Zahlen" width="xs" type="number" mono value="22"></lcm-input>
          <lcm-input label="s · 11rem · Minuten, Tage, kurze Kürzel" width="s" type="number" mono value="30"></lcm-input>
          <lcm-input label="m · 18rem · Benutzer, Hostnamen, Namen (Vorgabe)" mono value="root"></lcm-input>
          <lcm-input label="l · 30rem · E-Mail-Adressen, URLs, Pfade" width="l" mono value="https://lcm.example.com"></lcm-input>
          <lcm-input label="full · 100 % · Schlüssel, Tokens, lange Texte" width="full" mono value="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0b2wN7mRx lcm-onboarding"></lcm-input>
        </div>
      </lcm-card>
      <div class="grid cols-2">
        <lcm-card heading="Server bearbeiten" subtitle="Felder untereinander, kurze Felder nebeneinander">
          <div class="form">
            <lcm-input label="Name" value="web03" hint="Erscheint in Listen, Suche und Benachrichtigungen."></lcm-input>
            <div class="field-row">
              <lcm-input
                label="Hostname oder IP"
                mono
                icon="server"
                .value=${this.host}
                error=${this.host.includes('.techeve.de') ? '' : 'Der Hostname ist unvollständig: Es fehlt die Domain, z. B. .techeve.de.'}
                @lcm-input=${(e: CustomEvent<{ value: string }>) => (this.host = e.detail.value)}
              ></lcm-input>
              <lcm-input label="Port" type="number" mono width="xs" value="22"></lcm-input>
            </div>
            <lcm-select
              label="Gruppe"
              value="prod"
              .options=${[
                { value: 'prod', label: 'Produktion' },
                { value: 'db', label: 'Datenbanken' },
                { value: 'dev', label: 'Entwicklung' },
              ]}
              @lcm-change=${followChange}
            ></lcm-select>
            <lcm-combobox
              label="Server suchen"
              placeholder="Name tippen …"
              empty-text="Kein Server gefunden"
              value="srv-103"
              .options=${servers.map((s) => ({ value: s.id, label: s.name as string, hint: s.osName as string }))}
            ></lcm-combobox>
            <lcm-input label="Bemerkung" width="l" value="Wird nicht mehr verwendet" disabled></lcm-input>
            <lcm-switch
              label="Kerndienste getrennt patchen"
              description="Datenbank, Webserver und Proxy bekommen Updates nur über die Patch-Richtlinie."
              checked
              on-text="Kerndienst-Pakete werden nur über die Patch-Richtlinie aktualisiert."
              off-text="Der Zeitplan aktualisiert alle Pakete, auch die der Kerndienste."
              on-label=${switchLabels['on-label']}
              off-label=${switchLabels['off-label']}
              @lcm-change=${followChange}
            ></lcm-switch>
            <lcm-switch label="Neustart bei Bedarf erlauben" description="Nur im Wartungsfenster." @lcm-change=${followChange}></lcm-switch>
          </div>
        </lcm-card>
        <div class="stack">
          <lcm-card heading="Zeitplan" subtitle="Cron-Baukasten (Skizze)">
            <lcm-cron
              .value=${this.cron}
              .presets=${PRESETS}
              .labels=${CRON_LABELS}
              description=${DESCRIPTIONS[this.cron] ?? ''}
              locale="de"
              .now=${new Date('2026-11-01T14:30:00')}
              @lcm-change=${(e: CustomEvent<{ value: string }>) => (this.cron = e.detail.value)}
            ></lcm-cron>
          </lcm-card>
          <lcm-card heading="Neues Passwort" subtitle="Mit Sichtbarkeit und Stärkeanzeige">
            <lcm-password
              label="Passwort"
              .value=${this.password}
              hint="Mindestens zwölf Zeichen. Ein Satz ist besser als ein Wort."
              strength-title="Passwortstärke"
              .strengthLabels=${['leer', 'sehr schwach', 'schwach', 'gut', 'stark']}
              show-label="Passwort anzeigen"
              hide-label="Passwort verbergen"
              @lcm-input=${(e: CustomEvent<{ value: string }>) => (this.password = e.detail.value)}
            ></lcm-password>
          </lcm-card>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-forms', CatForms);
