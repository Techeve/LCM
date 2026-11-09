// Passwort-Regeln für die sofortige Rückmeldung beim Tippen. Kein Sicherheitsmerkmal:
// verbindlich prüft allein der Server (internal/core/services/password_policy.go).
// Diese Datei spiegelt dessen Regeln und Problem-Codes; ändert sich die Server-Regel,
// muss sie nachgezogen werden.

import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';

export const PASSWORD_MIN_LENGTH = 12;
export const PASSWORD_MAX_LENGTH = LIMITS.newPassword;
const PASSPHRASE_LENGTH = 20;
const MIN_UNIQUE = 6;

export type PasswordProblem =
  | 'too_short'
  | 'too_long'
  | 'control_chars'
  | 'whitespace_edges'
  | 'needs_classes'
  | 'low_variety'
  | 'contains_identity'
  | 'context_word'
  | 'common_password'
  | 'sequence'
  | 'repeat';

/** Angaben zur Person, die nicht im Passwort stehen dürfen. */
export interface PasswordIdentity {
  username?: string;
  email?: string;
  firstName?: string;
  lastName?: string;
}

export interface PasswordCheck {
  ok: boolean;
  /** Stärke von 0 bis 4, wie der Server sie meldet: mit Regelverstoß nie über 1. */
  score: number;
  problems: PasswordProblem[];
}

const CONTEXT_WORDS = [
  'lcm', 'techeve', 'admin', 'administrator', 'root', 'server', 'linux',
  'passwort', 'password', 'kennwort', 'login', 'secret', 'geheim', 'backup', 'sicherung',
];

const COMMON = new Set([
  'password', 'passwort', 'kennwort', 'passwd', '123456', '12345678', '123456789', '1234567890',
  'qwerty', 'qwertz', 'qwertzuiop', 'qwertyuiop', 'asdfgh', 'asdfghjkl', 'yxcvbnm', 'zxcvbnm',
  'letmein', 'welcome', 'willkommen', 'monkey', 'dragon', 'master', 'sunshine', 'princess',
  'football', 'baseball', 'iloveyou', 'ichliebedich', 'admin', 'administrator', 'root', 'toor',
  'superman', 'batman', 'trustno', 'whatever', 'starwars', 'pokemon', 'hallo', 'hello',
  'schatz', 'sommer', 'winter', 'fruehling', 'herbst', 'deutschland', 'berlin', 'muenchen',
  'hamburg', 'bayern', 'borussia', 'schalke', 'geheim', 'test', 'testtest', 'demo', 'default',
  'changeme', 'aendermich', 'secret', 'geheimnis', 'willkommen1', 'computer', 'internet', 'system',
  'server', 'linux', 'ubuntu', 'debian', 'mustermann', 'maxmustermann', 'abcdef', 'abcdefg',
  'qazwsx', 'zaqwsx', 'aaaaaa', '111111', '000000', '121212', 'abc123', 'a1b2c3',
  'passwort1', 'password1', 'michael', 'thomas', 'andreas', 'stefan', 'sabine', 'julia',
]);

const SEQUENCES = [
  'abcdefghijklmnopqrstuvwxyz', '0123456789',
  'qwertzuiopü', 'asdfghjklöä', 'yxcvbnm', 'qwertyuiop', 'asdfghjkl', 'zxcvbnm', '1qaz2wsx', 'qazwsxedc',
];

const LEET: Record<string, string> = {
  '@': 'a', '4': 'a', '8': 'b', '(': 'c', '3': 'e', '6': 'g', '1': 'i', '!': 'i', '|': 'i',
  '0': 'o', '5': 's', '$': 's', '7': 't', '+': 't', '2': 'z', '9': 'g',
};

const stripToAlnum = (value: string) => value.replace(/[^\p{L}\p{N}]/gu, '');
const applyLeet = (value: string) => [...value].map((char) => LEET[char] ?? char).join('');

function isControl(char: string): boolean {
  const code = char.codePointAt(0) ?? 0;
  return code < 0x20 || (code >= 0x7f && code < 0xa0);
}

/** Vergleichsformen eines Passworts: klein, ohne Satzzeichen, ohne Ziffern am Ende, ohne Leetspeak. */
function candidates(password: string): string[] {
  const plain = stripToAlnum(password.trim().toLowerCase());
  const trimmed = plain.replace(/\d+$/, '');
  return [plain, trimmed, stripToAlnum(applyLeet(plain)), stripToAlnum(applyLeet(trimmed))].filter(Boolean);
}

function countClasses(password: string): number {
  const classes = new Set<string>();
  for (const char of password) {
    if (/\p{Ll}/u.test(char)) classes.add('lower');
    else if (/\p{Lu}/u.test(char)) classes.add('upper');
    else if (/\p{N}/u.test(char)) classes.add('digit');
    else classes.add('other');
  }
  return classes.size;
}

