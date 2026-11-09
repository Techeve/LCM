import { LitElement, css, html } from 'lit';
import '../../components/lcm-dialog';
import '../../components/lcm-server-action';
import type { IconName } from '../../components/icons';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import type { ServerRef } from '../../api/containers';
import { refreshServer, type StartedJob } from '../../api/servers';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { jobsLink } from '../../lib/job-link';
import { actionLabels } from '../servers/preview';

/** Ergebnis eines Laufs über mehrere Server: wo der Job startete und wo nicht (mit Grund). */
export interface FleetResult {
  started: ServerRef[];
  failed: { server: ServerRef; error: string }[];
}

/** Startet die Aktion nacheinander auf jedem Server; ein Fehler hält die übrigen nicht auf. */
export async function runOnServers(servers: ServerRef[], run: (serverId: number) => Promise<StartedJob>): Promise<FleetResult> {
  const result: FleetResult = { started: [], failed: [] };
  for (const server of servers) {
    try {
      await run(server.id);
      result.started.push(server);
    } catch (error) {
      result.failed.push({ server, error: errorText(error) });
    }
  }
  return result;
}

/**
 * `<lcm-ctr-fleet-action .servers .run label help heading>`: eine Container-Aktion auf allen Servern einer Zeile
 * (Image aktualisieren, Compose-Projekt aktualisieren). Erklärblase, Bestätigung mit den betroffenen Servern,
 * danach je Server ein Job.
 */
export class CtrFleetAction extends LitElement {
  static override properties = {
    servers: { attribute: false },
    run: { attribute: false },
    label: { type: String },
    help: { type: String },
    meaning: { type: String },
    heading: { type: String },
    icon: { type: String },
    _open: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
      }
    `,
  ];

  declare servers: ServerRef[];
  declare run: ((serverId: number) => Promise<StartedJob>) | undefined;
  declare label: string;
  declare help: string;
  declare meaning: string;
  declare heading: string;
  declare icon: IconName | '';
  declare _open: boolean;
  declare _busy: boolean;

  constructor() {
    super();
    this.servers = [];
    this.run = undefined;
    this.label = '';
    this.help = '';
    this.meaning = '';
    this.heading = '';
    this.icon = '';
    this._open = false;
    this._busy = false;
    new I18nController(this);
  }

  get #impacts(): Impact[] {
    return [
      { label: t('serverOps.fleet.servers', { count: this.servers.length }), value: this.servers.map((s) => s.name).join(', ') },
      { label: t('serverOps.fleet.jobs'), value: t('serverOps.fleet.jobsValue', { count: this.servers.length }) },
    ];
  }

  #report(result: FleetResult): void {
    if (result.started.length > 0) {
      toast({
        tone: 'info',
        title: t('serverOps.fleet.started', { label: this.label, count: result.started.length }),
        ...jobsLink(t('serverOps.fleet.showJobs')),
      });
    }
    for (const { server, error } of result.failed) toast({ tone: 'crit', title: t('serverOps.fleet.failed', { label: this.label, name: server.name }), message: error });
  }

  async #start(): Promise<void> {
    if (!this.run) return;
    this._busy = true;
    try {
      const result = await runOnServers(this.servers, this.run);
      for (const server of result.started) refreshServer(server.id);
      this.#report(result);
      this._open = false;
    } finally {
      this._busy = false;
    }
  }

  override render() {
    return html`<lcm-server-action
        variant="outline"
        label=${this.label}
        help=${this.help}
        meaning=${this.meaning}
        icon=${this.icon}
        ?busy=${this._busy && !this._open}
        ?disabled=${this.servers.length === 0}
        .labels=${actionLabels()}
        .preview=${{}}
        @click=${() => (this._open = true)}
      ></lcm-server-action>
      <lcm-dialog
        ?open=${this._open}
        heading=${this.heading || this.label}
        confirm-label=${this.label}
        cancel-label=${t('servers.confirm.cancel')}
        .impacts=${this.#impacts}
        ?busy=${this._busy}
        @lcm-cancel=${() => (this._open = false)}
        @lcm-confirm=${() => void this.#start()}
      ></lcm-dialog>`;
  }
}
customElements.define('lcm-ctr-fleet-action', CtrFleetAction);
