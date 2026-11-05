// Eingabeprüfung, die überall gleich gilt. Die Oberfläche meldet früh, was das Backend
// ohnehin ablehnt; die Grenzen spiegeln internal/api/controllers/validate.go. Das Backend
// bleibt die Autorität, hier geht es nur darum, Müll gar nicht erst abzuschicken.

/** Längengrenzen in Zeichen, wie im Backend. */
export const LIMITS = {
  name: 64,
  description: 1000,
  host: 253,
  email: 254,
  /** Absender mit Anzeigenamen (`maxEmailLen + 2 * maxPersonNameLen + 8`): Name (auch mit Escapes), Adresse, Klammern. */
  sender: 390,
  personName: 64,
  loginUser: 64,
  password: 1024,
  /** Neue Passwörter von Benutzern (Passwort-Regeln des Servers, `password_policy.go`). */
  newPassword: 200,
  script: 32768,
  query: 100,
  cron: 100,
  command: 512,
  path: 4096,
  /** Linux-Benutzernamen (useradd, NAME_MAX der Distributionen). */
  linuxUser: 32,
  /** Bemerkung einer Firewall-Regel: sie landet im Regelsatz auf dem Zielsystem (`firewallCommentMax`). */
  firewallComment: 120,
  /** Aktivierungslink-Token und Subscription-Schlüssel (`maxTokenLen`). */
  token: 256,
  /** Gültigkeit eines API-Schlüssels in Tagen (`maxAPIKeyDays`). */
  apiKeyDays: 3650,
  /** Listenfelder (Presets, Domains, Empfänger, Paketnamen), `maxListLen`. */
  list: 4096,
  /** Eigene Quell-Adressen je Firewall-Regel (`firewallMaxSources`). */
  firewallSources: 100,
} as const;

/** Länge in Zeichen wie `utf8.RuneCountInString`, nicht in UTF-16-Einheiten. */
export function charCount(value: string): number {
  return [...value].length;
}

// Steuerzeichen außer Zeilenumbruch und Tabulator; einzeilige Felder verbieten auch diese.
const CONTROL = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/;
const LINE_BREAK = /[\t\n\r]/;

/** Enthält Steuerzeichen; Zeilenumbrüche und Tabulatoren nur bei `allowBreaks` erlaubt. */
export function hasControlChars(value: string, allowBreaks = false): boolean {
  return CONTROL.test(value) || (!allowBreaks && LINE_BREAK.test(value));
}

/** Einzeilig, ohne Steuerzeichen, höchstens `max` Zeichen. Leer gilt als gültig, Pflicht prüft der Aufrufer. */
export function isLine(value: string, max: number): boolean {
  return charCount(value) <= max && !CONTROL.test(value) && !LINE_BREAK.test(value);
}

export function isIPv4(value: string): boolean {
  const parts = value.split('.');
  return parts.length === 4 && parts.every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255);
}

export function isIPv6(value: string): boolean {
  if (!/^[0-9a-fA-F:.]+$/.test(value) || !value.includes(':')) return false;
  const halves = value.split('::');
  if (halves.length > 2) return false;
  const groupsOf = (half: string) => (half === '' ? [] : half.split(':'));
  const groups = halves.flatMap(groupsOf);
  const tail = groups.at(-1) ?? '';
  const v4Tail = tail.includes('.');
  if (v4Tail && !isIPv4(tail)) return false;
  const hexGroups = v4Tail ? groups.slice(0, -1) : groups;
  if (!hexGroups.every((group) => /^[0-9a-fA-F]{1,4}$/.test(group))) return false;
  const size = hexGroups.length + (v4Tail ? 2 : 0);
  return halves.length === 2 ? size < 8 : size === 8;
}

export function isIP(value: string): boolean {
  return isIPv4(value) || isIPv6(value);
}

/** Die Adresse als Zahl, für den Vergleich mit dem Netz. */
function ipBits(address: string): bigint {
  if (isIPv4(address)) return address.split('.').reduce((bits, part) => (bits << 8n) | BigInt(part), 0n);
  const [head, tail = ''] = address.split('::');
  const expand = (half: string) => (half === '' ? [] : half.split(':')).flatMap((group) => (group.includes('.') ? v4Groups(group) : [group]));
  const left = expand(head);
  const right = expand(tail);
  const groups = address.includes('::') ? [...left, ...Array<string>(8 - left.length - right.length).fill('0'), ...right] : left;
  return groups.reduce((bits, group) => (bits << 16n) | BigInt(`0x${group}`), 0n);
}

