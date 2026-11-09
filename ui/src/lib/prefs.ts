// Oberflächen-Einstellungen im Browser (Theme, Sprache, Dichte ...). Hier liegen
// nie API-Daten. localStorage kann fehlen oder werfen (privates Fenster,
// gesperrte Websitedaten), deshalb gilt jeder Zugriff als optional.

export function readPref(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writePref(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Einstellung gilt dann nur bis zum Neuladen
  }
}
