// Ein Job öffnet sich als Fenster über der Seite, auf der man gerade ist: Wer es schließt, steht wieder dort.
// Die Adresse `/jobs/<id>` bleibt der Weg für Lesezeichen und neue Tabs.

import { router } from './router';

const listeners = new Set<() => void>();
let openId = '';

function set(id: string): void {
  if (id === openId) return;
  openId = id;
  for (const listener of listeners) listener();
}

export const jobWindow = {
  get id(): string {
    return openId;
  },
  /** Auf der Jobseite ist das Fenster schon Teil der Seite, sie hält auch ihre Liste aktuell. */
  open(id: string): void {
    if (router.state.path.startsWith('/jobs')) void router.navigate(`/jobs/${encodeURIComponent(id)}${location.search}`);
    else set(id);
  },
  close(): void {
    set('');
  },
  onChange(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
};

/** Klick auf einen Verweis nach `/jobs/<id>`: öffnet das Fenster; Cmd/Strg/Umschalt behalten den neuen Tab. */
export function openJobOnClick(id: string): (event: MouseEvent) => void {
  return (event) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) return;
    event.preventDefault();
    jobWindow.open(id);
  };
}
