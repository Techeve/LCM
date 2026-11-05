import { LitElement, css, html, nothing } from 'lit';
import { repeat } from 'lit/directives/repeat.js';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-switch';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { LOG_LEVEL_URL, logStreamUrl, logsUrl, normaliseLogEntry, normaliseLogLevel, normaliseLogs, settingsMoreApi, type LogEntry, type LogLevel } from '../../api/settings-more';
import { api, ApiError } from '../../lib/api';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, i18n, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import type { SseEvent } from '../../lib/sse';
import { LEVELS, appendLines, formatClock, levelTone, lineText, parseLogFilter, type LogFilter } from './messages/events-model';
import { sectionStyles } from './packages/kit';

// Zeilen, die noch unter dieser Höhe am Ende stehen, gelten als „liest das Ende“: Neue Zeilen scrollen dann mit.
const STICKY_PX = 80;

// Pause, bevor ein beendeter oder unterbrochener Strom neu aufgebaut wird.
const STREAM_RETRY_MS = 2000;

const LEVEL_KEYS: Record<string, string> = {
  '': 'settings.events.level.all',
  debug: 'settings.events.level.debug',
  info: 'settings.events.level.info',
  warn: 'settings.events.level.warn',
  error: 'settings.events.level.error',
};

/** Wartet, endet aber sofort, wenn das Signal abbricht. */
function pause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener('abort', done);
      resolve();
    };
    const timer = setTimeout(done, ms);
    signal.addEventListener('abort', done);
  });
}

