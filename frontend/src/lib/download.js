/**
 * downloadText - bietet einen Text als Datei-Download an (z.B. den einmalig
 * ausgelieferten privaten SSH-Schlüssel). Läuft rein clientseitig über eine
 * Blob-URL; nichts verlässt den Browser.
 */
export function downloadText(filename, text) {
  const blob = new Blob([text], { type: 'application/octet-stream' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
