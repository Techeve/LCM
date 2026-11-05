import { LitElement, css, html } from 'lit';
import '../../components/lcm-ampel.js';
import '../../components/lcm-badge.js';
import '../../components/lcm-button.js';
import '../../components/lcm-card.js';
import '../../components/lcm-data-table.js';
import '../../components/lcm-meter.js';
import '../../components/lcm-os.js';
import '../../components/lcm-server-action.js';
import '../../components/lcm-status.js';
import '../../components/lcm-tabs.js';
import type { ActionPreview } from '../../components/lcm-server-action.js';
import type { Column } from '../../components/lcm-data-table.js';
import type { TabItem } from '../../components/lcm-tabs.js';
import { baseStyles, type StatusLevel } from '../../components/shared.js';
import { appNav } from '../app-frame.js';
import { catalogStyles } from '../catalog-styles.js';
import { servers, statusOptions } from '../data.js';
import { actionLabels, tableLabels } from '../text.js';

const TABS: TabItem[] = [
  { id: 'overview', label: 'Übersicht' },
  { id: 'repos', label: 'Repositories' },
  { id: 'packages', label: 'Pakete', badge: '27', badgeTone: 'warn' },
  { id: 'snaps', label: 'Snaps' },
  { id: 'containers', label: 'Container', badge: '7' },
  { id: 'apps', label: 'Apps' },
  { id: 'users', label: 'Benutzer' },
  { id: 'security', label: 'Sicherheit', badge: '2', badgeTone: 'crit' },
  { id: 'deepscan', label: 'Deep-Scan' },
  { id: 'storage', label: 'Speicher' },
  { id: 'jobs', label: 'Jobs' },
  { id: 'ssh', label: 'SSH-Sitzungen' },
];

const LIST_COLUMNS: Column[] = [
  { key: 'status', label: 'Status', kind: 'status', sortable: true, filter: 'select', options: statusOptions, value: (r) => (r.status as { label: string }).label, sortValue: (r) => r.statusRank as number },
  { key: 'name', label: 'Name', sortable: true, filter: 'text', locked: true },
  { key: 'os', label: 'OS', render: (r) => html`<lcm-os os=${(r.os as { os: string }).os}></lcm-os>`, hidden: false },
  { key: 'updates', label: 'Updates', sortable: true, align: 'end' },
];

interface Health {
  name: string;
  level: StatusLevel;
  state: string;
  since: string;
  failures: string;
  error?: string;
  hint?: string;
}

const HEALTH: Health[] = [
  { name: 'Verbindung', level: 'great', state: 'in Ordnung', since: 'seit 12 Tagen', failures: 'keine Fehlschläge' },
  {
    name: 'Pakete',
    level: 'crit',
    state: 'kritisch',
    since: 'seit 3 Stunden',
    failures: '3 Fehlschläge in Folge',
    error: 'E: Failed to fetch http://security.debian.org/debian-security/dists/bookworm-security/InRelease  Connection timed out [IP: 151.101.66.132 80]',
    hint: 'Der Server erreicht die Paketquelle nicht. Prüfe Proxy, DNS und den ausgehenden Datenverkehr der Firewall.',
  },
  { name: 'Regeln', level: 'warn', state: 'Warnung', since: 'seit 12 Tagen', failures: '2 Läufe übersprungen', error: 'Regel „Neustart im Wartungsfenster“ wartet auf einen Neustart, der nicht freigegeben ist.', hint: 'Gib den Neustart frei oder verschiebe das Wartungsfenster.' },
  { name: 'CVE-Scan', level: 'ok', state: 'in Ordnung', since: 'seit 4 Tagen', failures: 'letzter Scan vor 26 Minuten' },
];

const REPAIR: ActionPreview = {
  commands: ['apt-get clean', 'rm -f /var/lib/apt/lists/lock', 'apt-get update'],
  reboot: false,
  reversible: true,
  duration: 'etwa 30 Sekunden',
};