function hasSequenceRun(password: string, length = 4): boolean {
  const lower = password.toLowerCase();
  return SEQUENCES.some((sequence) => {
    const chars = [...sequence];
    for (let start = 0; start + length <= chars.length; start++) {
      const window = chars.slice(start, start + length);
      if (lower.includes(window.join('')) || lower.includes([...window].reverse().join(''))) return true;
    }
    return false;
  });
}

function hasRepeatRun(password: string, length = 3): boolean {
  const chars = [...password.toLowerCase()];
  let run = 1;
  for (let index = 1; index < chars.length; index++) {
    run = chars[index] === chars[index - 1] ? run + 1 : 1;
    if (run >= length) return true;
  }
  return false;
}

function isRepeatedBlock(password: string): boolean {
  const value = password.toLowerCase();
  for (let size = 1; size <= Math.floor(value.length / 2); size++) {
    if (value.length % size === 0 && value.slice(0, size).repeat(value.length / size) === value) return true;
  }
  return false;
}

function containsIdentity(password: string, identity: PasswordIdentity): boolean {
  const haystack = stripToAlnum(applyLeet(password.toLowerCase()));
  const local = (identity.email ?? '').split('@')[0] ?? '';
  const tokens = [identity.username, identity.firstName, identity.lastName, local, ...local.split(/[^\p{L}\p{N}]+/u)];
  return tokens.some((raw) => {
    const token = stripToAlnum(applyLeet((raw ?? '').toLowerCase()));
    return token.length >= 3 && haystack.includes(token);
  });
}

/** Naheliegende Begriffe dürfen vorkommen, das Passwort aber nicht beherrschen. */
function dominatedByContextWord(password: string): boolean {
  const base = stripToAlnum(applyLeet(password.toLowerCase()));
  return CONTEXT_WORDS.some((word) => base.includes(word) && base.split(word).join('').length < 8);
}

function scoreOf(password: string, problems: PasswordProblem[]): number {
  if (problems.length > 0) return problems.length >= 3 ? 0 : 1;
  const length = [...password].length;
  const lengthBonus = length >= 24 ? 2 : length >= 16 ? 1 : 0;
  return Math.min(4, 2 + lengthBonus + (countClasses(password) === 4 ? 1 : 0));
}

/** Prüft ein Passwort gegen die (gespiegelte) Regel des Servers. */
export function checkPassword(password: string, identity: PasswordIdentity = {}): PasswordCheck {
  const length = [...password].length;
  if (length < PASSWORD_MIN_LENGTH) return { ok: false, score: 0, problems: ['too_short'] };
  if (length > PASSWORD_MAX_LENGTH) return { ok: false, score: 0, problems: ['too_long'] };

  const problems: PasswordProblem[] = [];
  if ([...password].some(isControl)) problems.push('control_chars');
  if (password.trim() !== password) problems.push('whitespace_edges');
  if (countClasses(password) < (length >= PASSPHRASE_LENGTH ? 2 : 3)) problems.push('needs_classes');
  if (new Set([...password.toLowerCase()]).size < MIN_UNIQUE) problems.push('low_variety');
  if (containsIdentity(password, identity)) problems.push('contains_identity');
  if (dominatedByContextWord(password)) problems.push('context_word');
  if (candidates(password).some((candidate) => COMMON.has(candidate))) problems.push('common_password');
  if (hasSequenceRun(password)) problems.push('sequence');
  if (hasRepeatRun(password) || isRepeatedBlock(password)) problems.push('repeat');
  return { ok: problems.length === 0, score: scoreOf(password, problems), problems };
}

/** Übersetzungsschlüssel je Problem (als Literale, damit die Locale-Prüfung sie findet). */
export const PROBLEM_KEYS: Record<PasswordProblem, string> = {
  too_short: 'password.problem.too_short',
  too_long: 'password.problem.too_long',
  control_chars: 'password.problem.control_chars',
  whitespace_edges: 'password.problem.whitespace_edges',
  needs_classes: 'password.problem.needs_classes',
  low_variety: 'password.problem.low_variety',
  contains_identity: 'password.problem.contains_identity',
  context_word: 'password.problem.context_word',
  common_password: 'password.problem.common_password',
  sequence: 'password.problem.sequence',
  repeat: 'password.problem.repeat',
};

/** Stärke und Texte der verletzten Regeln für `lcm-password`; ein leeres Feld zeigt keine Gründe. */
export function passwordFeedback(password: string, identity: PasswordIdentity): { score: number; problems: string[] } {
  const check = checkPassword(password, identity);
  const problems = password && !check.ok ? check.problems.map((problem) => t(PROBLEM_KEYS[problem])) : [];
  return { score: check.score, problems };
}
