// Deep-Link-Normalisierung: Das Routing ist hash-basiert (#/…), das Backend
// liefert aber für JEDEN Pfad die index.html aus. Ein direkter Aufruf von
// z. B. /security oder /servers/1 (Bookmark, geteilter Link) kommt daher ohne
// Hash an - der Router sähe nur „/" und rendert das Dashboard.
//
// Warum ein eigenes Modul und nicht ein paar Zeilen in main.js: ES-Importe
// werden VOR dem Rumpf des importierenden Moduls ausgewertet. svelte-spa-router
// liest den Hash beim Laden - stünde die Korrektur im Rumpf von main.js, wäre
// der Router längst auf „/" festgelegt, und ein späteres replaceState löst
// kein hashchange aus. Als erster Import läuft sie früh genug.
const { pathname, search, hash } = window.location;
if (pathname !== '/') {
  if (hash.startsWith('#/')) {
    // Hash-Route bereits vorhanden (z. B. /security#/docker): sie gewinnt,
    // nur der überflüssige Pfad wird entfernt.
    window.history.replaceState(null, '', '/' + hash);
  } else {
    // Trailing Slash entfernen - '/security/' würde sonst keine Route treffen.
    const route = pathname.replace(/\/+$/, '') || '/';
    window.history.replaceState(null, '', '/#' + route + search);
  }
}
