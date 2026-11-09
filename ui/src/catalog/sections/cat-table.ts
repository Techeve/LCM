import { LitElement, css, html } from 'lit';
import '../../components/lcm-button.js';
import '../../components/lcm-data-table.js';
import '../../components/lcm-meter.js';
import '../../components/lcm-server-action.js';
import type { Column } from '../../components/lcm-data-table.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { osOptions, servers, statusOptions } from '../data.js';
import { actionLabels, tableLabels } from '../text.js';

type StatusCell = { level: string; label: string };

export const serverColumns: Column[] = [
  { key: 'status', label: 'Status', kind: 'status', sortable: true, filter: 'select', options: statusOptions, value: (r) => (r.status as StatusCell).label, sortValue: (r) => r.statusRank as number },
  { key: 'name', label: 'Name', sortable: true, filter: 'text', locked: true },
  { key: 'host', label: 'Host', kind: 'mono', sortable: true, filter: 'text' },
  { key: 'os', label: 'Betriebssystem', kind: 'os', sortable: true, filter: 'select', options: osOptions, value: (r) => r.osName as string },
  { key: 'kernel', label: 'Kernel', kind: 'mono', sortable: true, filter: 'text' },
  { key: 'updates', label: 'Updates', sortable: true, align: 'end' },
  { key: 'seen', label: 'Zuletzt gesehen' },
  {
    key: 'storage',
    label: 'Speicher',
    sortable: true,
    render: (r) => html`<lcm-meter value=${r.storage as number} label="Speicher belegt auf ${r.name}"></lcm-meter>`,
  },
  { key: 'group', label: 'Gruppe', sortable: true, hidden: true },
];

export class CatTable extends LitElement {
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .hint {
        margin-block-start: var(--space-3);
      }
      kbd {
        padding: 0.05rem 0.4rem;
        border-radius: var(--radius-s);
        background: var(--surface-2);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
    `,
  ];

  override render() {
    return html`<section>
      <h2>Datentabelle</h2>
      <p class="lead">Sortierbare Köpfe, Filterzeile, Spaltenwahl, Auswahl mit Sammelleiste, Distributionslogos und zwei Dichten. Die Zeilen sind per Tastatur bedienbar. Die Aktionen der Sammelleiste tragen dieselbe Erklärblase wie im Serverdetail.</p>
      <lcm-data-table
        .columns=${serverColumns}
        .rows=${servers}
        .labels=${tableLabels}
        selectable
        density-toggle
        active-id="srv-103"
      >
        <lcm-button slot="toolbar" variant="primary" icon="plus">Server hinzufügen</lcm-button>
        <lcm-server-action
          slot="bulk"
          label="Updates einspielen"
          icon="bolt"
          variant="primary"
          help="Installiert auf allen ausgewählten Servern die verfügbaren Updates. Dienste werden dabei bei Bedarf neu gestartet."
          meaning="Pro Server läuft ein eigener Job. Server mit laufendem Job werden eingereiht."
          .labels=${actionLabels}
          .preview=${{
            commands: ['apt-get update', 'apt-get -y dist-upgrade'],
            reboot: false,
            reversible: false,
            duration: 'etwa 4 Minuten je Server',
            docsHref: '#doku-updates',
          }}
        ></lcm-server-action>
        <lcm-server-action
          slot="bulk"
          label="Paketlisten aktualisieren"
          icon="refresh"
          help="Liest die Paketquellen neu ein und zeigt, welche Updates verfügbar sind. Es wird nichts installiert."
          .labels=${actionLabels}
          .preview=${{ commands: ['apt-get update'], reboot: false, reversible: true, duration: 'etwa 10 Sekunden' }}
        ></lcm-server-action>
      </lcm-data-table>
      <p class="hint small muted">Tastatur: <kbd>↑</kbd> <kbd>↓</kbd> wechseln die Zeile, <kbd>Pos1</kbd>/<kbd>Ende</kbd> springen, <kbd>Leertaste</kbd> markiert, <kbd>Enter</kbd> öffnet. Die Zeile „web03“ ist als aktives Detail hervorgehoben.</p>
    </section>`;
  }
}
customElements.define('cat-table', CatTable);
