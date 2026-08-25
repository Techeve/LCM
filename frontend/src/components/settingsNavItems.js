// Zentrale Definition der Einstellungs-Unterseiten (per RBAC gefiltert).
// Gemeinsam genutzt von der Unter-Navigation (SettingsNav), der
// /settings-Weiterleitung und der Navbar (Sichtbarkeit des Menüpunkts).
// „Mein Konto" (Profil, Passwort, 2FA) ist KEINE Einstellungs-Seite mehr -
// sie ist über den Usernamen in der Navbar erreichbar (/account).
// Berechtigungsprofile und Regelbausteine ebenfalls nicht mehr - sie sind
// Reiter auf der Linux-Benutzer-Seite (linuxUsersTabs.js).
import { auth } from '../stores/auth.svelte.js';

// labelKey verweist auf den i18n-Schlüssel (settings.nav.*); die Beschriftung
// wird erst beim Rendern übersetzt, damit ein Sprachwechsel sofort greift.
export const settingsItems = [
  { path: '/settings/users', labelKey: 'settings.nav.users', show: () => auth.can('users:read') },
  { path: '/settings/apikeys', labelKey: 'settings.nav.apikeys', show: () => auth.can('apikeys:manage') },
  { path: '/settings/mcp', labelKey: 'settings.nav.mcp', show: () => auth.can('settings:manage') },
  { path: '/settings/general', labelKey: 'settings.nav.general', show: () => auth.can('settings:manage') },
  { path: '/settings/security', labelKey: 'settings.nav.security', show: () => auth.can('settings:manage') },
  { path: '/settings/repositories', labelKey: 'settings.nav.repositories', show: () => auth.can('settings:manage') },
  { path: '/settings/apps', labelKey: 'settings.nav.apps', show: () => auth.can('settings:manage') },
  { path: '/settings/apt-cache', labelKey: 'settings.nav.aptCache', show: () => auth.can('settings:manage') },
  { path: '/settings/dns', labelKey: 'settings.nav.dns', show: () => auth.can('settings:manage') },
  { path: '/settings/time', labelKey: 'settings.nav.time', show: () => auth.can('settings:manage') },
  { path: '/settings/crowdsec', labelKey: 'settings.nav.crowdsec', show: () => auth.can('settings:manage') },
  { path: '/settings/allowlists', labelKey: 'settings.nav.allowlists', show: () => auth.can('settings:manage') },
  { path: '/settings/subscription', labelKey: 'settings.nav.subscription', show: () => auth.can('settings:manage') },
  { path: '/settings/backups', labelKey: 'settings.nav.backups', show: () => auth.can('backups:manage') },
  { path: '/settings/schedules', labelKey: 'settings.nav.schedules', show: () => auth.can('rules:manage') },
  { path: '/settings/custom-actions', labelKey: 'settings.nav.customActions', show: () => auth.can('rules:manage') },
  { path: '/settings/notifications', labelKey: 'settings.nav.notifications', show: () => auth.can('notifications:manage') },
  { path: '/settings/alerts', labelKey: 'settings.nav.alerts', show: () => auth.can('alerts:manage') },
];

// Die aktuell sichtbaren Einträge (RBAC des eingeloggten Users).
export function visibleSettingsItems() {
  return settingsItems.filter((i) => i.show());
}
