// Passwort-Policy für die SOFORTIGE Rückmeldung beim Tippen.
//
// WICHTIG: Das hier ist KEIN Sicherheitsmerkmal. Verbindlich ist
// ausschließlich die serverseitige Prüfung in
// internal/core/services/password_policy.go - diese Datei spiegelt deren
// Regeln nur, damit der Nutzer nicht erst beim Absenden erfährt, was fehlt.
// Beide Seiten verwenden dieselben Problem-Codes; die Übersetzung liegt in
// den Sprachkatalogen unter `password.problem.*`.
//
// Wird die Server-Policy geändert, MUSS diese Datei nachgezogen werden.

export const PASSWORD_MIN_LENGTH = 12;
export const PASSWORD_MAX_LENGTH = 200;
const PASSPHRASE_LENGTH = 20;
const MIN_UNIQUE = 6;

const CONTEXT_WORDS = [
  'lcm', 'techeve', 'admin', 'administrator', 'root', 'server', 'linux',
  'passwort', 'password', 'kennwort', 'login', 'secret', 'geheim',
  'backup', 'sicherung',
];

// Kuratierte Liste der häufigsten Standard-Passwörter (Spiegel der
// Server-Liste; der Server bleibt die Autorität, hier genügt der Kern).
const COMMON = new Set([
  'password', 'passwort', 'kennwort', 'passwd', 'qwerty', 'qwertz',
  'qwertzuiop', 'qwertyuiop', 'asdfgh', 'asdfghjkl', 'yxcvbnm', 'zxcvbnm',
  'letmein', 'welcome', 'willkommen', 'monkey', 'dragon', 'master',
  'sunshine', 'princess', 'football', 'iloveyou', 'ichliebedich', 'admin',
  'administrator', 'root', 'toor', 'superman', 'batman', 'starwars',
  'hallo', 'hello', 'schatz', 'sommer', 'winter', 'herbst', 'deutschland',
  'berlin', 'muenchen', 'hamburg', 'bayern', 'geheim', 'test', 'testtest',
  'demo', 'default', 'changeme', 'aendermich', 'secret', 'geheimnis',
  'computer', 'internet', 'system', 'server', 'linux', 'ubuntu', 'debian',
  'mustermann', 'maxmustermann', 'abcdef', 'abcdefg', 'qazwsx', 'abc123',
  'passwort1', 'password1', '123456', '12345678', '123456789', '1234567890',
  '111111', '000000', '121212', 'aaaaaa',
]);

const SEQUENCES = [
  'abcdefghijklmnopqrstuvwxyz',
  '0123456789',
  'qwertzuiopü', 'asdfghjklöä', 'yxcvbnm',
  'qwertyuiop', 'asdfghjkl', 'zxcvbnm',
  '1qaz2wsx', 'qazwsxedc',
];

const LEET = { '@': 'a', '4': 'a', '8': 'b', '(': 'c', '3': 'e', '6': 'g',
  '1': 'i', '!': 'i', '|': 'i', '0': 'o', '5': 's', '$': 's', '7': 't',
  '+': 't', '2': 'z', '9': 'g' };

const stripToAlnum = (s) => s.replace(/[^\p{L}\p{N}]/gu, '');
const applyLeet = (s) => s.replace(/[@48(36 1!|05$7+29]/g, (c) => LEET[c] ?? c);

/** Vergleichsformen eines Passworts (siehe passwordCandidates im Backend). */
function candidates(password) {
  const plain = stripToAlnum(password.trim().toLowerCase());
  const trimmed = plain.replace(/\d+$/, '');
  return [plain, trimmed, stripToAlnum(applyLeet(plain)), stripToAlnum(applyLeet(trimmed))]
    .filter(Boolean);
}

function countClasses(password) {
  let lower = false, upper = false, digit = false, other = false;
  for (const ch of password) {
    if (/\p{Ll}/u.test(ch)) lower = true;
    else if (/\p{Lu}/u.test(ch)) upper = true;
    else if (/\p{N}/u.test(ch)) digit = true;
    else other = true;
  }
  return [lower, upper, digit, other].filter(Boolean).length;
}

function hasSequenceRun(password, n = 4) {
  const lower = password.toLowerCase();
  for (const seq of SEQUENCES) {
    for (let i = 0; i + n <= seq.length; i++) {
      const window = seq.slice(i, i + n);
      const reversed = [...window].reverse().join('');
      if (lower.includes(window) || lower.includes(reversed)) return true;
    }
  }
  return false;
}

function hasRepeatRun(password, n = 3) {
  const chars = [...password.toLowerCase()];
  let run = 1;
  for (let i = 1; i < chars.length; i++) {
    if (chars[i] === chars[i - 1]) {
      if (++run >= n) return true;
    } else run = 1;
  }
  return false;
}

function isRepeatedBlock(password) {
  const s = password.toLowerCase();
  for (let size = 1; size <= Math.floor(s.length / 2); size++) {
    if (s.length % size !== 0) continue;
    if (s.slice(0, size).repeat(s.length / size) === s) return true;
  }
  return false;
}

/**
 * Prüft ein Passwort gegen die (gespiegelte) Policy.
 * @param {string} password
 * @param {{username?:string,email?:string,firstName?:string,lastName?:string}} identity
 * @returns {{ok:boolean, score:number, codes:string[]}} codes = Problem-Codes
 */
export function checkPassword(password, identity = {}) {
  const codes = [];
  const chars = [...(password ?? '')];
  const length = chars.length;

  if (length < PASSWORD_MIN_LENGTH) codes.push('too_short');
  else if (length > PASSWORD_MAX_LENGTH) codes.push('too_long');
  if (codes.length) return { ok: false, score: 0, codes };

  if (password.trim() !== password) codes.push('whitespace_edges');

  const required = length >= PASSPHRASE_LENGTH ? 2 : 3;
  if (countClasses(password) < required) codes.push('needs_classes');

  if (new Set(chars.map((c) => c.toLowerCase())).size < MIN_UNIQUE) codes.push('low_variety');

  // Personenbezug: Benutzername, Vor-/Nachname, lokaler Teil der E-Mail.
  const haystack = stripToAlnum(applyLeet(password.toLowerCase()));
  const local = (identity.email ?? '').split('@')[0] ?? '';
  const tokens = [identity.username, identity.firstName, identity.lastName, local,
    ...local.split(/[^\p{L}\p{N}]+/u)];
  for (const raw of tokens) {
    const token = stripToAlnum(applyLeet(String(raw ?? '').toLowerCase()));
    if (token.length >= 3 && haystack.includes(token)) { codes.push('contains_identity'); break; }
  }

  // Naheliegende Begriffe dürfen das Passwort nicht dominieren.
  const base = stripToAlnum(applyLeet(password.toLowerCase()));
  for (const word of CONTEXT_WORDS) {
    if (base.includes(word) && base.split(word).join('').length < 8) {
      codes.push('context_word');
      break;
    }
  }

  if (candidates(password).some((c) => COMMON.has(c))) codes.push('common_password');
  if (hasSequenceRun(password)) codes.push('sequence');
  if (hasRepeatRun(password) || isRepeatedBlock(password)) codes.push('repeat');

  let score;
  if (codes.length >= 3) score = 0;
  else if (codes.length > 0) score = 1;
  else {
    score = 2;
    if (length >= 24) score += 2;
    else if (length >= 16) score += 1;
    if (countClasses(password) === 4) score += 1;
    score = Math.min(score, 4);
  }
  return { ok: codes.length === 0, score, codes };
}
