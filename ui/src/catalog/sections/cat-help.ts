import { LitElement, css, html } from 'lit';
import '../../components/lcm-badge.js';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-explainer.js';
import '../../components/lcm-server-action.js';
import '../../components/lcm-switch.js';
import '../../components/lcm-term.js';
import type { ActionPreview } from '../../components/lcm-server-action.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { actionLabels } from '../text.js';

const UPDATE_PREVIEW: ActionPreview = {
  commands: ['apt-get update', 'apt-get -y dist-upgrade', 'needrestart -r a'],
  packages: ['openssl', 'libssl3', 'linux-image-6.1.0-28-amd64', '… und 24 weitere'],
  reboot: true,
  reversible: false,
  duration: 'etwa 4 bis 8 Minuten',
  docsHref: '#doku-updates',
};

export class CatHelp extends LitElement {
  static override properties = { preview: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .with-explainer {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: var(--gap);
        align-items: start;
      }
      .rules {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .rules li {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
      }
      .rules .name {
        flex: 1;
        font-weight: var(--fw-medium);
      }
      .rules .what {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      lcm-explainer p {
        margin: 0 0 var(--space-3);
      }
      p.prose {
        max-inline-size: 46rem;
        color: var(--text-2);
      }
    `,
  ];
  declare preview: ActionPreview;
  private timer = 0;

  constructor() {
    super();
    this.preview = {};
  }

  private loadPreview(): void {
    this.preview = { loading: true };
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => (this.preview = UPDATE_PREVIEW), 700);
  }

  override render() {
    return html`<section>
      <h2>Erklärungen</h2>
      <p class="lead">Die Oberfläche erklärt sich selbst, ohne unter jedem Knopf Text zu zeigen: große Hover-Blasen an Server-Aktionen, eine zusammenklappbare Erklärspalte auf komplexen Seiten und Glossarbegriffe.</p>

      <h3 class="sub">Erklärblase an Server-Aktionen (400 ms Verzögerung, auch bei Tastaturfokus)</h3>
      <lcm-card heading="Serveraktionen" subtitle="Mit der Maus verweilen oder per Tab fokussieren. Ohne help-Text wirft die Komponente im Entwicklungsmodus einen Fehler.">
        <div class="row" id="actions-row">
          <lcm-server-action
            id="act-update"
            label="Updates einspielen"
            icon="bolt"
            variant="primary"
            help="Installiert alle verfügbaren Updates auf diesem Server. Dienste werden dabei bei Bedarf neu gestartet."
            meaning="Der Server ist während der Dienst-Neustarts kurz nicht erreichbar. Ein Neustart des ganzen Servers wird nur vorgeschlagen, nie ohne dich ausgeführt."
            .labels=${actionLabels}
            .preview=${this.preview}
            @lcm-help-open=${this.loadPreview}
          ></lcm-server-action>
          <lcm-server-action
            label="Paketlisten aktualisieren"
            icon="refresh"
            help="Liest die Paketquellen neu ein und zeigt, welche Updates es gibt. Es wird nichts installiert."
            .labels=${actionLabels}
            .preview=${{ commands: ['apt-get update'], reboot: false, reversible: true, duration: 'etwa 10 Sekunden' }}
          ></lcm-server-action>
          <lcm-server-action
            label="Neustart"
            icon="power"
            help="Startet den ganzen Server neu."
            meaning="Alle Dienste und Verbindungen auf web03 sind für ein bis zwei Minuten weg."
            .labels=${actionLabels}
            .preview=${{ commands: ['systemctl reboot'], reboot: true, reversible: true, duration: 'etwa 90 Sekunden' }}
          ></lcm-server-action>
          <lcm-server-action
            label="Terminal"
            icon="terminal"
            variant="outline"
            help="Öffnet eine Shell auf diesem Server im Browser. Alles, was du tippst, läuft mit deinem Linux-Benutzer."
            meaning="Die Sitzung wird aufgezeichnet und erscheint unter „SSH-Sitzungen“."
            .labels=${actionLabels}
            .preview=${{ commands: ['ssh techeve@web03.prod.techeve.de'], duration: 'sofort', docsHref: '#doku-terminal' }}
          ></lcm-server-action>
        </div>
      </lcm-card>

      <h3 class="sub">Begriffe aus dem Glossar</h3>
      <p class="prose">
        Ein <lcm-term heading="Kerndienst" definition="Ein Dienst, der für den Betrieb des Servers zentral ist, etwa Datenbank, Webserver oder Proxy. Seine Pakete aktualisiert LCM nur über die Patch-Richtlinie." docs-href="#doku-kerndienst" docs-label="Mehr in der Doku">Kerndienst</lcm-term>
        gehört zu einem <lcm-term heading="Verbund" definition="Mehrere Server, die zusammen einen Dienst bilden. LCM aktualisiert sie nacheinander, damit immer einer läuft.">Verbund</lcm-term>. Gilt für eine Gruppe die <lcm-term heading="Vorrang" definition="Wenn mehrere Gruppen eine Regel setzen, gewinnt die Gruppe mit dem höheren Vorrang.">Vorrang</lcm-term>-Reihenfolge, steht die <lcm-term heading="Grundsatz-Regel" definition="Eine Regel, die immer gilt und von keiner Gruppe außer Kraft gesetzt werden kann.">Grundsatz-Regel</lcm-term> an erster Stelle.
        Fahre mit der Maus über einen unterstrichenen Begriff oder springe mit Tab hin.
      </p>

      <h3 class="sub">Erklärspalte (merkt sich je Seite, ob sie zu ist)</h3>
      <div class="with-explainer">
        <lcm-card heading="Regeln der Gruppe Produktion" subtitle="Reihenfolge entspricht dem Vorrang">
          <ul class="rules">
            <li><lcm-badge tone="accent">1</lcm-badge><span class="name">Sicherheitsupdates sofort</span><span class="what">täglich 03:00 · 6 Server</span></li>
            <li><lcm-badge>2</lcm-badge><span class="name">Kerndienste zuletzt</span><span class="what">sonntags 03:30 · 3 Server</span></li>
            <li><lcm-badge>3</lcm-badge><span class="name">Neustart im Wartungsfenster</span><span class="what">sonntags 04:30 · 6 Server</span></li>
          </ul>
        </lcm-card>
        <lcm-explainer
          topic="catalog-group-rules"
          heading="Wozu Regeln?"
          docs-href="#doku-regeln"
          docs-label="Mehr in der Doku"
          collapse-label="Erklärung einklappen"
          expand-label="Erklärung ausklappen"
        >
          <p>Eine Regel sagt LCM, <strong>was</strong> auf den Servern einer Gruppe <strong>wann</strong> passiert, ganz ohne dass du daran denken musst.</p>
          <ul>
            <li>Regeln werden von oben nach unten ausgewertet.</li>
            <li>Ein Server in mehreren Gruppen folgt der Regel mit dem höheren <lcm-term definition="Wenn mehrere Gruppen eine Regel setzen, gewinnt die Gruppe mit dem höheren Vorrang.">Vorrang</lcm-term>.</li>
            <li>Änderungen gelten ab dem nächsten Lauf.</li>
          </ul>
        </lcm-explainer>
      </div>
    </section>`;
  }
}
customElements.define('cat-help', CatHelp);
