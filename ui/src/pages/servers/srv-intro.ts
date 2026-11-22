import { LitElement, html, nothing } from 'lit';
import { toast } from '../../components/lcm-toaster';
import { isApiDevice, serverPath, visibleTabs, type ServerItem } from '../../api/servers';
import { lcmHostMode, lcmHostUrl, normaliseLcmHost, type LcmHostStatus } from '../../api/server-detail';
import { settingsApi } from '../../api/settings';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { closeIntro } from '../setup/setup-model';
import { introSteps, type IntroStepDef, type IntroView } from './intro-steps';
import './ov-lcm-host-tour';
import type { TourStep } from './ov-lcm-host-tour';

/** Die Adresse, unter der eine Ansicht steht: der Reiter, und für die Einstellungen das offene Fenster auf seiner Seite. */
export function viewPath(serverId: number, view: IntroView): string {
  const path = serverPath(serverId, view.tab);
  return view.section ? `${path}?modal=settings&section=${view.section}` : path;
}

/**
 * `<lcm-srv-intro .server>`: die Einführung auf der Seite des LCM-Hosts, solange der Einrichtungsassistent bei `intro`
 * steht. Sie lebt auf der Serverseite, nicht in einem Reiter, weil sie Reiter wechselt und das Einstellungsfenster öffnet.
 * Je Schritt navigiert sie dorthin und lässt das Fenster (`lcm-srv-lcm-tour`) erklären und hervorheben; Aktionen führt sie
 * nicht aus, die stehen auf den Karten der Seite. Schließen setzt den Stand auf erledigt; sie kommt nicht wieder.
 */
export class SrvIntro extends LitElement {
  static override properties = {
    server: { attribute: false },
    _step: { state: true },
  };

  declare server: ServerItem;
  declare _step: number;

  readonly #status = new LiveData<LcmHostStatus>(this, {
    url: () => lcmHostUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normaliseLcmHost,
  });

  constructor() {
    super();
    this._step = 0;
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  /** Erst wenn der Zustand des Hosts bekannt ist (oder nicht zu holen war): Er entscheidet, ob Trivy und apt-cacher-ng drankommen. */
  get #steps(): IntroStepDef[] | null {
    const { data, error } = this.#status;
    if (!data && !error) return null;
    const server = this.server;
    const can = auth.can.bind(auth);
    return introSteps({
      hostCards: !!data && lcmHostMode(data) === 'setup',
      write: auth.can('servers:write'),
      device: isApiDevice(server),
      proxmox: !!server.proxmox_type,
      tabs: visibleTabs(server, can),
    });
  }

  #go(event: CustomEvent<{ step: number }>, steps: readonly IntroStepDef[]): void {
    const step = Math.max(0, Math.min(event.detail.step, steps.length - 1));
    this._step = step;
    const view = steps[step]!.view;
    void router.navigate(viewPath(this.server.id, view), { replace: true });
  }

  async #close(): Promise<void> {
    try {
      await closeIntro(settingsApi);
      router.setQuery({ modal: null, section: null }, { replace: true });
      await auth.refresh();
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    }
  }

  override render() {
    const defs = this.#steps;
    if (!defs) return nothing;
    const step = Math.min(this._step, defs.length - 1);
    const steps: TourStep[] = defs.map((def) => ({
      key: def.key,
      title: t(def.titleKey),
      text: t(def.textKey),
      badge: def.badgeKey ? t(def.badgeKey) : undefined,
      targets: def.targets,
    }));
    return html`<lcm-srv-lcm-tour .steps=${steps} .step=${step} @lcm-tour-step=${(event: CustomEvent<{ step: number }>) => this.#go(event, defs)} @lcm-tour-close=${() => void this.#close()}></lcm-srv-lcm-tour>`;
  }
}
customElements.define('lcm-srv-intro', SrvIntro);
