// Register für ungespeicherte Eingaben. Formulare und Fenster melden sich an,
// solange sie Änderungen halten, und ab, sobald gespeichert oder verworfen ist.
// Der Versionswächter lädt nur neu, solange das Register leer ist. Der Router
// fragt nach, bevor ein Benutzer die Seite wechselt, und der Browser warnt beim
// Neuladen und Schließen. Die Rückfrage selbst stellt die Shell (`setPrompt`),
// damit dieses Modul keine Oberfläche kennt.

const owners = new Set<object>();
const listeners = new Set<() => void>();
let prompt: (() => Promise<boolean>) | undefined;

export const unsaved = {
  set(owner: object, dirty: boolean): void {
    const before = owners.size > 0;
    if (dirty) owners.add(owner);
    else owners.delete(owner);
    if (owners.size > 0 !== before) listeners.forEach((listener) => listener());
  },

  has(): boolean {
    return owners.size > 0;
  },

  /** Wird aufgerufen, wenn das Register leer oder nicht mehr leer wird. */
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },

  setPrompt(ask: () => Promise<boolean>): void {
    prompt = ask;
  },

  /** Fragt nach, wenn etwas verloren ginge; `true` heißt: Die Seite darf gewechselt werden. */
  mayLeave(): Promise<boolean> {
    return owners.size > 0 && prompt ? prompt() : Promise.resolve(true);
  },
};

addEventListener('beforeunload', (event) => {
  if (owners.size > 0) event.preventDefault();
});
