import { LitElement, css, html } from 'lit';
import '../../components/lcm-ampel.js';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-status-tile.js';
import { nextRuns } from '../../components/cron.js';
import { baseStyles, type StatusLevel } from '../../components/shared.js';
import { appNav } from '../app-frame.js';
import { catalogStyles } from '../catalog-styles.js';
import { alerts, attention, jobs, schedules } from '../data.js';

const NOW = new Date('2026-11-01T14:30:00');
const dateFormat = new Intl.DateTimeFormat('de', { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });

const TILES: { level: StatusLevel; label: string; count: number; hint: string }[] = [
  { level: 'great', label: 'hervorragend', count: 9, hint: 'Alles aktuell' },
  { level: 'ok', label: 'in Ordnung', count: 21, hint: 'Kleine Updates stehen an' },
  { level: 'warn', label: 'Warnung', count: 4, hint: 'Bald Aufmerksamkeit nötig' },
  { level: 'crit', label: 'kritisch', count: 2, hint: 'Jetzt handeln' },
  { level: 'unknown', label: 'LCM handlungsunfähig', count: 1, hint: 'Agent oder SSH nicht erreichbar' },
];

export class CatDashboard extends LitElement {
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .frame {
        container-type: inline-size;
      }
      .head {
        display: flex;
        align-items: flex-end;
        justify-content: space-between;
        gap: var(--space-4);
        margin-block-end: var(--space-5);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.15;
      }
      .live {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin: var(--space-1) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .tiles {
        display: grid;
        grid-template-columns: repeat(5, minmax(0, 1fr));
        gap: var(--gap);
        margin-block-end: var(--gap);
      }
      .columns {
        display: grid;
        grid-template-columns: 1.3fr 1.1fr 1fr 1fr;
        gap: var(--gap);
        align-items: stretch;
      }
      @container (max-width: 75rem) {
        .tiles {
          grid-template-columns: repeat(3, minmax(0, 1fr));
        }
        .columns {
          grid-template-columns: repeat(2, minmax(0, 1fr));
        }
      }
      @container (max-width: 44rem) {
        .tiles,
        .columns {
          grid-template-columns: minmax(0, 1fr);
        }
      }
      ul {
        display: flex;
        flex-direction: column;
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr) auto;
        gap: var(--space-1) var(--space-3);
        align-items: start;
        padding-block: var(--space-3);
        border-block-start: 1px solid var(--line);
      }
      li:first-child {
        padding-block-start: 0;
        border-block-start: 0;
      }
      li:last-child {
        padding-block-end: 0;
      }
      li lcm-ampel {
        margin-block-start: 0.2rem;
      }
      .title {
        font-weight: var(--fw-semibold);
        overflow-wrap: break-word;
      }
      .upcoming li {
        grid-template-columns: auto minmax(0, 1fr);
      }
      .title a {
        color: var(--text-1);
        text-decoration: none;
      }
      .title a:hover {
        text-decoration: underline;
      }
      .sub {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .meta {
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        font-variant-numeric: tabular-nums;
        white-space: nowrap;
      }
      .srv {
        font-family: var(--font-mono);
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
      .act {
        grid-column: 2 / -1;
        justify-self: start;
        margin-block-start: var(--space-1);
      }
      .when {
        display: flex;
        flex-direction: column;
        align-items: flex-start;
        min-inline-size: 4.75rem;
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        font-variant-numeric: tabular-nums;
      }
      .when strong {
        font-size: var(--fs-m);
      }
    `,
  ];

  override render() {
    return html`<section>
      <h2>Dashboard</h2>
      <p class="lead">Oben die Statuskacheln (jede führt zur gefilterten Serverliste), darunter die Spalten Aufmerksamkeit, Verlauf, Zeitpläne und Alarme. Alles aktualisiert sich live über den Ereigniskanal.</p>
      <div class="frame">
        ${appNav('dashboard')}
        <div class="page">
          <div class="head">
            <div>
              <h1>Dashboard</h1>
              <p class="live"><lcm-ampel level="great" size="s"></lcm-ampel>Zustand der Flotte · 37 Server · live verbunden</p>
            </div>
            <lcm-button variant="primary" icon="plus">Server hinzufügen</lcm-button>
          </div>
          <div class="tiles">${TILES.map((t) => html`<lcm-status-tile level=${t.level} label=${t.label} count=${t.count} hint=${t.hint} href=${`#servers?status=${t.level}`}></lcm-status-tile>`)}</div>
          <div class="columns">
            <lcm-card level="2" heading="Braucht Aufmerksamkeit" subtitle="Rote und gelbe Server">
              <ul>
                ${attention.map(
                  (a) => html`<li>
                    <lcm-ampel level=${a.level} size="m" label=${a.level_label}></lcm-ampel>
                    <div>
                      <div class="title"><span class="srv">${a.server}</span> · ${a.title}</div>
                      <p class="sub">${a.text}</p>
                    </div>
                    <lcm-button class="act" size="small" variant="outline">${a.action}</lcm-button>
                  </li>`,
                )}
              </ul>
            </lcm-card>
            <lcm-card level="2" heading="Zuletzt gelaufen" subtitle="Die letzten Jobs">
              <ul>
                ${jobs.map(
                  (j) => html`<li>
                    <lcm-ampel level=${j.level} size="m" label=${j.result}></lcm-ampel>
                    <div>
                      <div class="title"><span class="srv">${j.server}</span> · ${j.kind}</div>
                      <p class="sub">${j.result} · ${j.when}</p>
                    </div>
                    <span class="meta">${j.duration}</span>
                  </li>`,
                )}
              </ul>
            </lcm-card>
            <lcm-card level="2" heading="Demnächst" subtitle="Nächste Läufe der Zeitpläne">
              <ul class="upcoming">
                ${schedules.map((s) => {
                  const next = nextRuns(s.cron, NOW, 1)[0]!;
                  const [day, time] = dateFormat.format(next).split(', ').reduce<string[]>((acc, part, i, all) => (i < all.length - 1 ? [`${acc[0] ? `${acc[0]}, ` : ''}${part}`] : [acc[0] ?? '', part]), []);
                  return html`<li>
                    <span class="when"><strong>${time}</strong><span class="muted">${day}</span></span>
                    <div>
                      <div class="title">${s.group}</div>
                      <p class="sub">${s.rules}</p>
                    </div>
                  </li>`;
                })}
              </ul>
            </lcm-card>
            <lcm-card level="2" heading="Letzte Alarme">
              <ul>
                ${alerts.map(
                  (a) => html`<li>
                    <lcm-ampel level=${a.level} size="m" label=${a.level === 'crit' ? 'kritisch' : 'Warnung'}></lcm-ampel>
                    <div>
                      <div class="title">${a.title}</div>
                      <p class="sub"><span class="srv">${a.server}</span></p>
                    </div>
                    <span class="meta">${a.when}</span>
                  </li>`,
                )}
              </ul>
            </lcm-card>
          </div>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-dashboard', CatDashboard);
