import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { i18n } from './i18n';

/** Rendert das Element neu, sobald die Quelle meldet (Router, Auth, Sprache ...). */
export class SubscribeController implements ReactiveController {
  #host: ReactiveControllerHost;
  #subscribe: (listener: () => void) => () => void;
  #unsubscribe: (() => void) | undefined;

  constructor(host: ReactiveControllerHost, subscribe: (listener: () => void) => () => void) {
    this.#host = host;
    this.#subscribe = subscribe;
    host.addController(this);
  }

  hostConnected(): void {
    this.#unsubscribe = this.#subscribe(() => this.#host.requestUpdate());
  }

  hostDisconnected(): void {
    this.#unsubscribe?.();
    this.#unsubscribe = undefined;
  }
}

/** Für Elemente mit übersetzten Texten oder Formaten: rendert bei Sprachwechsel neu. */
export class I18nController extends SubscribeController {
  constructor(host: ReactiveControllerHost) {
    super(host, (listener) => i18n.onChange(listener));
  }
}
