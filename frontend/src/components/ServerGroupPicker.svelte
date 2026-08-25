<script>
  // Auswahl von Servergruppen für eine Alarmregel.
  //
  // Zwei Dinge macht die Auswahl ausdrücklich, die eine bloße Ankreuzliste
  // nicht kann: Sie unterscheidet „gilt für alle Server" von „gilt für diese
  // Gruppen" (vorher war „alle" nur das Fehlen jeder Auswahl - nicht erkennbar
  // als Entscheidung), und sie bleibt bei vielen Gruppen benutzbar: gesucht
  // wird getippt, Gewähltes steht als Pille mit Kreuz darüber.
  import { i18n } from '../stores/i18n.svelte.js';
  const t = (k, p) => i18n.t(k, p);

  let { groups = [], selected = $bindable([]) } = $props();

  // Eigene id-Basis: Die beiden Radios brauchen eindeutige ids, sonst
  // schalten zwei Auswahlfelder auf derselben Seite einander um.
  const uid = $props.id();

  // Beim Öffnen aus dem Bestand ableiten; danach steuert die Auswahl der Modus.
  let scoped = $state(selected.length > 0);
  let query = $state('');
  let listOpen = $state(false);
  let inputEl = $state(null);

  let chosen = $derived(groups.filter((g) => selected.includes(g.id)));
  let matches = $derived(
    groups
      .filter((g) => !selected.includes(g.id))
      .filter((g) => g.name.toLowerCase().includes(query.trim().toLowerCase()))
      .slice(0, 8),
  );

  function setScoped(value) {
    scoped = value;
    // „Alle Server" heißt: keine Einschränkung. Eine stehengebliebene Auswahl
    // würde das stillschweigend widerlegen.
    if (!value) selected = [];
  }

  function add(group) {
    selected = [...selected, group.id];
    query = '';
    inputEl?.focus();
  }

  function remove(id) {
    selected = selected.filter((x) => x !== id);
  }

  function onKey(e) {
    if (e.key === 'Escape') {
      listOpen = false;
    } else if (e.key === 'Enter' && matches.length > 0) {
      e.preventDefault();
      add(matches[0]);
    }
  }
</script>

<div data-testid="group-picker">
  <div class="btn-group btn-group-sm mb-2" role="group">
    <input type="radio" class="btn-check" id="{uid}-all" name="{uid}-scope" checked={!scoped}
      onchange={() => setScoped(false)} />
    <label class="btn btn-outline-secondary" for="{uid}-all"
      data-testid="group-scope-all" aria-pressed={!scoped}>{t('groupPicker.allServers')}</label>
    <input type="radio" class="btn-check" id="{uid}-scoped" name="{uid}-scope" checked={scoped}
      onchange={() => setScoped(true)} />
    <label class="btn btn-outline-secondary" for="{uid}-scoped"
      data-testid="group-scope-selected" aria-pressed={scoped}>{t('groupPicker.selectedGroups')}</label>
  </div>

  {#if scoped}
    {#if chosen.length > 0}
      <div class="d-flex flex-wrap gap-1 mb-2" data-testid="group-chips">
        {#each chosen as g (g.id)}
          <span class="badge text-bg-primary d-inline-flex align-items-center gap-1">
            {g.name}
            <button type="button" class="btn-close btn-close-white"
              style="font-size: 0.55rem" data-testid="group-chip-remove-{g.id}"
              aria-label={t('groupPicker.remove', { name: g.name })}
              onclick={() => remove(g.id)}></button>
          </span>
        {/each}
      </div>
    {/if}

    <div class="position-relative">
      <input class="form-control form-control-sm" type="search" bind:this={inputEl}
        bind:value={query} placeholder={t('groupPicker.search')} data-testid="group-search"
        onfocus={() => (listOpen = true)} onkeydown={onKey} />
      {#if listOpen}
        <!-- Klick außerhalb: onblur mit kurzer Verzögerung würde die Auswahl
             per Maus verschlucken - deshalb schließt die Liste beim Wählen
             und per Escape. -->
        <ul class="list-group position-absolute w-100 shadow-sm mt-1"
          style="z-index: 1080; max-height: 12rem; overflow-y: auto" data-testid="group-options">
          {#each matches as g (g.id)}
            <li class="list-group-item list-group-item-action py-1 px-2 small">
              <button type="button" class="btn btn-link btn-sm p-0 text-decoration-none"
                data-testid="group-option-{g.id}" onclick={() => add(g)}>{g.name}</button>
            </li>
          {:else}
            <li class="list-group-item py-1 px-2 small text-body-secondary">
              {groups.length === 0 ? t('groupPicker.noGroups') : t('groupPicker.noMatches')}
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    {#if chosen.length === 0}
      <p class="small text-danger mb-0 mt-1" data-testid="group-picker-empty">{t('groupPicker.pickOne')}</p>
    {/if}
  {:else}
    <p class="small text-body-secondary mb-0">{t('groupPicker.allServersHint')}</p>
  {/if}
</div>
