import { LitElement, css, html } from 'lit';
import '../../components/lcm-badge.js';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-empty-state.js';
import '../../components/lcm-job-progress.js';
import '../../components/lcm-meter.js';
import '../../components/lcm-skeleton.js';
import '../../components/lcm-tabs.js';
import type { TabItem } from '../../components/lcm-tabs.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';

const TABS: TabItem[] = [
  { id: 'overview', label: 'Übersicht' },
  { id: 'packages', label: 'Pakete', badge: '27', badgeTone: 'warn' },
  { id: 'security', label: 'Sicherheit', badge: '2', badgeTone: 'crit' },
  { id: 'jobs', label: 'Jobs' },
];

const TAB_TEXT: Record<string, string> = {
  overview: 'Zustand, Betriebsfähigkeit und die letzten Läufe auf einen Blick.',
  packages: '27 Pakete haben Updates, davon 3 mit Sicherheitsbezug.',
  security: 'Zwei offene CVEs mit verfügbarem Fix: openssl und libssl3.',
  jobs: 'Zuletzt: Paketlisten aktualisieren, fehlgeschlagen vor 41 Minuten.',
};

export class CatSurfaces extends LitElement {
  static override properties = { tab: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .stat {
        display: flex;
        align-items: baseline;
        gap: var(--space-3);
      }
      .stat strong {
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        font-variant-numeric: tabular-nums;
      }
      .chart-title {
        margin: 0 0 var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .tab-text {
        color: var(--text-2);
      }
    `,
  ];
  declare tab: string;
  constructor() {
    super();
    this.tab = 'overview';
  }

  override render() {
    return html`<section>
      <h2>Karten und Bausteine</h2>
      <p class="lead">Geschichtete Flächen statt harter Rahmen: Karten heben sich über Erhebung und Flächenstufe ab. Richtung C zeichnet stattdessen feine Linien.</p>
      <div class="grid cols-3">
        <lcm-card heading="Karte" subtitle="Standard">
          <p class="muted">Inhalt auf Flächenstufe 1. Kopf, Inhalt und optionaler Fuß.</p>
          <lcm-button slot="actions" size="small" variant="ghost" icon="refresh" icon-only label="Aktualisieren"></lcm-button>
          <span slot="footer">Zuletzt aktualisiert vor 2 Minuten</span>
        </lcm-card>
        <lcm-card heading="Inset" subtitle="variant=inset" variant="inset"><p class="muted">Eine Stufe tiefer, ohne Schatten, für Gruppierungen innerhalb von Karten.</p></lcm-card>
        <lcm-card heading="Kennzahl">
          <div class="stat"><strong>27</strong><lcm-badge tone="warn">+4 seit gestern</lcm-badge></div>
          <p class="muted small">Pakete mit offenem Update</p>
        </lcm-card>
      </div>

      <h3 class="sub">Reiter</h3>
      <lcm-card>
        <lcm-tabs label="Beispiel" .tabs=${TABS} value=${this.tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => (this.tab = e.detail.id)}>
          <p class="tab-text">${TAB_TEXT[this.tab]}</p>
        </lcm-tabs>
      </lcm-card>

      <h3 class="sub">Kleine Diagramme (reines SVG)</h3>
      <div class="grid cols-2">
        <lcm-card heading="Fehlgeschlagene Jobs pro Tag">
          <p class="chart-title">Als Zeilenbegleiter in Tabellen und Kacheln</p>
          <div class="stack">
            <lcm-meter value="41" label="Speicher"></lcm-meter>
            <lcm-meter value="79" label="Speicher"></lcm-meter>
            <lcm-meter value="93" label="Speicher"></lcm-meter>
          </div>
        </lcm-card>
      </div>

      <h3 class="sub">Job-Fortschritt</h3>
      <div class="grid cols-2">
        <lcm-card>
          <div class="stack">
            <lcm-job-progress
              state="running"
              percent="62"
              label="Updates einspielen auf git01"
              state-label="läuft gerade"
              detail="Paket 9 von 14: libssl3 (3.0.15-1)"
              .steps=${[
                { label: 'Paketlisten', state: 'done' },
                { label: 'Herunterladen', state: 'done' },
                { label: 'Installieren', state: 'active' },
                { label: 'Aufräumen', state: 'pending' },
              ]}
            ></lcm-job-progress>
            <lcm-job-progress state="running" indeterminate meta="seit 0:32" label="CVE-Scan auf db01" state-label="läuft gerade"></lcm-job-progress>
          </div>
        </lcm-card>
        <lcm-card>
          <div class="stack">
            <lcm-job-progress state="ok" percent="100" label="Updates einspielen auf web02" state-label="erfolgreich" detail="12 Pakete aktualisiert in 3:41"></lcm-job-progress>
            <lcm-job-progress
              state="failed"
              percent="18"
              label="Paketlisten aktualisieren auf web03"
              state-label="fehlgeschlagen"
              detail="security.debian.org antwortet nicht (Zeitüberschreitung)"
              .steps=${[
                { label: 'Verbinden', state: 'done' },
                { label: 'Paketlisten', state: 'failed' },
              ]}
            ></lcm-job-progress>
            <lcm-job-progress state="queued" percent="0" label="Neustart von mail01" state-label="wartet" detail="Wartet auf den laufenden Job auf diesem Server"></lcm-job-progress>
          </div>
        </lcm-card>
      </div>

      <h3 class="sub">Leerzustand und Ladeskelett</h3>
      <div class="grid cols-2">
        <lcm-card>
          <lcm-empty-state icon="server" heading="Noch keine Server" text="Hier erscheinen alle Linux-Server, die LCM verwaltet, mit Ampel, Updates und Sicherheitslage. Der erste Schritt: einen Server hinzufügen, per Agent oder SSH.">
            <lcm-button slot="action" variant="primary" icon="plus">Server hinzufügen</lcm-button>
            <lcm-button slot="action" variant="ghost">Wie funktioniert das?</lcm-button>
          </lcm-empty-state>
        </lcm-card>
        <lcm-card>
          <div class="stack">
            <div class="row"><lcm-skeleton shape="circle" label="Lädt"></lcm-skeleton><div class="grow"><lcm-skeleton lines="2"></lcm-skeleton></div></div>
            <lcm-skeleton shape="row" lines="4"></lcm-skeleton>
          </div>
        </lcm-card>
      </div>
    </section>`;
  }
}
customElements.define('cat-surfaces', CatSurfaces);
