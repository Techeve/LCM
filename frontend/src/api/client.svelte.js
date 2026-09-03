/**
 * Zentraler API-Client des Templates.
 *
 * Komponenten rufen NIE direkt fetch() auf, sondern immer Methoden der
 * Service-Klassen (siehe index.js). Dieser Client erledigt zentral:
 *
 *  - JWT-Verwaltung: Token liegt in localStorage und wird automatisch
 *    als Authorization-Header an jeden Request gehängt.
 *  - Fehler-Normalisierung: Jede Fehlerantwort wird eine ApiError mit
 *    status und message.
 *  - Auto-Logout: Antwortet der Server mit 401 (Token abgelaufen oder
 *    ungültig), wird die Session lokal verworfen und zum Login geleitet.
 *
 * Die Datei ist eine .svelte.js-Datei, damit der Auth-Zustand mit
 * Svelte-5-Runes ($state) reaktiv ist.
 */

import { push } from 'svelte-spa-router';
import { i18n } from '../stores/i18n.svelte.js';

const TOKEN_KEY = 'lcm.token';
const USER_KEY = 'lcm.user';

/** Normalisierter API-Fehler mit HTTP-Status. */
export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

class ApiClient {
  /** Reaktiver Auth-Zustand (Svelte 5 Runes). */
  #session = $state({
    token: localStorage.getItem(TOKEN_KEY),
    user: JSON.parse(localStorage.getItem(USER_KEY) ?? 'null'),
  });

  get user() {
    return this.#session.user;
  }

  get isLoggedIn() {
    return this.#session.token !== null;
  }

  /** Prüft eine RBAC-Permission des eingeloggten Users (fürs UI). */
  hasPermission(code) {
    return this.#session.user?.permissions?.includes(code) ?? false;
  }

  /** Wird von AuthApi.login() aufgerufen. */
  startSession(token, user) {
    this.#session = { token, user };
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
  }

  /** Verwirft die Session lokal und leitet zum Login um. */
  logout(redirect = true) {
    this.#session = { token: null, user: null };
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
    if (redirect) push('/login');
  }

  /**
   * Führt einen Request gegen /api/v1 aus.
   * @param {string} method  HTTP-Methode
   * @param {string} path    Pfad relativ zu /api/v1, z.B. '/users'
   * @param {object} [body]  wird als JSON gesendet
   */
  async request(method, path, body) {
    const headers = { Accept: 'application/json' };
    if (this.#session.token) {
      headers['Authorization'] = `Bearer ${this.#session.token}`;
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }

    const response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (response.status === 401 && this.isLoggedIn) {
      // Token abgelaufen/ungültig => zentrale Logout-Logik.
      this.logout();
      throw new ApiError(401, i18n.t('common.sessionExpired'));
    }

    if (!response.ok) {
      let message = `HTTP ${response.status}`;
      try {
        const data = await response.json();
        if (data.error) message = data.error;
      } catch {
        /* keine JSON-Antwort */
      }
      throw new ApiError(response.status, message);
    }

    if (response.status === 204) return null;
    return response.json();
  }

  /**
   * Liest einen Server-Sent-Events-Strom und ruft onEvent je Ereignis auf.
   *
   * Bewusst per fetch statt über EventSource: Die Anmeldung läuft über den
   * Authorization-Header, den EventSource nicht setzen kann. Der naheliegende
   * Ausweg - den Token an die URL hängen - verbietet sich, weil er dann in
   * Zugriffs- und Proxy-Protokollen steht. Der Token bleibt so auch hier
   * gekapselt; die Seite bekommt ihn nie zu sehen.
   *
   * @param {string} path    Pfad relativ zu /api/v1
   * @param {AbortSignal} signal  beendet den Strom
   * @param {(event: string, data: string) => void} onEvent
   */
  async stream(path, signal, onEvent) {
    const headers = { Accept: 'text/event-stream' };
    if (this.#session.token) {
      headers['Authorization'] = `Bearer ${this.#session.token}`;
    }
    const response = await fetch(`/api/v1${path}`, { headers, signal });
    if (response.status === 401 && this.isLoggedIn) {
      this.logout();
      throw new ApiError(401, i18n.t('common.sessionExpired'));
    }
    if (!response.ok) {
      throw new ApiError(response.status, `HTTP ${response.status}`);
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let rest = '';
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      rest += decoder.decode(value, { stream: true });
      // SSE trennt Ereignisse durch eine Leerzeile; ein angefangenes bleibt
      // liegen, bis es vollständig ist.
      const blocks = rest.split('\n\n');
      rest = blocks.pop();
      for (const block of blocks) {
        let event = 'message';
        let data = '';
        for (const line of block.split('\n')) {
          if (line.startsWith('event: ')) event = line.slice(7);
          else if (line.startsWith('data: ')) data += line.slice(6);
        }
        if (data) onEvent(event, data);
      }
    }
  }

  /**
   * Lädt eine Datei (mit Auth-Header) und stößt den Browser-Download an.
   * @param {string} path      Pfad relativ zu /api/v1
   * @param {string} filename  Vorgeschlagener Dateiname
   */
  async download(path, filename) {
    const headers = {};
    if (this.#session.token) {
      headers['Authorization'] = `Bearer ${this.#session.token}`;
    }
    const response = await fetch(`/api/v1${path}`, { headers });
    if (response.status === 401 && this.isLoggedIn) {
      this.logout();
      throw new ApiError(401, i18n.t('common.sessionExpired'));
    }
    if (!response.ok) {
      let message = `HTTP ${response.status}`;
      try {
        const data = await response.json();
        if (data.error) message = data.error;
      } catch {
        /* keine JSON-Antwort */
      }
      throw new ApiError(response.status, message);
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  /**
   * Sendet ein FormData (multipart/form-data) - für Datei-Uploads. Der Browser
   * setzt Content-Type inkl. Boundary selbst.
   * @param {string} path         Pfad relativ zu /api/v1
   * @param {FormData} formData    Multipart-Payload
   */
  async upload(path, formData) {
    const headers = { Accept: 'application/json' };
    if (this.#session.token) {
      headers['Authorization'] = `Bearer ${this.#session.token}`;
    }
    const response = await fetch(`/api/v1${path}`, { method: 'POST', headers, body: formData });
    if (response.status === 401 && this.isLoggedIn) {
      this.logout();
      throw new ApiError(401, i18n.t('common.sessionExpired'));
    }
    if (!response.ok) {
      let message = `HTTP ${response.status}`;
      try {
        const data = await response.json();
        if (data.error) message = data.error;
      } catch {
        /* keine JSON-Antwort */
      }
      throw new ApiError(response.status, message);
    }
    if (response.status === 204) return null;
    return response.json();
  }

  get(path) {
    return this.request('GET', path);
  }
  post(path, body) {
    return this.request('POST', path, body);
  }
  put(path, body) {
    return this.request('PUT', path, body);
  }
  patch(path, body) {
    return this.request('PATCH', path, body);
  }
  delete(path) {
    return this.request('DELETE', path);
  }
}

/** Singleton - von allen Service-Klassen geteilt. */
export const client = new ApiClient();
