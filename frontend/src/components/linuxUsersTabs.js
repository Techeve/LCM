// Zentrale Definition der Reiter auf der Linux-Benutzer-Seite (per RBAC
// gefiltert) - die Reiter-Leiste in LinuxUsersLayout rendert daraus.
//
// Berechtigungsprofile und Regelbausteine lagen früher unter „Einstellungen".
// Sie gehören fachlich hierher: Beide beschreiben, was ein Linux-Benutzer auf
// den Servern darf - und wer sie pflegt, kommt von der Benutzerliste.
import { auth } from '../stores/auth.svelte.js';

const tabs = [
  { path: '/linux-users', labelKey: 'linuxUsers.tabs.users', show: () => auth.can('linuxusers:read') },
  { path: '/linux-users/profiles', labelKey: 'linuxUsers.tabs.profiles', show: () => auth.can('profiles:read') },
  { path: '/linux-users/profile-blocks', labelKey: 'linuxUsers.tabs.blocks', show: () => auth.can('profiles:read') },
];

// Die aktuell sichtbaren Reiter (RBAC des eingeloggten Users). Ein
// Verwaltungs-User hat weder linuxusers:read noch profiles:read und sieht
// damit gar keinen - für ihn ist die Seite als Ganzes nicht erreichbar.
export function visibleLinuxUsersTabs() {
  return tabs.filter((i) => i.show());
}
