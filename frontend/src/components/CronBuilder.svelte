<script>
  // Baukasten für Zeitpläne: „jeden Dienstag um 03:00“ statt „0 3 * * 2“.
  //
  // Der Cron-Ausdruck bleibt die Wahrheit - er steht weiterhin im Feld
  // darunter und ist frei editierbar. Dieser Baukasten schreibt ihn nur, und
  // liest ihn zurück, solange er einem der geläufigen Muster entspricht.
  // Alles Feinere (mehrere Uhrzeiten, Monatsauswahl, Schrittweiten in
  // Wochentagen) bleibt dem Ausdruck vorbehalten: Der Baukasten würde daran
  // scheitern und stellt in dem Fall auf „eigener Ausdruck“.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let { value = $bindable('0 3 * * *') } = $props();

  const WEEKDAYS = [1, 2, 3, 4, 5, 6, 0]; // Montag zuerst, Sonntag = 0 (Cron)

  let mode = $state('daily'); // daily | weekly | monthly | hours | minutes | custom
  let time = $state('03:00');
  let minute = $state(0); // nur für den Stunden-Takt
  let weekdays = $state([2]);
  let dayOfMonth = $state(1);
  let every = $state(15);

  // Zuletzt selbst erzeugter Ausdruck: Nur was von außen kommt (Dialog wird
  // mit einem bestehenden Zeitplan geöffnet, jemand tippt im Feld) darf den
  // Baukasten neu stellen - sonst überschriebe er die eigene Eingabe.
  let emitted = '';

  const pad = (n) => String(n).padStart(2, '0');
  const isNum = (s, max) => /^\d+$/.test(s) && Number(s) <= max;

  /** Cron-Ausdruck → Baukasten-Zustand; null, wenn kein Muster passt. */
  function parse(expr) {
    const f = String(expr ?? '').trim().split(/\s+/);
    if (f.length !== 5) return null;
    const [m, h, dom, mon, dow] = f;
    if (mon !== '*') return null;
    const step = /^\*\/(\d+)$/;
    if (step.test(m) && h === '*' && dom === '*' && dow === '*') {
      return { mode: 'minutes', every: Number(m.match(step)[1]) };
    }
    if (isNum(m, 59) && step.test(h) && dom === '*' && dow === '*') {
      return { mode: 'hours', every: Number(h.match(step)[1]), minute: Number(m) };
    }
    if (!isNum(m, 59) || !isNum(h, 23)) return null;
    const at = { time: `${pad(Number(h))}:${pad(Number(m))}` };
    if (dom === '*' && dow === '*') return { mode: 'daily', ...at };
    if (dom === '*' && /^[0-6](,[0-6])*$/.test(dow)) {
      return { mode: 'weekly', ...at, weekdays: dow.split(',').map(Number) };
    }
    if (dow === '*' && isNum(dom, 31) && Number(dom) >= 1) {
      return { mode: 'monthly', ...at, dayOfMonth: Number(dom) };
    }
    return null;
  }

  /** Baukasten-Zustand → Cron-Ausdruck. */
  function build() {
    const [h, m] = time.split(':').map(Number);
    switch (mode) {
      case 'minutes':
        return `*/${every} * * * *`;
      case 'hours':
        return `${minute} */${every} * * *`;
      case 'weekly':
        return `${m} ${h} * * ${[...weekdays].sort().join(',')}`;
      case 'monthly':
        return `${m} ${h} ${dayOfMonth} * *`;
      default:
        return `${m} ${h} * * *`;
    }
  }

  function emit() {
    if (mode === 'custom') return;
    if (mode === 'weekly' && weekdays.length === 0) return; // ohne Tag kein Plan
    emitted = build();
    value = emitted;
  }

  function toggleDay(d) {
    weekdays = weekdays.includes(d) ? weekdays.filter((x) => x !== d) : [...weekdays, d];
    emit();
  }

  $effect(() => {
    const incoming = value;
    if (incoming === emitted) return;
    const p = parse(incoming);
    if (!p) {
      mode = 'custom';
      return;
    }
    mode = p.mode;
    if (p.time) time = p.time;
    if (p.minute !== undefined) minute = p.minute;
    if (p.weekdays) weekdays = p.weekdays;
    if (p.dayOfMonth) dayOfMonth = p.dayOfMonth;
    if (p.every) every = p.every;
    emitted = incoming;
  });
</script>

<div class="row g-2 mb-2" data-testid="cron-builder">
  <div class="col-12 col-sm-5">
    <label class="form-label small mb-1" for="cb-mode">{t('cron.repeat')}</label>
    <select id="cb-mode" class="form-select" bind:value={mode} onchange={emit} data-testid="cron-mode">
      <option value="minutes">{t('cron.modes.minutes')}</option>
      <option value="hours">{t('cron.modes.hours')}</option>
      <option value="daily">{t('cron.modes.daily')}</option>
      <option value="weekly">{t('cron.modes.weekly')}</option>
      <option value="monthly">{t('cron.modes.monthly')}</option>
      <option value="custom">{t('cron.modes.custom')}</option>
    </select>
  </div>

  {#if mode === 'minutes' || mode === 'hours'}
    <div class="col-6 col-sm-3">
      <label class="form-label small mb-1" for="cb-every">{t('cron.every')}</label>
      <input id="cb-every" class="form-control" type="number" min="1" max={mode === 'minutes' ? 59 : 23}
        bind:value={every} oninput={emit} data-testid="cron-every" />
    </div>
  {/if}

  {#if mode === 'hours'}
    <div class="col-6 col-sm-4">
      <label class="form-label small mb-1" for="cb-minute">{t('cron.minute')}</label>
      <input id="cb-minute" class="form-control" type="number" min="0" max="59"
        bind:value={minute} oninput={emit} />
    </div>
  {:else if mode !== 'minutes' && mode !== 'custom'}
    <div class="col-6 col-sm-4">
      <label class="form-label small mb-1" for="cb-time">{t('cron.time')}</label>
      <input id="cb-time" class="form-control" type="time" bind:value={time} oninput={emit} data-testid="cron-time" />
    </div>
  {/if}

  {#if mode === 'monthly'}
    <div class="col-6 col-sm-3">
      <label class="form-label small mb-1" for="cb-dom">{t('cron.dayOfMonth')}</label>
      <input id="cb-dom" class="form-control" type="number" min="1" max="31" bind:value={dayOfMonth} oninput={emit} />
    </div>
  {/if}

  {#if mode === 'weekly'}
    <div class="col-12">
      <span class="form-label small mb-1 d-block">{t('cron.weekdays')}</span>
      <div class="btn-group flex-wrap" role="group" aria-label={t('cron.weekdays')}>
        {#each WEEKDAYS as d (d)}
          <button type="button" data-testid="cron-day-{d}"
            class="btn btn-sm {weekdays.includes(d) ? 'btn-primary' : 'btn-outline-secondary'}"
            aria-pressed={weekdays.includes(d)} onclick={() => toggleDay(d)}>{t('cron.day.' + d)}</button>
        {/each}
      </div>
      {#if weekdays.length === 0}
        <p class="small text-danger mb-0 mt-1">{t('cron.pickDay')}</p>
      {/if}
    </div>
  {/if}
</div>
