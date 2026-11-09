import { LitElement, css, html } from 'lit';
import '../../components/lcm-ampel.js';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-status.js';
import { baseStyles, type StatusLevel } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { levelLabels } from '../text.js';

const LEVELS: StatusLevel[] = ['great', 'ok', 'warn', 'crit', 'unknown', 'running'];

const MEANINGS: Record<StatusLevel, string> = {
  great: 'Alles aktuell, nichts zu tun.',
  ok: 'Läuft, kleine Updates stehen an.',
  warn: 'Braucht bald Aufmerksamkeit.',
  crit: 'Jetzt handeln: Sicherheit oder Betrieb gefährdet.',
  unknown: 'LCM hat keine aktuellen Daten.',
  running: 'Ein Job läuft gerade auf dem Server.',
};

export class CatStatus extends LitElement {
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      table {
        border-collapse: collapse;
      }
      th {
        padding: 0 var(--space-5) var(--space-3) 0;
        color: var(--text-2);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-align: start;
        text-transform: var(--label-transform);
      }
      td {
        padding: var(--space-2) var(--space-5) var(--space-2) 0;
      }
      .insight h4 {
        margin: 0 0 var(--space-1);
        font-size: var(--fs-m);
      }
      .insight p {
        margin: 0 0 var(--space-3);
        color: var(--text-2);
      }
    `,
  ];

  override render() {
    return html`<section>
      <h2>Ampel</h2>
      <p class="lead">Das Markenzeichen: ein Punkt im Ring, überall gleich und immer mit Text. Die Stufe steht in Farbe <em>und</em> Form (voller Ring mit Hof, ruhiger Ring, Ring mit Lücke, großer Punkt mit Ausrufezeichen, gestrichelter Ring, kreisender Bogen). Bei „läuft gerade“ pulsiert sie; mit <span class="mono">prefers-reduced-motion</span> steht sie still.</p>
      <div class="grid cols-2">
        <lcm-card heading="Stufen und Größen" subtitle="S, M, L, XL">
          <table>
            <thead><tr><th>Stufe</th><th>S</th><th>M</th><th>L</th><th>XL</th><th>Bedeutung</th></tr></thead>
            <tbody>
              ${LEVELS.map(
                (l) => html`<tr>
                  <td><lcm-status level=${l} label=${levelLabels[l]}></lcm-status></td>
                  <td><lcm-ampel level=${l} size="s" label=${levelLabels[l]}></lcm-ampel></td>
                  <td><lcm-ampel level=${l} size="m" label=${levelLabels[l]}></lcm-ampel></td>
                  <td><lcm-ampel level=${l} size="l" label=${levelLabels[l]}></lcm-ampel></td>
                  <td><lcm-ampel level=${l} size="xl" label=${levelLabels[l]}></lcm-ampel></td>
                  <td class="small muted">${MEANINGS[l]}</td>
                </tr>`,
              )}
            </tbody>
          </table>
        </lcm-card>
        <div class="stack">
          <lcm-card heading="Als Pille" subtitle="variant=pill">
            <div class="row">${LEVELS.map((l) => html`<lcm-status level=${l} label=${levelLabels[l]} variant="pill"></lcm-status>`)}</div>
          </lcm-card>
          <lcm-card heading="Mit Insight-Popover" subtitle="Klick öffnet Erklärung und passende Aktion">
            <div class="row">
              <lcm-status level="crit" label="kritisch" variant="pill" insight-label="Details zeigen">
                <div slot="insight" class="insight">
                  <h4>Paketquellen nicht erreichbar</h4>
                  <p>apt update schlägt seit drei Läufen fehl. Der Server bekommt keine Sicherheitsupdates mehr, bis die Quelle wieder antwortet.</p>
                  <lcm-button size="small" variant="primary" icon="repair">Quellen prüfen</lcm-button>
                </div>
              </lcm-status>
              <lcm-status level="warn" label="Warnung" insight-label="Details zeigen">
                <div slot="insight" class="insight">
                  <h4>Neustart seit 12 Tagen nötig</h4>
                  <p>Der Kernel 6.8.0-48 ist installiert, läuft aber noch nicht.</p>
                  <lcm-button size="small" icon="power">Neustart planen</lcm-button>
                </div>
              </lcm-status>
              <lcm-status level="running" label="läuft gerade"></lcm-status>
            </div>
          </lcm-card>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-status', CatStatus);
