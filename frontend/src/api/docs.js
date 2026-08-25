/**
 * DocsApi - die in LCM mitgelieferte Anwender-Doku (Markdown, ins Binary
 * eingebettet). Öffentlich erreichbar: Die Anleitung zum Einrichten des
 * SSH-Schlüssels wird gebraucht, BEVOR man einen Zugang hat.
 */
export class DocsApi {
  #client;

  constructor(client) {
    this.#client = client;
  }

  /** Seitenübersicht [{slug, title}] in der gewünschten Sprache. */
  list(lang) {
    return this.#client.get(`/docs?lang=${encodeURIComponent(lang)}`);
  }

  /** Eine Seite samt gerendertem HTML. */
  get(slug, lang) {
    return this.#client.get(`/docs/${encodeURIComponent(slug)}?lang=${encodeURIComponent(lang)}`);
  }
}
