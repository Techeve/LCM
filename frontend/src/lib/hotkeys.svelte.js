// Tastenkürzel der Oberfläche - EIN globaler Tastatur-Handler statt vieler
// verstreuter Listener, damit sich die Kürzel nicht in die Quere kommen und
// an einer Stelle nachlesbar sind (Hilfe-Overlay: „?").
//
// Drei Arten von Kürzeln:
//   - Sprünge: „g" und dann ein Buchstabe (g d = Dashboard, g j = Jobs …).
//     Zwei Tasten nacheinander, damit ein einzelner Buchstabe nirgends
//     versehentlich navigiert.
//   - Seitenkürzel: Schaltflächen und Felder tragen data-hotkey="n" bzw.
//     data-hotkey="/". Der Handler sucht das sichtbare Element mit dieser
//     Taste und klickt es bzw. setzt den Fokus hinein. Die Seite selbst
//     braucht keinen Code dafür - nur das Attribut. Ein Abzeichen am Element
//     (CSS, siehe theme.css) zeigt die Taste direkt am Namen.
//   - Pfeiltasten: In Navigationsleisten (data-arrow-nav) und Tabellen
//     wandert der Fokus mit den Pfeilen, ohne durch jede Schaltfläche
//     tabben zu müssen.
//
// In Eingabefeldern ist alles abgeschaltet außer Strg/Cmd+S (Speichern) -
// wer tippt, will Buchstaben tippen. Escape bleibt den Dialogen überlassen.
import { router } from 'svelte-spa-router';
import { auth } from '../stores/auth.svelte.js';
import { visibleSettingsItems } from '../components/settingsNavItems.js';

const STORAGE_KEY = 'lcm.hotkeys.badges';
const SEQUENCE_MS = 1500;

// Sprungziele: Taste nach „g", Route, Textschlüssel und - falls nötig - das
// Recht, ohne das die Seite gar nicht im Menü stünde.
export const GOTO = [
  { key: 'd', path: '/', labelKey: 'nav.dashboard', perm: 'servers:read' },
  { key: 'g', path: '/groups', labelKey: 'nav.groups', perm: 'groups:read' },
  { key: 'u', path: '/linux-users', labelKey: 'nav.linuxUsers', perm: 'linuxusers:read' },
  { key: 's', path: '/security', labelKey: 'nav.security', perm: 'packages:read' },
  { key: 'c', path: '/docker', labelKey: 'nav.docker', perm: 'packages:read' },
  { key: 'j', path: '/jobs', labelKey: 'nav.jobs', perm: 'jobs:read' },
  { key: 'e', path: '/settings', labelKey: 'nav.settings', perm: 'settings' },
  { key: 'a', path: '/account', labelKey: 'nav.account' },
  { key: 'h', path: '/doku', labelKey: 'nav.docs' },
];

export const isMac = /Mac|iPhone|iPad/.test(navigator.platform);

// Anzeigetext einer Taste, so wie er am Abzeichen und in der Hilfe steht.
export function keyLabel(key) {
  if (key === 'mod+s') return isMac ? '⌘S' : 'Ctrl+S';
  if (key === '/') return '/';
  return key.toUpperCase();
}

export function visibleGoto() {
  return GOTO.filter((g) => {
    if (!g.perm) return true;
    if (g.perm === 'settings') return visibleSettingsItems().length > 0;
    return auth.can(g.perm);
  });
}

// Tippt der Mensch gerade? Dann gehören Buchstaben ihm, nicht uns.
function isEditable(el) {
  if (!el) return false;
  if (el.isContentEditable) return true;
  if (el.closest?.('.xterm')) return true;
  const tag = el.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') {
    return !['checkbox', 'radio', 'button', 'submit', 'reset', 'file', 'range', 'color'].includes(el.type);
  }
  return false;
}

function isVisible(el) {
  return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
}

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function focusables(root) {
  return [...root.querySelectorAll(FOCUSABLE)].filter(isVisible);
}

// Das Ziel eines Seitenkürzels: sichtbar, bedienbar - und wenn ein Dialog
// offen ist, nur innerhalb des Dialogs (sonst klickt „n" hinter dem Dialog).
function hotkeyTarget(key) {
  const dialog = document.querySelector('.modal.show, [role="dialog"][aria-modal="true"]');
  const scope = dialog ?? document;
  return [...scope.querySelectorAll(`[data-hotkey="${CSS.escape(key)}"]`)].find(
    (el) => isVisible(el) && !el.disabled && !el.closest('[inert]'),
  );
}

function trigger(el) {
  if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT') {
    el.focus();
    el.select?.();
  } else {
    el.focus();
    el.click();
  }
}

// Pfeiltasten in einer Leiste (data-arrow-nav="horizontal|vertical").
function arrowInBar(e, el) {
  const bar = el.closest('[data-arrow-nav]');
  if (!bar) return false;
  const horizontal = bar.dataset.arrowNav !== 'vertical';
  const prev = horizontal ? 'ArrowLeft' : 'ArrowUp';
  const next = horizontal ? 'ArrowRight' : 'ArrowDown';
  if (![prev, next, 'Home', 'End'].includes(e.key)) return false;
  const items = focusables(bar);
  const i = items.indexOf(el);
  if (i < 0) return false;
  let target;
  if (e.key === 'Home') target = items[0];
  else if (e.key === 'End') target = items[items.length - 1];
  else if (e.key === next) target = items[(i + 1) % items.length];
  else target = items[(i - 1 + items.length) % items.length];
  target?.focus();
  return true;
}

