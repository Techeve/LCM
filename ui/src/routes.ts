// Routentabelle. Die Reihenfolge entscheidet: Besonderes vor Allgemeinem.
// Seiten werden erst beim Aufruf geladen (`{ path, tag, load }`). Unbekannte Adressen
// zeigt die Shell als „nicht gefunden“.
//
// Eine Seite ist ein Custom Element ohne Eigenschaften von außen. Sie liest Pfadteile aus
// `router.state.match.params` und die Query aus `router.state.query` (und hört mit
// `SubscribeController(this, router.onChange)`; dasselbe Element bleibt beim Wechsel von
// `/servers/1` zu `/servers/2` stehen). Berechtigung und Seitentitel stehen in
// `lib/navigation.ts`. Befehle für die Palette: `palette.register(this, [...])`, Ungespeichertes:
// `unsaved.set(this, true|false)`, Speichern: `document.addEventListener('lcm-save', e => { e.preventDefault(); ... })`.

import { auth } from './lib/auth';
import { visibleSettings } from './lib/navigation';
import type { Route } from './lib/route-match';
import { serverPathByName } from './lib/server-lookup';

function under(base: string, rest: string): string {
  return rest ? `${base}/${rest}` : base;
}

/** `/settings` führt zum ersten Abschnitt, den der Benutzer sehen darf. */
function settingsHome(): string {
  const first = visibleSettings((permission) => auth.can(permission))[0]?.sections[0];
  return first ? `/settings/${first.id}` : '/';
}

export const routes: Route[] = [
  // alte Adressen, damit Lesezeichen weiter funktionieren
  { path: '/docker/*', redirect: ({ '*': rest }) => under('/containers', rest) },
  { path: '/doku/*', redirect: ({ '*': rest }) => under('/docs', rest), public: true },
  { path: '/linux-aktivierung/*', redirect: ({ '*': rest }) => under('/linux-activate', rest), public: true },
  { path: '/aktivierung/*', redirect: ({ '*': rest }) => under('/activate', rest), public: true },
  { path: '/servers/by-name/:name', redirect: ({ name }) => serverPathByName(name) },
  { path: '/settings/profiles', redirect: () => '/linux-users/profiles' },
  { path: '/settings/profile-blocks', redirect: () => '/linux-users/profile-blocks' },
  { path: '/settings', redirect: settingsHome },

  { path: '/', tag: 'lcm-page-dashboard', load: () => import('./pages/dashboard/dashboard-page') },
  { path: '/servers', tag: 'lcm-page-servers', load: () => import('./pages/servers/servers-page') },
  { path: '/servers/join', tag: 'lcm-page-server-join', load: () => import('./pages/servers/join/join-page') },
  { path: '/servers/:id/:tab?', tag: 'lcm-page-servers', load: () => import('./pages/servers/servers-page') },
  { path: '/groups', tag: 'lcm-page-groups', load: () => import('./pages/groups/groups-page') },
  { path: '/groups/:id/:tab?', tag: 'lcm-page-groups', load: () => import('./pages/groups/groups-page') },
  { path: '/linux-users/:section?', tag: 'lcm-page-linux-users', load: () => import('./pages/linux-users/linux-users-page') },
  { path: '/security/:tab?', tag: 'lcm-page-security', load: () => import('./pages/security/security-page') },
  { path: '/containers/*', tag: 'lcm-page-containers', load: () => import('./pages/containers/containers-page') },
  { path: '/jobs/:id?', tag: 'lcm-page-jobs', load: () => import('./pages/jobs/jobs-page') },
  { path: '/settings/:section', tag: 'lcm-page-settings', load: () => import('./pages/settings-page') },
  { path: '/account/:section?', tag: 'lcm-page-account', load: () => import('./pages/account/account-page') },

  { path: '/login', tag: 'lcm-page-login', load: () => import('./pages/login-page'), public: true },
  { path: '/docs/*', tag: 'lcm-page-docs', load: () => import('./pages/docs/docs-page'), public: true },
  { path: '/activate/*', tag: 'lcm-page-activate', load: () => import('./pages/public/activate-page'), public: true },
  { path: '/linux-activate/*', tag: 'lcm-page-linux-activate', load: () => import('./pages/public/linux-activate-page'), public: true },
  { path: '/password-reset/*', tag: 'lcm-page-activate', load: () => import('./pages/public/activate-page'), public: true },
];
