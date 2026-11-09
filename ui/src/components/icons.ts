import { html, svg, type TemplateResult } from 'lit';

/** Eigener kleiner Symbolsatz: Linien-Icons auf 24er Raster, Farbe über currentColor. */
const paths = {
  search: svg`<circle cx="11" cy="11" r="6.5"/><path d="m20 20-4-4"/>`,
  book: svg`<path d="M12 6.5C10.5 5 8 4.5 4.5 4.5v13c3.5 0 6 .5 7.5 2 1.5-1.5 4-2 7.5-2v-13c-3.5 0-6 .5-7.5 2z"/><path d="M12 6.5v13"/>`,
  sun: svg`<circle cx="12" cy="12" r="4"/><path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6 7 7M17 17l1.4 1.4M5.6 18.4 7 17M17 7l1.4-1.4"/>`,
  moon: svg`<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"/>`,
  auto: svg`<circle cx="12" cy="12" r="8"/><path d="M12 4v16"/><path d="M12 4a8 8 0 0 1 0 16z" fill="currentColor" stroke="none"/>`,
  globe: svg`<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.6 2.4 3.6 5.3 3.6 8.5s-1 6.1-3.6 8.5c-2.6-2.4-3.6-5.3-3.6-8.5s1-6.1 3.6-8.5z"/>`,
  user: svg`<circle cx="12" cy="8.5" r="3.5"/><path d="M5 20c.6-3.6 3.4-5.5 7-5.5s6.4 1.9 7 5.5"/>`,
  users: svg`<circle cx="9" cy="8.5" r="3"/><path d="M3 19.5c.5-3.2 2.8-5 6-5s5.5 1.8 6 5"/><path d="M16 5.7a3 3 0 0 1 0 5.6M18.5 14.8c1.5.7 2.3 2.3 2.5 4.2"/>`,
  'chevron-down': svg`<path d="m6 9 6 6 6-6"/>`,
  'chevron-up': svg`<path d="m6 15 6-6 6 6"/>`,
  'chevron-right': svg`<path d="m9 6 6 6-6 6"/>`,
  'chevron-left': svg`<path d="m15 6-6 6 6 6"/>`,
  check: svg`<path d="m5 12.5 4.5 4.5L19 7.5"/>`,
  x: svg`<path d="M6 6l12 12M18 6 6 18"/>`,
  menu: svg`<path d="M4 7h16M4 12h16M4 17h16"/>`,
  dashboard: svg`<rect x="3.5" y="3.5" width="7" height="8" rx="2"/><rect x="13.5" y="3.5" width="7" height="5" rx="2"/><rect x="13.5" y="11.5" width="7" height="9" rx="2"/><rect x="3.5" y="14.5" width="7" height="6" rx="2"/>`,
  server: svg`<rect x="3.5" y="4" width="17" height="7" rx="2.5"/><rect x="3.5" y="13" width="17" height="7" rx="2.5"/><path d="M7.5 7.5h.01M7.5 16.5h.01"/>`,
  terminal: svg`<rect x="3" y="4.5" width="18" height="15" rx="3"/><path d="m7.5 10 3 2.5-3 2.5M13 15h4"/>`,
  shield: svg`<path d="M12 3.5 5 6v5.5c0 4.2 2.9 7.3 7 9 4.1-1.7 7-4.8 7-9V6z"/><path d="m9 12 2.2 2.2L15.5 10"/>`,
  container: svg`<path d="m12 3 8 4.2v9.6L12 21l-8-4.2V7.2z"/><path d="m4 7.2 8 4.3 8-4.3M12 11.5V21"/>`,
  jobs: svg`<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`,
  settings: svg`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`,
  refresh: svg`<path d="M19.5 12a7.5 7.5 0 0 1-13 5.1M4.5 12a7.5 7.5 0 0 1 13-5.1"/><path d="M17.5 3v4h-4M6.5 21v-4h4"/>`,
  play: svg`<path d="M8 5.5v13l10.5-6.5z"/>`,
  trash: svg`<path d="M4.5 7h15M9.5 7V4.5h5V7M6.5 7l.8 12a1.5 1.5 0 0 0 1.5 1.4h6.4a1.5 1.5 0 0 0 1.5-1.4L17.5 7"/><path d="M10 11v6M14 11v6"/>`,
  edit: svg`<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>`,
  plus: svg`<path d="M12 5v14M5 12h14"/>`,
  filter: svg`<path d="M4 6h16l-6 7.5V19l-4 1.5v-7z"/>`,
  columns: svg`<rect x="3.5" y="4.5" width="17" height="15" rx="3"/><path d="M9.5 4.5v15M15 4.5v15"/>`,
  'sort-asc': svg`<path d="m7 14 5-5 5 5"/>`,
  'sort-desc': svg`<path d="m7 10 5 5 5-5"/>`,
  sort: svg`<path d="m8 9.5 4-4 4 4M8 14.5l4 4 4-4"/>`,
  info: svg`<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.5M12 7.7v.01"/>`,
  warning: svg`<path d="M12 4 3 19.5h18z"/><path d="M12 10v4.5M12 17.2v.01"/>`,
  external: svg`<path d="M14 4h6v6M20 4l-9 9"/><path d="M18 14v4.5a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 18.5v-10A1.5 1.5 0 0 1 6.5 7H11"/>`,
  link: svg`<path d="M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1 1"/><path d="M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1-1"/>`,
  lock: svg`<rect x="5" y="10.5" width="14" height="10" rx="3"/><path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/>`,
  key: svg`<circle cx="8" cy="15" r="3.5"/><path d="m10.5 12.5 8-8M15.5 7.5l2.5 2.5M13.5 9.5l2 2"/>`,
  eye: svg`<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.8"/>`,
  'eye-off': svg`<path d="M4 4l16 16M9.8 6a8.7 8.7 0 0 1 2.2-.5C18 5.5 21.5 12 21.5 12a14 14 0 0 1-2.7 3.4M6.3 7.7A14 14 0 0 0 2.5 12S6 18.5 12 18.5a8.6 8.6 0 0 0 3.6-.8"/><path d="M10 10.2a2.8 2.8 0 0 0 3.8 3.8"/>`,
  firewall: svg`<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><path d="M3.5 9.5h17M3.5 14.5h17M9 4.5v5M15 9.5v5M9 14.5v5"/>`,
  calendar: svg`<rect x="3.5" y="5" width="17" height="15.5" rx="3"/><path d="M3.5 10h17M8 3v4M16 3v4"/>`,
  clock: svg`<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`,
  package: svg`<path d="m12 3 8 4.2v9.6L12 21l-8-4.2V7.2z"/><path d="m4 7.2 8 4.3 8-4.3M8 5l8 4.3"/>`,
  copy: svg`<rect x="8.5" y="8.5" width="12" height="12" rx="3"/><path d="M15.5 8.5V6.5A2.5 2.5 0 0 0 13 4H6.5A2.5 2.5 0 0 0 4 6.5V13a2.5 2.5 0 0 0 2.5 2.5h2"/>`,
  'panel-right': svg`<rect x="3.5" y="4.5" width="17" height="15" rx="3"/><path d="M14.5 4.5v15"/>`,
  'arrow-right': svg`<path d="M5 12h14M13 6l6 6-6 6"/>`,
  power: svg`<path d="M12 3.5v8M7 6.5a7.5 7.5 0 1 0 10 0"/>`,
  undo: svg`<path d="M9 8 4.5 12.5 9 17"/><path d="M5 12.5h9a5.5 5.5 0 0 1 0 11"/>`,
  bolt: svg`<path d="M13 3 5 13.5h6L10 21l8-10.5h-6z"/>`,
  download: svg`<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19.5h14"/>`,
  bell: svg`<path d="M6 17V11a6 6 0 0 1 12 0v6l1.5 2h-15z"/><path d="M10 21h4"/>`,
  logout: svg`<path d="M10 4.5H6.5A2.5 2.5 0 0 0 4 7v10a2.5 2.5 0 0 0 2.5 2.5H10M15 8l4 4-4 4M19 12H9.5"/>`,
  keyboard: svg`<rect x="2.5" y="6" width="19" height="12" rx="3"/><path d="M6.5 10h.01M10 10h.01M14 10h.01M17.5 10h.01M7 14h10"/>`,
  cpu: svg`<rect x="6.5" y="6.5" width="11" height="11" rx="2.5"/><path d="M9.5 3.5v3M14.5 3.5v3M9.5 17.5v3M14.5 17.5v3M3.5 9.5h3M3.5 14.5h3M17.5 9.5h3M17.5 14.5h3"/>`,
  disk: svg`<ellipse cx="12" cy="6.5" rx="7.5" ry="3"/><path d="M4.5 6.5v11c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3v-11M4.5 12c0 1.7 3.4 3 7.5 3s7.5-1.3 7.5-3"/>`,
  repair: svg`<path d="M14.5 6.5a4 4 0 0 0 4.8 4.8L12 18.6a2.5 2.5 0 0 1-3.5-3.5z"/><path d="m15 5 4 4"/>`,
} satisfies Record<string, ReturnType<typeof svg>>;

export type IconName = keyof typeof paths;

/** Ein Symbol als Inline-SVG. Dekorativ (aria-hidden): die Beschriftung liefert der umgebende Knopf. */
export function icon(name: IconName): TemplateResult {
  return html`<svg
    class="icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    ${paths[name]}
  </svg>`;
}
