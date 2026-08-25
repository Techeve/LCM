// Farbmodus der App (Bootstrap 5.3 Color Modes): "auto" folgt dem
// Betriebssystem (Default), "light"/"dark" sind feste Overrides. Die Wahl
// wird in localStorage gemerkt und als data-bs-theme auf <html> gesetzt -
// alle Bootstrap-Komponenten schalten damit automatisch um.
const STORAGE_KEY = 'lcm.theme';
const media = window.matchMedia('(prefers-color-scheme: dark)');

const MODES = ['auto', 'dark', 'light'];

function sanitize(value) {
  return MODES.includes(value) ? value : 'auto';
}

function createTheme() {
  let mode = $state(sanitize(localStorage.getItem(STORAGE_KEY)));

  function apply() {
    const resolved = mode === 'auto' ? (media.matches ? 'dark' : 'light') : mode;
    document.documentElement.setAttribute('data-bs-theme', resolved);
  }

  // Im Auto-Modus live auf Systemwechsel (hell ↔ dunkel) reagieren.
  media.addEventListener('change', () => {
    if (mode === 'auto') apply();
  });
  apply();

  return {
    get mode() {
      return mode;
    },
    set(value) {
      mode = sanitize(value);
      localStorage.setItem(STORAGE_KEY, mode);
      apply();
    },
    // Durchschalten in der Navbar: System → Dunkel → Hell → System …
    cycle() {
      this.set(MODES[(MODES.indexOf(mode) + 1) % MODES.length]);
    },
  };
}

export const theme = createTheme();