/** Einstellungen → Ereignisse: das Log des LCM-Dienstes, auf Wunsch live über einen Ereignisstrom (SSE). */
export class SettingsEvents extends LitElement {
  static override properties = { _lines: { state: true }, _loading: { state: true }, _error: { state: true }, _query: { state: true }, _debugBusy: { state: true } };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .filters {
        display: flex;
        flex-wrap: wrap;
        align-items: flex-end;
        gap: var(--space-3);
      }
      .filters lcm-select {
        inline-size: 11rem;
      }
      .filters lcm-input {
        flex: 1 1 16rem;
      }
      .debug {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
      }
      .log {
        max-block-size: 60vh;
        overflow-y: auto;
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
      .line {
        display: grid;
        grid-template-columns: 6.5rem 4.5rem minmax(0, 1fr);
        gap: var(--space-3);
        align-items: baseline;
        padding: 0.2rem var(--space-3);
        border-block-end: 1px solid var(--line);
      }
      .line:last-child {
        border-block-end: 0;
      }
      .time {
        color: var(--text-2);
        font-variant-numeric: tabular-nums;
      }
      .msg {
        overflow-wrap: anywhere;
      }
      .empty {
        padding: var(--space-4);
        color: var(--text-2);
        font-family: var(--font-sans);
        font-size: var(--fs-s);
      }
      .live {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--ok-text);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .live::before {
        content: '';
        inline-size: 0.5rem;
        block-size: 0.5rem;
        border-radius: 50%;
        background: var(--ok);
        animation: pulse var(--dur-pulse) var(--ease-loop) infinite;
      }
      @keyframes pulse {
        50% {
          opacity: 0.3;
        }
      }
    `,
  ];

  declare _lines: LogEntry[];
  declare _loading: boolean;
  declare _error: string;
  declare _query: string;
  declare _debugBusy: boolean;
  #filterKey = '';
  #stream: AbortController | null = null;
  #loadToken = 0;
  #atEnd = true;
  readonly #level = new LiveData<LogLevel>(this, { url: () => LOG_LEVEL_URL, transform: normaliseLogLevel });

  constructor() {
    super();
    this._lines = [];
    this._loading = true;
    this._error = '';
    this._query = '';
    this._debugBusy = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopStream();
    this.#filterKey = '';
  }

  get #filter(): LogFilter {
    return parseLogFilter(router.state.query);
  }

  override willUpdate(): void {
    const filter = this.#filter;
    const key = JSON.stringify(filter);
    if (key === this.#filterKey) return;
    this.#filterKey = key;
    this._query = filter.query;
    void this.#reload(filter);
  }

  override updated(): void {
    this.#stickToEnd();
  }

  /** Der Strom beginnt am aktuellen Ende der Datei; den Verlauf davor liefert der normale Abruf. */
  async #reload(filter: LogFilter): Promise<void> {
    this.#stopStream();
    const token = ++this.#loadToken;
    this._loading = true;
    this._error = '';
    try {
      const lines = normaliseLogs(await api.get(logsUrl(filter.level, filter.query)));
      if (token !== this.#loadToken) return;
      this.#atEnd = true;
      this._lines = lines;
      if (filter.follow) this.#startStream(filter);
    } catch (error) {
      if (token === this.#loadToken) this._error = errorText(error);
    } finally {
      if (token === this.#loadToken) this._loading = false;
    }
  }

  /**
   * Folgt dem Log. Per fetch statt `EventSource`: Das kann keine Kopfzeile setzen und trüge die
   * Sitzung der Arbeitsspeicher-Variante nicht mit.
   */
  #startStream(filter: LogFilter): void {
    const stream = new AbortController();
    this.#stream = stream;
    void this.#follow(filter, stream.signal);
  }

  /** Ein Strom endet nach spätestens 30 Minuten oder bei einer Unterbrechung: dann neu verbinden. Lehnt der Server ab, ist Schluss. */
  async #follow(filter: LogFilter, signal: AbortSignal): Promise<void> {
    while (!signal.aborted) {
      try {
        await api.stream(logStreamUrl(filter.level, filter.query), (event) => this.#onLogEvent(event), { signal });
      } catch (error) {
        if (signal.aborted) return;
        if (!(error instanceof ApiError && error.code === 'network')) {
          toast({ tone: 'warn', title: t('settings.events.streamClosed') });
          router.setQuery({ follow: null }, { replace: true });
          return;
        }
      }
      await pause(STREAM_RETRY_MS, signal);
    }
  }

  #onLogEvent(event: SseEvent): void {
    if (event.event !== 'log') return;
    try {
      this.#atEnd = this.#isAtEnd();
      this._lines = appendLines(this._lines, [normaliseLogEntry(JSON.parse(event.data))]);
    } catch {
      // Eine unlesbare Zeile übergehen, statt die Ansicht zu verlieren.
    }
  }

  #stopStream(): void {
    this.#stream?.abort();
    this.#stream = null;
  }

  #isAtEnd(): boolean {
    const log = this.renderRoot.querySelector<HTMLElement>('.log');
    return !log || log.scrollHeight - log.scrollTop - log.clientHeight < STICKY_PX;
  }

  /** Neue Zeilen scrollen nur mit, wenn man ohnehin am Ende liest; wer nach oben gescrollt hat, liest etwas. */
  #stickToEnd(): void {
    if (!this.#atEnd) return;
    const log = this.renderRoot.querySelector<HTMLElement>('.log');
    if (log) log.scrollTop = log.scrollHeight;
  }

  async #toggleDebug(on: boolean): Promise<void> {
    this._debugBusy = true;
    try {
      await settingsMoreApi.setDebug(on);
      toast({ tone: on ? 'warn' : 'ok', title: t(on ? 'settings.events.debug.started' : 'settings.events.debug.stopped') });
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.events.debug.failed'), message: errorText(error) });
    } finally {
      this._debugBusy = false;
    }
  }

  #filters() {
    const filter = this.#filter;
    return html`<div class="filters">
      <lcm-select
        label=${t('settings.events.levelLabel')}
        width="full"
        .options=${['', ...LEVELS].map((level) => ({ value: level, label: t(LEVEL_KEYS[level]) }))}
        .value=${filter.level}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => router.setQuery({ level: e.detail.value || null }, { replace: true })}
      ></lcm-select>
      <lcm-input maxlength=${LIMITS.query}
        label=${t('settings.events.search')}
        width="full"
        icon="search"
        placeholder=${t('settings.events.searchPlaceholder')}
        .value=${this._query}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._query = e.detail.value)}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => router.setQuery({ q: e.detail.value.trim() || null }, { replace: true })}
      ></lcm-input>
      <lcm-button icon="refresh" ?busy=${this._loading} @click=${() => void this.#reload(filter)}>${t('settings.events.reload')}</lcm-button>
      <lcm-button
        variant=${filter.follow ? 'primary' : 'secondary'}
        icon=${filter.follow ? 'x' : 'play'}
        @click=${() => router.setQuery({ follow: filter.follow ? null : '1' }, { replace: true })}
        >${t(filter.follow ? 'settings.events.stop' : 'settings.events.follow')}</lcm-button
      >
      ${filter.follow ? html`<span class="live" role="status">${t('settings.events.live')}</span>` : nothing}
    </div>`;
  }

  #debugCard() {
    const level = this.#level.data;
    const debug = level?.level === 'debug';
    return html`<lcm-card level="2" heading=${t('settings.events.debug.title')}>
      <div class="debug">
        <lcm-switch
          label=${t('settings.events.debug.label')}
          on-label=${t('settings.more.switch.on')}
          off-label=${t('settings.more.switch.off')}
          on-text=${t('settings.events.debug.on')}
          off-text=${t('settings.events.debug.off')}
          .checked=${debug}
          ?disabled=${!level || this._debugBusy}
          @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => void this.#toggleDebug(e.detail.checked)}
        ></lcm-switch>
        ${debug && level?.debug_until ? html`<lcm-badge tone="warn">${t('settings.events.debug.until', { time: formatClock(level.debug_until, i18n.language) })}</lcm-badge>` : nothing}
      </div>
    </lcm-card>`;
  }

  #log() {
    if (this._error) return html`<p class="callout warn">${this._error}</p>`;
    return html`<div class="log" role="log" tabindex="0" aria-live="off" aria-label=${t('settings.events.logLabel')} @scroll=${() => (this.#atEnd = this.#isAtEnd())}>
      ${this._lines.length === 0 && !this._loading
        ? html`<div class="empty">${t('settings.events.empty')}</div>`
        : repeat(
            this._lines,
            (_, index) => index,
            (line) => html`<div class="line" title=${line.raw}>
              <span class="time">${formatClock(line.time, i18n.language)}</span>
              <span><lcm-badge tone=${levelTone(line.level)}>${line.level || '–'}</lcm-badge></span>
              <span class="msg">${lineText(line)}</span>
            </div>`,
          )}
    </div>`;
  }

  override render() {
    return html`<div class="main full">
      <p class="lead">${t('settings.events.lead')}</p>
      <lcm-card level="2" heading=${t('settings.events.title')}>${this.#filters()}</lcm-card>
      ${this.#log()} ${this.#debugCard()}
    </div>`;
  }
}
customElements.define('lcm-settings-events', SettingsEvents);
