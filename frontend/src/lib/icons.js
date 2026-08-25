// Gemeinsame Inline-SVG-Icons (Zero-Bloat: keine Icon-Font/-Bibliothek nötig).
// Alle Icons nutzen stroke="currentColor" bzw. fill="currentColor" und
// width/height="1em" - sie erben Textfarbe UND -größe des umgebenden
// Elements und passen sich damit automatisch an Light-/Dark-Mode sowie
// Bootstrap-Textklassen (text-danger, text-body-secondary, …) an. Anders als
// farbige Emoji sehen sie in jedem Betriebssystem/Browser gleich aus.
//
// Verwendung: {@html icons.refresh} (Svelte rendert rohes SVG-Markup, wie im
// Navbar-Farbmodus-/Sprachumschalter bereits etabliert).

const line = (inner) =>
  `<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:-0.125em">${inner}</svg>`;

export const icons = {
  // Aktualisieren/neu einlesen (ersetzt 🔄).
  refresh: line(
    '<polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/>',
  ),
  // Gesperrt/eingeschränkt (ersetzt 🔒).
  lock: line('<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>'),
  // Einstellungen (ersetzt ⚙️) - 8-strahliges Zahnrad.
  gear: line(
    '<circle cx="12" cy="12" r="3.2"/><circle cx="12" cy="12" r="7.4"/>' +
      '<line x1="12" y1="2.4" x2="12" y2="5.2"/><line x1="12" y1="18.8" x2="12" y2="21.6"/>' +
      '<line x1="2.4" y1="12" x2="5.2" y2="12"/><line x1="18.8" y1="12" x2="21.6" y2="12"/>' +
      '<line x1="5.3" y1="5.3" x2="7.2" y2="7.2"/><line x1="16.8" y1="16.8" x2="18.7" y2="18.7"/>' +
      '<line x1="18.7" y1="5.3" x2="16.8" y2="7.2"/><line x1="7.2" y1="16.8" x2="5.3" y2="18.7"/>',
  ),
  // CVE-/Sicherheits-Badge (ersetzt 🛡).
  shield: line('<path d="M12 2 4 5v6c0 5 3.4 8.5 8 9 4.6-.5 8-4 8-9V5l-8-3z"/>'),
  // Paket/Container (ersetzt 📦).
  box: line('<path d="M21 8 12 3 3 8v8l9 5 9-5V8z"/><path d="M3.3 7.6 12 12.5l8.7-4.9"/><path d="M12 12.5V21.5"/>'),
  // Unbekannt (ersetzt ❔).
  question: line(
    '<circle cx="12" cy="12" r="9.5"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
  ),
  // Virtuelle Maschine (ersetzt 🖥️).
  monitor: line('<rect x="2" y="4" width="20" height="13" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>'),
  // Physisches Blech (ersetzt 🔩).
  cpu: line(
    '<rect x="6" y="6" width="12" height="12" rx="1"/>' +
      '<line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/>' +
      '<line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/>' +
      '<line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="15" x2="4" y2="15"/>' +
      '<line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="15" x2="23" y2="15"/>',
  ),
  // Laufender Job (ersetzt ⏳).
  clock: line('<circle cx="12" cy="12" r="9.5"/><polyline points="12 7 12 12 15.5 14"/>'),
  // Löschen/Aufräumen (ersetzt 🧹 sowie "Entfernen").
  trash: line(
    '<polyline points="3 6 5 6 21 6"/><path d="M19 6v13a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>' +
      '<line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>',
  ),
  // Scannen (ersetzt 🔍).
  search: line('<circle cx="11" cy="11" r="7.5"/><line x1="21" y1="21" x2="16.3" y2="16.3"/>'),
  // Warnung (ersetzt ⚠).
  warning: line('<path d="M12 2.5 1.5 21h21L12 2.5z"/><line x1="12" y1="9" x2="12" y2="13.5"/><line x1="12" y1="17" x2="12.01" y2="17"/>'),
  // Bearbeiten (ersetzt ✎).
  pencil: line('<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z"/>'),
  // Update verfügbar (ersetzt 🚀).
  arrowUpCircle: line('<circle cx="12" cy="12" r="9.5"/><polyline points="16 13 12 9 8 13"/><line x1="12" y1="16" x2="12" y2="9"/>'),
  // Neustart (neu - Aktionen-Menü).
  power: line('<path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/>'),
  // Zertifikat rotieren (Aktionen-Menü).
  key: line('<circle cx="7.5" cy="15.5" r="5.5"/><path d="M21 2 11.4 11.6"/><path d="M15.5 7.5 19 11l3-3"/>'),
  // Neu verbinden (Aktionen-Menü).
  link: line(
    '<path d="M10 13a5 5 0 0 0 7.07 0l1.93-1.93a5 5 0 0 0-7.07-7.07L10.5 5.5"/>' +
      '<path d="M14 11a5 5 0 0 0-7.07 0L5 12.93a5 5 0 0 0 7.07 7.07L13.5 18.5"/>',
  ),
  // User-Sync (Aktionen-Menü).
  users: line(
    '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/>' +
      '<path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
  ),
};