// Pfeiltasten in Tabellen: hoch/runter wechselt die Zeile (gleiche Spalte,
// sonst das erste Bedienelement der Zeile), links/rechts wandert in der Zeile.
function arrowInTable(e, el) {
  const cell = el.closest('td, th');
  const row = cell?.closest('tr');
  const table = row?.closest('table');
  if (!table || !table.closest('main')) return false;
  if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
    const items = focusables(row);
    const i = items.indexOf(el);
    const target = items[i + (e.key === 'ArrowRight' ? 1 : -1)];
    if (!target) return false;
    target.focus();
    return true;
  }
  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return false;
  const rows = [...table.querySelectorAll('tbody > tr, thead > tr')].filter(isVisible);
  const ri = rows.indexOf(row);
  const step = e.key === 'ArrowDown' ? 1 : -1;
  const col = cell.cellIndex;
  for (let r = ri + step; r >= 0 && r < rows.length; r += step) {
    const sameCol = rows[r].cells[col] ? focusables(rows[r].cells[col])[0] : null;
    const target = sameCol ?? focusables(rows[r])[0];
    if (target) {
      target.focus();
      return true;
    }
  }
  return false;
}

function createHotkeys() {
  let badges = $state(localStorage.getItem(STORAGE_KEY) !== 'off');
  let helpOpen = $state(false);
  let pending = null; // „g" wurde gedrückt, der Zielbuchstabe fehlt noch
  let pendingTimer = 0;

  function applyBadges() {
    document.documentElement.dataset.hotkeyBadges = badges ? 'on' : 'off';
  }
  applyBadges();

  // Abzeichen: Jedes Element mit data-hotkey bekommt den Anzeigetext als
  // data-hotkey-label, den das CSS als ::after anhängt. Der Beobachter
  // erledigt das für alle Seiten, ohne dass eine Komponente daran denkt.
  function labelAll(root = document) {
    for (const el of root.querySelectorAll?.('[data-hotkey]:not([data-hotkey-label])') ?? []) {
      el.dataset.hotkeyLabel = keyLabel(el.dataset.hotkey);
    }
  }
  const observer = new MutationObserver((records) => {
    for (const r of records) {
      for (const n of r.addedNodes) {
        if (n.nodeType === 1) {
          if (n.dataset?.hotkey && !n.dataset.hotkeyLabel) n.dataset.hotkeyLabel = keyLabel(n.dataset.hotkey);
          labelAll(n);
        }
      }
    }
  });
  observer.observe(document.documentElement, { childList: true, subtree: true });
  labelAll();

  function clearPending() {
    pending = null;
    clearTimeout(pendingTimer);
  }

  function goto(key) {
    const target = visibleGoto().find((g) => g.key === key);
    if (!target) return false;
    location.hash = '#' + target.path;
    return true;
  }

  function onKeydown(e) {
    const el = e.target instanceof Element ? e.target : document.activeElement;
    const mod = isMac ? e.metaKey : e.ctrlKey;

    // Speichern gilt auch beim Tippen - genau dort will man es.
    if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 's') {
      const save = hotkeyTarget('mod+s');
      if (save) {
        e.preventDefault();
        trigger(save);
      }
      return;
    }
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === 'Escape') {
      clearPending();
      if (helpOpen) helpOpen = false;
      return;
    }
    if (isEditable(el)) return;

    if (e.key.startsWith('Arrow') || e.key === 'Home' || e.key === 'End') {
      if (el && (arrowInBar(e, el) || arrowInTable(e, el))) e.preventDefault();
      return;
    }

    if (pending === 'g') {
      clearPending();
      if (goto(e.key.toLowerCase())) e.preventDefault();
      return;
    }
    if (e.key === '?') {
      e.preventDefault();
      helpOpen = !helpOpen;
      return;
    }
    if (helpOpen) return;
    if (e.key === 'g') {
      pending = 'g';
      pendingTimer = setTimeout(clearPending, SEQUENCE_MS);
      return;
    }
    if (!auth.isLoggedIn) return;
    const target = hotkeyTarget(e.key.length === 1 ? e.key.toLowerCase() : e.key);
    if (target) {
      e.preventDefault();
      trigger(target);
    }
  }
  document.addEventListener('keydown', onKeydown);
  // Ein Seitenwechsel schließt die Hilfe - ihre Seitenkürzel gelten dort
  // nicht mehr.
  window.addEventListener('hashchange', () => (helpOpen = false));

  return {
    get badges() {
      return badges;
    },
    set badges(v) {
      badges = !!v;
      localStorage.setItem(STORAGE_KEY, badges ? 'on' : 'off');
      applyBadges();
    },
    get helpOpen() {
      return helpOpen;
    },
    set helpOpen(v) {
      helpOpen = !!v;
    },
    // Seitenkürzel, die gerade greifen würden - für die Hilfe.
    pageActions() {
      const seen = new Set();
      const out = [];
      for (const el of document.querySelectorAll('[data-hotkey]')) {
        const key = el.dataset.hotkey;
        if (seen.has(key) || !isVisible(el) || el.disabled) continue;
        seen.add(key);
        const label =
          el.getAttribute('aria-label') || el.getAttribute('title') || el.textContent?.trim() || el.getAttribute('placeholder') || key;
        out.push({ key, label: label.replace(/\s+/g, ' ') });
      }
      return out;
    },
    get currentPath() {
      return router.location;
    },
  };
}

export const hotkeys = createHotkeys();