/** Eine eingebettete IPv4-Adresse (`::ffff:192.0.2.1`) als zwei IPv6-Gruppen. */
function v4Groups(address: string): string[] {
  const bits = ipBits(address);
  return [(bits >> 16n).toString(16), (bits & 0xffffn).toString(16)];
}

/** Eine IP-Adresse oder ein Netz in CIDR-Schreibweise, IPv4 oder IPv6. Hostnamen sind keine Quelle. Bei einem Netz muss die Netzadresse stehen: `10.0.0.5/24` ist ein Tippfehler. */
export function isAddressOrNet(entry: string): boolean {
  const [address, prefix, ...rest] = entry.split('/');
  if (rest.length > 0) return false;
  const v4 = isIPv4(address);
  if (!v4 && !isIPv6(address)) return false;
  if (prefix === undefined) return true;
  const width = v4 ? 32 : 128;
  if (!/^(0|[1-9]\d{0,2})$/.test(prefix) || Number(prefix) > width) return false;
  return (ipBits(address) & ((1n << BigInt(width - Number(prefix))) - 1n)) === 0n;
}

/** Liste aus Kommas, Leerzeichen oder Zeilen. */
export function splitList(text: string): string[] {
  return text.split(/[\s,]+/).filter(Boolean);
}

/** Die Einträge einer Adressliste, die weder IP noch Netz sind. */
export function invalidAddresses(text: string): string[] {
  return splitList(text).filter((entry) => !isAddressOrNet(entry));
}

/** Was wie eine IPv4-Adresse aussieht, muss auch eine sein, sonst wären `300.1.1.1` und `192.168.1.x` Hostnamen. */
function looksLikeIPv4(name: string): boolean {
  const labels = name.split('.');
  return /^\d+$/.test(labels.at(-1) ?? '') || (labels.length === 4 && labels.slice(0, 3).every((label) => /^\d+$/.test(label)));
}

/** Hostname nach RFC 1123: Labels aus Buchstaben, Ziffern und Bindestrich, je höchstens 63, gesamt 253. */
export function isHostname(value: string): boolean {
  const name = value.endsWith('.') ? value.slice(0, -1) : value;
  if (name === '' || name.length > LIMITS.host || looksLikeIPv4(name)) return false;
  return name.split('.').every((label) => /^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$/.test(label));
}

/** Hostname oder IP, auch IPv6 in eckigen Klammern und mit Zonenkennung (`fe80::1%eth0`). */
export function isHost(value: string): boolean {
  const bare = value.startsWith('[') && value.endsWith(']') ? value.slice(1, -1) : value;
  const zone = bare.indexOf('%');
  const address = zone >= 0 && bare.includes(':') && /^[A-Za-z0-9_.-]+$/.test(bare.slice(zone + 1)) ? bare.slice(0, zone) : bare;
  return isIP(address) || isHostname(value);
}

/** Port 1 bis 65535, nur Ziffern. */
export function isPort(value: string | number): boolean {
  const text = String(value).trim();
  return /^[1-9]\d{0,4}$/.test(text) && Number(text) <= 65535;
}

/** Nur Ziffern übrig lassen, etwa für Port-Felder beim Tippen. */
export function digitsOnly(value: string, maxDigits = 5): string {
  return value.replace(/\D+/g, '').slice(0, maxDigits);
}

