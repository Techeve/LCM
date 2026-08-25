<script>
  // Responsive Navigationsleiste: ab < lg klappt das Menü hinter einen
  // Burger-Button (Toggle über Svelte-State, ohne Bootstrap-JS - Zero-Bloat).
  // Oben rechts bündelt eine Konto-Pille (User-Icon) ein Dropdown mit
  // Username, Farbmodus-Auswahl, „Mein Konto" und „Abmelden" - spart Platz
  // bei mittlerer Auflösung.
  import { link, router } from 'svelte-spa-router';
  import { api } from '../api';
  import { auth } from '../stores/auth.svelte.js';
  import { theme } from '../stores/theme.svelte.js';
  import { i18n, LOCALES } from '../stores/i18n.svelte.js';
  import { visibleSettingsItems } from './settingsNavItems.js';

  const t = (k, p) => i18n.t(k, p);

  // Nächste Sprache durchschalten (aktuell DE ↔ EN).
  function cycleLocale() {
    const i = LOCALES.indexOf(i18n.locale);
    i18n.set(LOCALES[(i + 1) % LOCALES.length]);
  }

  let open = $state(false); // mobiles Burger-Menü
  let userMenu = $state(false); // Konto-Dropdown oben rechts
  let userMenuEl; // Wurzel des Dropdowns (für Klick-außerhalb)

  // Farbmodus-Icons als Inline-SVG (currentColor). Reihenfolge: Hell (Sonne),
  // Dunkel (Mond), Automatik (Halbkreis). Nur Icon - Text erscheint per title
  // (Tooltip) beim Hovern.
  const sunSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>`;
  const moonSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`;
  const halfSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none"/></svg>`;
  const themeOptions = [
    { value: 'light', svg: sunSvg },
    { value: 'dark', svg: moonSvg },
    { value: 'auto', svg: halfSvg },
  ];

  // Sprach-Flaggen als Inline-SVG (rendern überall, anders als Flag-Emojis).
  const deFlag = `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="14" viewBox="0 0 5 3" class="rounded" preserveAspectRatio="none"><rect width="5" height="3" fill="#000"/><rect width="5" height="2" y="1" fill="#D00"/><rect width="5" height="1" y="2" fill="#FFCE00"/></svg>`;
  const enFlag = `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="14" viewBox="0 0 60 30" class="rounded" preserveAspectRatio="none"><clipPath id="ukS"><rect width="60" height="30"/></clipPath><clipPath id="ukT"><path d="M30,15 h30 v15 z v-15 h-30 z h-30 v-15 z v15 h30 z"/></clipPath><g clip-path="url(#ukS)"><rect width="60" height="30" fill="#012169"/><path d="M0,0 L60,30 M60,0 L0,30" stroke="#fff" stroke-width="6"/><path d="M0,0 L60,30 M60,0 L0,30" clip-path="url(#ukT)" stroke="#C8102E" stroke-width="4"/><path d="M30,0 V30 M0,15 H60" stroke="#fff" stroke-width="10"/><path d="M30,0 V30 M0,15 H60" stroke="#C8102E" stroke-width="6"/></g></svg>`;
  const langOptions = [
    { value: 'de', svg: deFlag },
    { value: 'en', svg: enFlag },
  ];

  // Aktuell anzuzeigendes Icon/Flagge (die eine Schaltfläche zeigt den
  // aktuellen Zustand und schaltet beim Klick weiter).
  let currentThemeSvg = $derived(themeOptions.find((o) => o.value === theme.mode)?.svg ?? '');
  let currentLangSvg = $derived(langOptions.find((o) => o.value === i18n.locale)?.svg ?? '');

  // Konto-Pille: sauberes User-Icon + Chevron statt Emoji.
  const userSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>`;

  let displayName = $derived(
    auth.user ? auth.user.display_name || auth.user.username : '',
  );

  function isActive(path) {
    if (path === '/') return router.location === '/' ? 'active' : '';
    return router.location === path || router.location.startsWith(path + '/') ? 'active' : '';
  }

  // Nach Klick auf einen Menüpunkt beide Menüs schließen.
  function nav() {
    open = false;
    userMenu = false;
  }

  function logout() {
    nav();
    api.auth.logout();
  }

  // Klick außerhalb schließt das Konto-Dropdown (Capture-Phase, damit der
  // Öffnen-Klick auf die Pille selbst nicht sofort wieder schließt).
  function onDocClick(e) {
    if (userMenuEl && !userMenuEl.contains(e.target)) userMenu = false;
  }
  $effect(() => {
    if (!userMenu) return;
    document.addEventListener('click', onDocClick, true);
    return () => document.removeEventListener('click', onDocClick, true);
  });
</script>

<nav class="navbar navbar-expand-lg bg-dark navbar-dark mb-4">
  <div class="container">
    <a class="navbar-brand d-inline-flex align-items-center" href="/" use:link onclick={nav}>
      <img src="/logo-wordmark.svg" alt="LCM" height="30" width="81" />
    </a>

    <button
      class="navbar-toggler"
      type="button"
      aria-label={t('nav.toggleMenu')}
      aria-expanded={open}
      onclick={() => (open = !open)}
    >
      <span class="navbar-toggler-icon"></span>
    </button>

    <div class="collapse navbar-collapse {open ? 'show' : ''}">
      <ul class="navbar-nav me-auto">
        {#if auth.can('servers:read')}
          <li class="nav-item"><a class="nav-link {isActive('/')}" href="/" use:link onclick={nav}>{t('nav.dashboard')}</a></li>
        {/if}
        {#if auth.can('groups:read')}
          <li class="nav-item"><a class="nav-link {isActive('/groups')}" href="/groups" use:link onclick={nav}>{t('nav.groups')}</a></li>
        {/if}
        {#if auth.can('linuxusers:read')}
          <li class="nav-item"><a class="nav-link {isActive('/linux-users')}" href="/linux-users" use:link onclick={nav}>{t('nav.linuxUsers')}</a></li>
        {/if}
        {#if auth.can('packages:read')}
          <li class="nav-item"><a class="nav-link {isActive('/security')}" href="/security" use:link onclick={nav}>{t('nav.security')}</a></li>
          <li class="nav-item"><a class="nav-link {isActive('/docker')}" href="/docker" use:link onclick={nav}>{t('nav.docker')}</a></li>
        {/if}
        {#if auth.can('jobs:read')}
          <li class="nav-item"><a class="nav-link {isActive('/jobs')}" href="/jobs" use:link onclick={nav}>{t('nav.jobs')}</a></li>
        {/if}
        {#if auth.isLoggedIn && visibleSettingsItems().length}
          <li class="nav-item"><a class="nav-link {isActive('/settings')}" href="/settings" use:link onclick={nav}>{t('nav.settings')}</a></li>
        {/if}
        <!-- Doku ohne Rechteprüfung: die Seiten sind öffentlich (siehe
             App.svelte) und enthalten nichts Vertrauliches. -->
        <li class="nav-item"><a class="nav-link {isActive('/doku')}" href="/doku" use:link onclick={nav}>{t('nav.docs')}</a></li>
      </ul>

      <div class="d-flex align-items-center gap-2 py-2 py-lg-0">
        {#if auth.isLoggedIn}
          <!-- Drei gleich große Icon-Schaltflächen: Farbmodus, Sprache, Konto.
               Farbmodus & Sprache zeigen den aktuellen Zustand und schalten
               beim Klick weiter; Text erscheint als Tooltip (title). -->
          <button
            type="button"
            class="btn btn-outline-light btn-sm d-inline-flex align-items-center justify-content-center p-0"
            style="width: 38px; height: 31px;"
            data-testid="theme-toggle"
            aria-label={t('theme.label') + ': ' + t('theme.' + theme.mode)}
            title={t('theme.label') + ': ' + t('theme.' + theme.mode)}
            onclick={() => theme.cycle()}
          >{@html currentThemeSvg}</button>

          <button
            type="button"
            class="btn btn-outline-light btn-sm d-inline-flex align-items-center justify-content-center p-0"
            style="width: 38px; height: 31px;"
            data-testid="lang-toggle"
            aria-label={t('lang.label') + ': ' + t('lang.' + i18n.locale)}
            title={t('lang.label') + ': ' + t('lang.' + i18n.locale)}
            onclick={cycleLocale}
          >{@html currentLangSvg}</button>

          <!-- Konto: gleiche Größe/Design; öffnet das Dropdown mit Username,
               „Mein Konto" und „Abmelden". -->
          <div class="dropdown" bind:this={userMenuEl}>
            <button
              class="btn btn-outline-light btn-sm d-inline-flex align-items-center justify-content-center p-0"
              style="width: 38px; height: 31px;"
              data-testid="current-user"
              aria-haspopup="menu"
              aria-expanded={userMenu}
              aria-label={t('nav.accountMenu', { name: displayName })}
              onclick={() => (userMenu = !userMenu)}
            >
              {@html userSvg}
              <span class="visually-hidden">{displayName}</span>
            </button>

            <!-- Rechtsbündig ohne Bootstrap-JS: dropdown-menu-end greift nur
                 mit Popper ([data-bs-popper]); daher hier per Inline-Position. -->
            <div
              class="dropdown-menu mt-1 {userMenu ? 'show' : ''}"
              style="min-width: 220px; right: 0; left: auto"
            >
              <div class="px-3 py-2">
                <div class="small text-body-secondary">{t('nav.loggedInAs')}</div>
                <div class="fw-semibold text-truncate">{displayName}</div>
              </div>
              <hr class="dropdown-divider my-1" />

              <a class="dropdown-item" href="/account" use:link data-testid="account-link" onclick={nav}>
                {t('nav.account')}
              </a>
              <button class="dropdown-item" onclick={logout}>{t('nav.logout')}</button>
            </div>
          </div>
        {:else}
          <a class="btn btn-info btn-sm" href="/login" use:link data-testid="nav-login" onclick={nav}>{t('nav.login')}</a>
        {/if}
      </div>
    </div>
  </div>
</nav>
