// Dateien zum Speichern anbieten. Ein Link auf einen geschützten API-Pfad trüge die Sitzung der
// Arbeitsspeicher-Variante nicht mit (sie liegt nur in der Kopfzeile): Die Datei wird per fetch
// geholt (api.download) und hier als Blob angeboten.

/** Bietet einen Blob als Datei zum Speichern an. */
export function saveBlob(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
