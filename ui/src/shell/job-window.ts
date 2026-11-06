import { LitElement, html, nothing } from 'lit';
import type { JobItem } from '../api/jobs';
import { auth } from '../lib/auth';
import { SubscribeController } from '../lib/controllers';
import { jobWindow } from '../lib/job-window';
import { router } from '../lib/router';

/**
 * <lcm-job-window> – zeigt einen Job als Fenster über der aktuellen Seite (`jobWindow.open`). Es schließt sich mit dem
 * Fenster oder sobald die Seite wechselt, etwa über den Verweis auf den Server. Der Code liegt in eigenen Stücken und kommt
 * erst beim ersten Öffnen.
 */
export class LcmJobWindow extends LitElement {
  static override properties = { _abort: { state: true } };

  declare _abort: JobItem | undefined;
  #path = router.state.path;

  constructor() {
    super();
    this._abort = undefined;
    new SubscribeController(this, (listener) => jobWindow.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new SubscribeController(this, (listener) =>
      router.onChange(() => {
        const moved = router.state.path !== this.#path;
        this.#path = router.state.path;
        if (moved) jobWindow.close();
        listener();
      }),
    );
  }

  override willUpdate(): void {
    if (jobWindow.id !== '' && !auth.isAuthenticated) jobWindow.close();
    if (jobWindow.id === '') return;
    void import('../pages/jobs/jobs-detail');
    void import('../pages/jobs/jobs-abort');
  }

  override render() {
    const id = jobWindow.id;
    if (id === '') return nothing;
    return html`<lcm-jobs-detail
        job-id=${id}
        ?can-abort=${auth.can('servers:write')}
        @lcm-close=${() => jobWindow.close()}
        @lcm-job-abort=${(event: CustomEvent<{ job: JobItem }>) => (this._abort = event.detail.job)}
      ></lcm-jobs-detail>
      <lcm-job-abort-dialog .job=${this._abort} @lcm-close=${() => (this._abort = undefined)}></lcm-job-abort-dialog>`;
  }
}
customElements.define('lcm-job-window', LcmJobWindow);