export class CatServer extends LitElement {
  static override properties = { tab: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .frame {
        container-type: inline-size;
      }
      .split {
        display: grid;
        grid-template-columns: minmax(21rem, 25rem) minmax(0, 1fr);
        gap: var(--gap);
        align-items: start;
      }
      @container (max-width: 62rem) {
        .split {
          grid-template-columns: minmax(0, 1fr);
        }
      }
      .list-head {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        margin-block-end: var(--space-3);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .detail {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      .crumbs {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .title-row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-4);
        margin: var(--space-1) 0 var(--space-2);
      }
      .title-row h2 {
        margin: 0;
        font-size: var(--fs-3xl);
        letter-spacing: var(--head-tracking);
        line-height: 1.1;
      }
      .facts {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-5);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .facts .mono {
        font-family: var(--font-mono);
      }
      .bar {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-start: var(--space-4);
      }
      .bar .push {
        margin-inline-start: auto;
      }
      .overview {
        display: grid;
        grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
        gap: var(--gap);
        align-items: start;
      }
      @container (max-width: 90rem) {
        .overview {
          grid-template-columns: minmax(0, 1fr);
        }
      }
      .health {
        display: flex;
        flex-direction: column;
      }
      .cat {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: var(--space-1) var(--space-3);
        padding-block: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
      .cat:first-child {
        padding-block-start: 0;
        border-block-start: 0;
      }
      .cat:last-child {
        padding-block-end: 0;
      }
      .cat lcm-ampel {
        margin-block-start: 0.25rem;
      }
      .cat-head {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: var(--space-1) var(--space-3);
      }
      .cat-name {
        font-weight: var(--fw-semibold);
      }
      .cat-state {
        font-weight: var(--fw-medium);
      }
      .cat-meta {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .cat-body {
        grid-column: 2;
      }
      .error {
        margin: var(--space-2) 0 0;
        padding: var(--space-3);
        border-radius: var(--radius-m);
        background: var(--crit-soft);
        color: var(--crit-text);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.5;
        overflow-wrap: anywhere;
      }
      .error.warn {
        background: var(--warn-soft);
        color: var(--warn-text);
        font-family: var(--font-sans);
        font-size: var(--fs-s);
      }
      .hint {
        margin: var(--space-2) 0 0;
        color: var(--text-1);
        font-size: var(--fs-s);
      }
      .actions {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-start: var(--space-3);
      }
      .kv {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-3) var(--space-5);
        align-items: center;
        margin: 0;
      }
      .kv dt {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .kv dd {
        margin: 0;
      }
      .big {
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        font-variant-numeric: tabular-nums;
      }
    `,
  ];
  declare tab: string;
  constructor() {
    super();
    this.tab = 'overview';
  }

  private renderHealth(item: Health) {
    const failing = item.level === 'crit' || item.level === 'warn';
    return html`<div class="cat">
      <lcm-ampel level=${item.level} size="l" label=${item.state}></lcm-ampel>
      <div>
        <div class="cat-head"><span class="cat-name">${item.name}</span><span class="cat-state">${item.state}</span></div>
        <div class="cat-meta">${item.since} · ${item.failures}</div>
      </div>
      ${item.error
        ? html`<div class="cat-body">
            <p class=${item.level === 'crit' ? 'error' : 'error warn'}>${item.error}</p>
            ${item.hint ? html`<p class="hint"><strong>Was tun?</strong> ${item.hint}</p>` : ''}
          </div>`
        : ''}
      ${failing && item.level === 'crit'
        ? html`<div class="cat-body actions">
            <lcm-server-action
              label="Reparieren"
              icon="repair"
              variant="primary"
              help="Räumt den Paketcache und eine hängende Sperre auf und liest die Paketquellen danach neu ein."
              meaning="Nichts wird installiert oder entfernt, nur der Zustand der Paketverwaltung wird bereinigt."
              .labels=${actionLabels}
              .preview=${REPAIR}
            ></lcm-server-action>
            <lcm-server-action
              label="Erneut versuchen"
              icon="refresh"
              help="Wiederholt die letzte Prüfung der Paketquellen. Hilft, wenn das Problem nur kurz bestand."
              .labels=${actionLabels}
              .preview=${{ commands: ['apt-get update'], reboot: false, reversible: true, duration: 'etwa 10 Sekunden' }}
            ></lcm-server-action>
          </div>`
        : ''}
    </div>`;
  }

  override render() {
    return html`<section>
      <h2>Serverseite</h2>
      <p class="lead">Auf breiten Bildschirmen bleibt die Liste links stehen, rechts liegt das Serverdetail. Die Aktionsleiste im Kopf trägt Erklärblasen, die Reiter laden bei Bedarf ein eigenes Modul. Die „Betriebsfähigkeit“ zeigt je Kategorie Zustand, Dauer, Fehler und passende Reparatur.</p>
      <div class="frame">
        ${appNav('servers')}
        <div class="page">
          <div class="split">
            <div>
              <div class="list-head"><h1>Server</h1><lcm-button variant="primary" size="small" icon="plus">Hinzufügen</lcm-button></div>
              <lcm-data-table .columns=${LIST_COLUMNS} .rows=${servers} .labels=${{ ...tableLabels, caption: 'Serverliste' }} density="compact" no-toolbar active-id="srv-103"></lcm-data-table>
            </div>
            <div class="detail">
              <div>
                <div class="crumbs">Server / web03</div>
                <div class="title-row">
                  <h2>web03</h2>
                  <lcm-status level="crit" label="kritisch" variant="pill" size="l" insight-label="Details zeigen">
                    <div slot="insight">
                      <strong>Paketquellen nicht erreichbar</strong>
                      <p>Seit drei Läufen schlägt <span class="mono">apt update</span> fehl. Der Server bekommt keine Sicherheitsupdates mehr, bis die Quelle antwortet.</p>
                    </div>
                  </lcm-status>
                </div>
                <div class="facts">
                  <lcm-os os="debian" label="Debian 12 (bookworm)"></lcm-os>
                  <span class="mono">web03.prod.techeve.de</span>
                  <span class="mono">Kernel 6.1.0-25-amd64</span>
                  <span>Gruppe: Produktion</span>
                  <span>zuletzt gesehen vor 2 Minuten</span>
                </div>
                <div class="bar">
                  <lcm-server-action label="Terminal" icon="terminal" variant="outline" help="Öffnet eine Shell auf diesem Server im Browser. Alles, was du tippst, läuft mit deinem Linux-Benutzer." meaning="Die Sitzung wird aufgezeichnet und erscheint unter „SSH-Sitzungen“." .labels=${actionLabels} .preview=${{ commands: ['ssh techeve@web03.prod.techeve.de'], duration: 'sofort', docsHref: '#doku-terminal' }}></lcm-server-action>
                  <lcm-server-action label="Firewall" icon="firewall" variant="outline" help="Zeigt die Firewall-Regeln dieses Servers und lässt dich sie ändern. Änderungen gelten erst, wenn du sie anwendest." .labels=${actionLabels} .preview=${{ commands: ['ufw status numbered'], reboot: false, reversible: true, duration: 'sofort' }}></lcm-server-action>
                  <lcm-server-action label="sudo einschränken" icon="lock" variant="outline" help="Ersetzt die uneingeschränkte sudo-Berechtigung des LCM-Benutzers durch eine Liste erlaubter Befehle." meaning="LCM kann danach nur noch die Befehle ausführen, die es für seine Aufgaben braucht." .labels=${actionLabels} .preview=${{ commands: ['visudo -cf /etc/sudoers.d/lcm', 'install -m 0440 lcm.sudoers /etc/sudoers.d/lcm'], reboot: false, reversible: true, duration: 'etwa 5 Sekunden' }}></lcm-server-action>
                  <lcm-server-action label="Einstellungen" icon="settings" variant="outline" help="Öffnet die Einstellungen dieses Servers: Name, Zugangsdaten, Gruppen und Regeln. Es wird nichts auf dem Server ausgeführt." .labels=${actionLabels} .preview=${{ reboot: false, duration: 'sofort' }}></lcm-server-action>
                  <lcm-server-action class="push" label="Entfernen" icon="trash" variant="danger" help="Entfernt web03 aus LCM. Der Agent und die Zugangsschlüssel werden vom Server gelöscht, der Server selbst läuft weiter." meaning="Verlauf und Statistik dieses Servers gehen verloren." .labels=${actionLabels} .preview=${{ commands: ['systemctl disable --now lcm-agent', 'rm -f /etc/lcm/agent.conf'], reboot: false, reversible: false, duration: 'etwa 10 Sekunden' }}></lcm-server-action>
                </div>
              </div>
              <lcm-tabs label="Serverdetail" .tabs=${TABS} value=${this.tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => (this.tab = e.detail.id)}>
                <div class="overview">
                  <lcm-card heading="Betriebsfähigkeit" subtitle="Kann LCM auf diesem Server arbeiten?">
                    <lcm-button slot="actions" size="small" variant="outline" icon="refresh">Alles prüfen</lcm-button>
                    <div class="health">${HEALTH.map((h) => this.renderHealth(h))}</div>
                  </lcm-card>
                  <div class="detail">
                    <lcm-card heading="Zustand">
                      <dl class="kv">
                        <dt>Updates</dt><dd><span class="big">27</span> <lcm-badge tone="crit">3 Sicherheit</lcm-badge></dd>
                        <dt>Speicher /</dt><dd><lcm-meter value="88" label="Speicher auf / belegt"></lcm-meter></dd>
                        <dt>Speicher /var</dt><dd><lcm-meter value="54" label="Speicher auf /var belegt"></lcm-meter></dd>
                        <dt>Neustart</dt><dd>nicht nötig</dd>
                      </dl>
                    </lcm-card>
                    <lcm-card heading="Letzte Jobs">
                      <dl class="kv">
                        <dt><lcm-status level="crit" label="fehlgeschlagen"></lcm-status></dt><dd>Paketlisten aktualisieren · vor 41 Min.</dd>
                        <dt><lcm-status level="great" label="erfolgreich"></lcm-status></dt><dd>CVE-Scan · vor 26 Min.</dd>
                        <dt><lcm-status level="great" label="erfolgreich"></lcm-status></dt><dd>Updates einspielen · vor 3 Tagen</dd>
                      </dl>
                    </lcm-card>
                  </div>
                </div>
              </lcm-tabs>
            </div>
          </div>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-server', CatServer);