/** Eine einzelne Adresse ohne Anzeigenamen, wie `mail.ParseAddress` mit gleicher Rückgabe. */
export function isEmail(value: string): boolean {
  return charCount(value) <= LIMITS.email && /^[^\s@"<>(),;:\\[\]]+@[^\s@"<>(),;:\\[\]]+\.[^\s@"<>(),;:\\[\]]+$/.test(value);
}

/** Anzeigename und Adresse eines Absenders, `null` bei ungültiger Form. */
export function parseSender(value: string): { name: string; address: string } | null {
  const v = value.trim();
  if (hasControlChars(v)) return null;
  if (!v.includes('<')) return v.includes('>') || !isEmail(v) ? null : { name: '', address: v };
  const match = /^(.*?)\s*<([^<>]*)>$/.exec(v);
  if (!match) return null;
  const address = match[2].trim();
  if (!isEmail(address)) return null;
  const raw = match[1];
  if (raw === '') return { name: '', address };
  if (raw.startsWith('"')) {
    const inner = raw.slice(1, -1);
    const closed = raw.length >= 2 && raw.endsWith('"') && /^(?:[^"\\]|\\.)*$/.test(inner);
    return closed ? { name: inner.replace(/\\(.)/g, '$1'), address } : null;
  }
  return /^[^"<>@,;:()[\]\\\s]+(\s+[^"<>@,;:()[\]\\\s]+)*$/.test(raw) ? { name: raw, address } : null;
}

/** Was an einem Absender nicht stimmt: Form, Länge des Anzeigenamens (höchstens 64 Zeichen) oder keiner. */
export function senderProblem(value: string): 'invalid' | 'name' | undefined {
  const parsed = parseSender(value);
  if (parsed === null) return 'invalid';
  return charCount(parsed.name) > LIMITS.personName ? 'name' : undefined;
}

/** Linux-Benutzername wie das Backend ihn annimmt (`ValidLinuxUsername`): Buchstabe oder _ zuerst, dann a–z, 0–9, _ und -. */
export function isLinuxUser(value: string): boolean {
  return value.length <= LIMITS.linuxUser && /^[a-z_][a-z0-9_-]*$/.test(value);
}

/** http(s)-Adresse ohne Leerzeichen und Anführungszeichen. */
export function isHttpUrl(value: string): boolean {
  if (!/^https?:\/\/[^\s'"`\\]+$/.test(value) || charCount(value) > LIMITS.path) return false;
  try {
    return new URL(value).hostname !== '';
  } catch {
    return false;
  }
}

/** Einträge einer Liste, die durch Kommas, Semikolons oder Leerraum getrennt ist. */
export function splitNames(text: string): string[] {
  return text.split(/[\s,;]+/).filter(Boolean);
}

/** Paketname wie das Backend ihn für apt, dnf und Co. zulässt (nach Kleinschreibung). */
export function isPackageName(value: string): boolean {
  return /^[a-z0-9][a-z0-9+._-]*$/.test(value.toLowerCase());
}

/** Die Einträge einer Paketliste, die kein Paketname sind. */
export function invalidPackages(text: string): string[] {
  return splitNames(text).filter((name) => !isPackageName(name));
}

/** Name eines Compose-Projekts oder Containers. */
export function isComposeName(value: string): boolean {
  return /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(value);
}

/** Domain für DNS-Tests: Buchstaben, Ziffern, Punkt, Bindestrich, Unterstrich. */
export function isDnsDomain(value: string): boolean {
  return /^[A-Za-z0-9._-]{1,253}$/.test(value);
}

/** Die Einträge einer Domainliste (Komma, Leerraum, Zeilen), die keine Domain sind. */
export function invalidDomains(text: string): string[] {
  return splitList(text).filter((entry) => !isDnsDomain(entry));
}

/** Absoluter Pfad, den das Backend für Verzeichnisse annimmt: kein `..`, keine Zeichen mit Bedeutung in der Shell. */
export function isSafeAbsolutePath(value: string): boolean {
  if (!value.startsWith('/') || /[\u0000-\u001f"'`$;|&<>*?]/.test(value)) return false;
  return !value.split('/').includes('..');
}

/** Zeitzone wie `Europe/Berlin` oder `UTC`. */
export function isTimezone(value: string): boolean {
  return /^[A-Za-z][A-Za-z0-9+_-]*(\/[A-Za-z0-9+._-]+){0,2}$/.test(value);
}

/** Ganze Zahl in Ziffern, im Bereich [min, max]; leer und alles andere gilt als ungültig. */
export function isWholeNumber(value: string | number, min: number, max: number): boolean {
  const text = String(value).trim();
  return /^\d{1,9}$/.test(text) && Number(text) >= min && Number(text) <= max;
}
