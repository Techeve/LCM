// Regeltypen, Formular und Prüfung. Reine Funktionen ohne DOM, damit sie testbar bleiben.
// Welche Typen es gibt und welche als Grundsatz-Regel taugen, bestimmt das Backend
// (`internal/core/services/group_service.go`); die Oberfläche bietet nur an, was dort durchgeht.

import type { Rule, RuleBody } from '../../api/groups';
import { LIMITS, charCount, hasControlChars, invalidDomains, invalidPackages, isAddressOrNet, isComposeName, isPort, splitList } from '../../lib/validate';

export type RuleField = 'none' | 'script' | 'packages' | 'firewall' | 'custom' | 'apt-proxy' | 'docker-scope' | 'dns-domains';
export type RuleCategory = 'packages' | 'restart' | 'docker' | 'checks' | 'config' | 'own';
/** Was die Regel auf dem Server bewirkt: nur lesen, Zustand abgleichen, Pakete/Container ändern, neu starten. */
export type RuleEffect = 'read' | 'config' | 'change' | 'maybe-reboot' | 'reboot';
export type Trigger = 'enforce' | number;

export interface RuleTypeInfo {
  id: string;
  /** Übersetzungsschlüssel ohne Endung; `.label` und `.help` hängen daran. */
  key: string;
  field: RuleField;
  category: RuleCategory;
  effect: RuleEffect;
}

/** Typen, die der Benutzer anlegen kann, in der Reihenfolge der Auswahl. */
export const RULE_TYPES: RuleTypeInfo[] = [
  { id: 'update', key: 'rules.type.update', field: 'none', category: 'packages', effect: 'change' },
  { id: 'security', key: 'rules.type.security', field: 'none', category: 'packages', effect: 'change' },
  { id: 'packages', key: 'rules.type.packages', field: 'packages', category: 'packages', effect: 'change' },
  { id: 'package-scan', key: 'rules.type.packageScan', field: 'none', category: 'packages', effect: 'read' },
  { id: 'autoremove', key: 'rules.type.autoremove', field: 'none', category: 'packages', effect: 'change' },
  { id: 'reboot-if-needed', key: 'rules.type.rebootIfNeeded', field: 'none', category: 'restart', effect: 'maybe-reboot' },
  { id: 'reboot', key: 'rules.type.reboot', field: 'none', category: 'restart', effect: 'reboot' },
  { id: 'docker-compose-update', key: 'rules.type.dockerComposeUpdate', field: 'docker-scope', category: 'docker', effect: 'change' },
  { id: 'docker-container-update', key: 'rules.type.dockerContainerUpdate', field: 'docker-scope', category: 'docker', effect: 'change' },
  { id: 'docker-update-unused', key: 'rules.type.dockerUpdateUnused', field: 'none', category: 'docker', effect: 'change' },
  { id: 'docker-prune', key: 'rules.type.dockerPrune', field: 'none', category: 'docker', effect: 'change' },
  { id: 'deep-scan', key: 'rules.type.deepScan', field: 'none', category: 'checks', effect: 'read' },
  { id: 'dns-test', key: 'rules.type.dnsTest', field: 'dns-domains', category: 'checks', effect: 'read' },
  { id: 'health', key: 'rules.type.health', field: 'none', category: 'checks', effect: 'read' },
  { id: 'sync', key: 'rules.type.sync', field: 'none', category: 'checks', effect: 'read' },
  { id: 'firewall', key: 'rules.type.firewall', field: 'firewall', category: 'config', effect: 'config' },
  { id: 'apt-proxy', key: 'rules.type.aptProxy', field: 'apt-proxy', category: 'config', effect: 'config' },
  { id: 'acl-setup', key: 'rules.type.aclSetup', field: 'none', category: 'config', effect: 'config' },
  { id: 'perm-sync', key: 'rules.type.permSync', field: 'none', category: 'config', effect: 'config' },
  { id: 'script', key: 'rules.type.script', field: 'script', category: 'own', effect: 'change' },
  { id: 'custom', key: 'rules.type.custom', field: 'custom', category: 'own', effect: 'change' },
];

/** Typen, die nur das System anlegt. Sie erscheinen in Listen, lassen sich aber nicht neu anlegen. */
const SYSTEM_TYPES: RuleTypeInfo[] = [
  { id: 'backup', key: 'rules.type.backup', field: 'none', category: 'checks', effect: 'read' },
  { id: 'cleanup', key: 'rules.type.cleanup', field: 'none', category: 'checks', effect: 'read' },
  { id: 'cve-scan', key: 'rules.type.cveScan', field: 'none', category: 'checks', effect: 'read' },
  { id: 'docker-check', key: 'rules.type.dockerCheck', field: 'none', category: 'checks', effect: 'read' },
  { id: 'app-check', key: 'rules.type.appCheck', field: 'none', category: 'checks', effect: 'read' },
  { id: 'alert-check', key: 'rules.type.alertCheck', field: 'none', category: 'checks', effect: 'read' },
];

/** Typen mit Soll-Zustand, die als Grundsatz-Regel taugen (bei jeder Verbindung geprüft). */
export const ENFORCE_TYPE_IDS = ['firewall', 'apt-proxy', 'acl-setup', 'perm-sync'];

/** Nur bei diesen Typen können zwei Gruppen einander widersprechen; der Vorrang entscheidet. */
export const CONFLICT_TYPE_IDS = ['firewall', 'apt-proxy'];

const BY_ID = new Map([...RULE_TYPES, ...SYSTEM_TYPES].map((type) => [type.id, type]));

export function typeInfo(id: string): RuleTypeInfo | undefined {
  return BY_ID.get(id);
}

/** Typen, die an diesem Auslöser wählbar sind. Firewall gibt es nur als Grundsatz-Regel. */
export function typesFor(trigger: Trigger | null): RuleTypeInfo[] {
  if (trigger === 'enforce') return RULE_TYPES.filter((type) => ENFORCE_TYPE_IDS.includes(type.id));
  return RULE_TYPES.filter((type) => type.id !== 'firewall');
}

// ---- Firewall ---------------------------------------------------------------------------

/** Das Backend kennt je Regel nur tcp oder udp; „both“ gibt es nur in der Oberfläche und wird beim Speichern zu zwei Regeln. */
export type FirewallProto = 'tcp' | 'udp' | 'both';
export type IpVersion = 'any' | 'v4' | 'v6';

// Kürzel sind Fachbegriffe und werden nicht übersetzt.
export const FIREWALL_PROTOCOLS: ReadonlyArray<{ value: FirewallProto; label: string; suffix: string }> = [
  { value: 'tcp', label: 'TCP', suffix: 'tcp' },
  { value: 'udp', label: 'UDP', suffix: 'udp' },
  { value: 'both', label: 'TCP/UDP', suffix: 'tcp+udp' },
];
export const IP_VERSIONS: ReadonlyArray<{ value: IpVersion; label: string }> = [
  { value: 'any', label: 'IPv4/6' },
  { value: 'v4', label: 'IPv4' },
  { value: 'v6', label: 'IPv6' },
];

/** Das Protokoll für Kurzfassungen: „tcp“, „udp“ oder „tcp+udp“. */
export function protoSuffix(proto: FirewallProto): string {
  return FIREWALL_PROTOCOLS.find((entry) => entry.value === proto)?.suffix ?? proto;
}

export interface FirewallRow {
  port: string;
  proto: FirewallProto;
  ip_version: IpVersion;
  allowlist_ids: number[];
  /** Eigene Quell-IPs oder Netze, mit Komma oder Leerzeichen getrennt. */
  source_ips: string;
  comment: string;
}

export function emptyFirewallRow(): FirewallRow {
  return { port: '', proto: 'tcp', ip_version: 'any', allowlist_ids: [], source_ips: '', comment: '' };
}

export function splitSources(text: string): string[] {
  return splitList(text);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object';
}

function rowFromJson(entry: unknown): FirewallRow {
  const r = isRecord(entry) ? entry : {};
  const ids = Array.isArray(r.allowlist_ids) ? r.allowlist_ids.filter((id): id is number => typeof id === 'number') : [];
  const sources = Array.isArray(r.source_ips) ? r.source_ips.filter((ip): ip is string => typeof ip === 'string') : [];
  return {
    port: r.port === undefined ? '' : String(r.port),
    proto: r.proto === 'udp' ? 'udp' : 'tcp',
    ip_version: r.ip_version === 'v4' || r.ip_version === 'v6' ? r.ip_version : 'any',
    allowlist_ids: ids,
    source_ips: sources.join(', '),
    comment: typeof r.comment === 'string' ? r.comment : '',
  };
}

const sortedIds = (ids: number[]) => [...ids].sort((a, b) => a - b).join(',');

/** Gleiche Freigabe bis auf das Protokoll? Nur dann lassen sich TCP- und UDP-Regel zu einer Zeile verbinden. */
function sameExceptProto(a: FirewallRow, b: FirewallRow): boolean {
  return (
    a.port === b.port &&
    a.ip_version === b.ip_version &&
    sortedIds(a.allowlist_ids) === sortedIds(b.allowlist_ids) &&
    a.source_ips === b.source_ips &&
    a.comment === b.comment
  );
}

/**
 * Fasst eine TCP- und eine UDP-Regel mit sonst gleichen Angaben zu einer Zeile „both“ zusammen. Die Zeile steht
 * dort, wo die TCP-Regel stand; die Reihenfolge der übrigen Zeilen bleibt. Das Backend sortiert Regeln nach Port,
 * Protokoll und IP-Version, die Partner liegen also nicht unbedingt nebeneinander.
 */
export function mergeProtocols(rows: FirewallRow[]): FirewallRow[] {
  const taken = new Set<number>();
  const merged: FirewallRow[] = [];
  rows.forEach((row, index) => {
    if (taken.has(index)) return;
    if (row.proto !== 'tcp') return void merged.push(row);
    const partner = rows.findIndex((other, i) => i > index && !taken.has(i) && other.proto === 'udp' && sameExceptProto(row, other));
    if (partner < 0) return void merged.push(row);
    taken.add(partner);
    merged.push({ ...row, proto: 'both' });
  });
  return merged;
}

/** Das Kommando einer Firewall-Regel: JSON-Liste oder, aus alter Zeit, eine Portliste „80,443“. */
export function parseFirewall(command: string): FirewallRow[] {
  const trimmed = command.trim();
  if (trimmed.startsWith('[')) {
    try {
      const parsed: unknown = JSON.parse(trimmed);
      return Array.isArray(parsed) ? mergeProtocols(parsed.map(rowFromJson)) : [];
    } catch {
      return [];
    }
  }
  return trimmed
    .split(',')
    .map((part) => Number(part.trim()))
    .filter((port) => Number.isInteger(port) && port > 0)
    .map((port) => ({ ...emptyFirewallRow(), port: String(port) }));
}

/** Die Protokolle, die eine Zeile an das Backend schickt: „both“ sind TCP und UDP. */
export function protocolsOf(proto: FirewallProto): Array<'tcp' | 'udp'> {
  return proto === 'both' ? ['tcp', 'udp'] : [proto];
}

export interface FirewallRulePart {
  port: number;
  proto: 'tcp' | 'udp';
  ip_version: IpVersion;
  allowlist_ids?: number[];
  source_ips?: string[];
  comment?: string;
}

/** Die Regeln für das Backend. Leere Quellen und Bemerkungen fehlen ganz: eine leere Quellenliste lehnt das Backend ab. */
export function firewallRules(rows: readonly FirewallRow[]): FirewallRulePart[] {
  return rows.flatMap((row) => {
    const sources = splitSources(row.source_ips);
    const comment = row.comment.trim();
    return protocolsOf(row.proto).map((proto) => ({
      port: Number(row.port),
      proto,
      ip_version: row.ip_version,
      ...(row.allowlist_ids.length > 0 ? { allowlist_ids: row.allowlist_ids } : {}),
      ...(sources.length > 0 ? { source_ips: sources } : {}),
      ...(comment ? { comment } : {}),
    }));
  });
}

/** JSON für das Kommando. */
export function serializeFirewall(rows: FirewallRow[]): string {
  return JSON.stringify(firewallRules(rows));
}

export interface FirewallRowErrors {
  port: boolean;
  comment: boolean;
  /** Einträge, die weder IP-Adresse noch Netz sind. */
  badSources: string[];
  /** Einträge der anderen Adressfamilie, als die feste IP-Version der Regel zulässt. */
  wrongFamily: string[];
  tooManySources: boolean;
  /** Irgendein Fehler bei den Quellen. */
  sources: boolean;
}

/** IPv4 oder IPv6, wie das Backend es an `:` erkennt. */
const familyOf = (entry: string): 'v4' | 'v6' => (entry.includes(':') ? 'v6' : 'v4');

/** Früh melden, was das Backend ohnehin ablehnt: Port 1 bis 65535, Quellen als IP oder Netz, Bemerkung nicht zu lang. */
export function firewallErrors(rows: FirewallRow[]): FirewallRowErrors[] {
  return rows.map((row) => {
    const entries = splitSources(row.source_ips);
    const badSources = entries.filter((entry) => !isAddressOrNet(entry));
    const wrongFamily = row.ip_version === 'any' ? [] : entries.filter((entry) => isAddressOrNet(entry) && familyOf(entry) !== row.ip_version);
    const tooManySources = entries.length > LIMITS.firewallSources;
    return {
      port: !isPort(row.port),
      comment: charCount(row.comment.trim()) > LIMITS.firewallComment,
      badSources,
      wrongFamily,
      tooManySources,
      sources: badSources.length > 0 || wrongFamily.length > 0 || tooManySources,
    };
  });
}

export function hasRowErrors(error: FirewallRowErrors): boolean {
  return error.port || error.comment || error.sources;
}

export function hasFirewallErrors(rows: FirewallRow[]): boolean {
  return firewallErrors(rows).some(hasRowErrors);
}

/** Kurzfassung für Listen: „80/tcp, 443/tcp+udp (Webshop)“. Leer, wenn es keine Freigaben gibt. */
export function firewallSummary(command: string): string {
  return parseFirewall(command)
    .map((row) => `${row.port}/${protoSuffix(row.proto)}${row.comment ? ` (${row.comment})` : ''}`)
    .join(', ');
}

// ---- Formular ---------------------------------------------------------------------------

export interface RuleForm {
  /** `null` beim Anlegen. */
  id: number | null;
  name: string;
  type: string;
  /** Beim Anlegen frei wählbar, beim Bearbeiten fest. `null`: noch nicht gewählt. */
  trigger: Trigger | null;
  /** Kommando für Skript, Pakete, Custom-Aktion (ID), APT-Cache (ID), Docker-Auswahl und Test-Domains. */
  command: string;
  firewall: FirewallRow[];
}

export function emptyRuleForm(trigger: Trigger | null): RuleForm {
  const first = typesFor(trigger)[0];
  return { id: null, name: '', type: first?.id ?? 'update', trigger, command: '', firewall: [] };
}

export function formFromRule(rule: Rule): RuleForm {
  return {
    id: rule.id,
    name: rule.name,
    type: rule.type,
    trigger: rule.enforce ? 'enforce' : rule.schedule_id,
    command: rule.type === 'firewall' ? '' : rule.command,
    firewall: rule.type === 'firewall' ? parseFirewall(rule.command) : [],
  };
}

/** Beim Wechsel des Auslösers oder Typs: Eingaben des alten Typs verwerfen, einen gültigen Typ halten. */
export function withTrigger(form: RuleForm, trigger: Trigger | null): RuleForm {
  const allowed = typesFor(trigger);
  const type = allowed.some((entry) => entry.id === form.type) ? form.type : (allowed[0]?.id ?? form.type);
  return type === form.type ? { ...form, trigger } : { ...form, trigger, type, command: '', firewall: [] };
}

export function withType(form: RuleForm, type: string): RuleForm {
  return type === form.type ? form : { ...form, type, command: '', firewall: [] };
}

export interface RuleFormErrors {
  name?: string;
  trigger?: string;
  command?: string;
  firewall?: boolean;
}

/** Platzhalter für die Fehlertexte des Kommando-Felds: die ungültigen Einträge und die Grenze. */
export function commandParams(form: RuleForm): { list: string; max: number } {
  const field = typeInfo(form.type)?.field;
  const bad = field === 'packages' ? invalidPackages(form.command) : field === 'dns-domains' ? invalidDomains(form.command) : [];
  return { list: bad.length > 3 ? `${bad.slice(0, 3).join(', ')} …` : bad.join(', '), max: LIMITS.script };
}

function commandError(form: RuleForm): string | undefined {
  const field = typeInfo(form.type)?.field ?? 'none';
  const command = form.command;
  if (field === 'script' || field === 'packages' || field === 'dns-domains') {
    if (charCount(command) > LIMITS.script) return 'check.tooLong';
    if (hasControlChars(command, true)) return 'check.controlChars';
  }
  switch (field) {
    case 'script':
      return command.trim() === '' ? 'rules.error.scriptRequired' : undefined;
    case 'packages':
      if (command.trim() === '') return 'rules.error.packagesRequired';
      return invalidPackages(command).length > 0 ? 'rules.error.packagesInvalid' : undefined;
    case 'custom':
      return command === '' ? 'rules.error.customRequired' : undefined;
    case 'docker-scope':
      return command.trim() !== '' && !isComposeName(command.trim()) ? 'rules.error.dockerScopeInvalid' : undefined;
    case 'dns-domains':
      return invalidDomains(command).length > 0 ? 'rules.error.domainsInvalid' : undefined;
    default:
      return undefined;
  }
}

export function validateRuleForm(form: RuleForm): RuleFormErrors {
  const errors: RuleFormErrors = {};
  const name = form.name.trim();
  if (name === '') errors.name = 'rules.error.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'rules.error.nameTooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  if (form.trigger === null) errors.trigger = 'rules.error.triggerRequired';
  const command = commandError(form);
  if (command) errors.command = command;
  if (typeInfo(form.type)?.field === 'firewall' && hasFirewallErrors(form.firewall)) errors.firewall = true;
  return errors;
}

export function isRuleFormValid(form: RuleForm): boolean {
  return Object.keys(validateRuleForm(form)).length === 0;
}

function commandOf(form: RuleForm): string {
  const field = typeInfo(form.type)?.field ?? 'none';
  if (field === 'firewall') return serializeFirewall(form.firewall);
  if (field === 'none') return '';
  return form.command.trim();
}

/** Der Körper für `groupsApi.defineRule`. Nur nach `isRuleFormValid` aufrufen. */
export function ruleBody(form: RuleForm): RuleBody {
  return { name: form.name.trim(), type: form.type, command: commandOf(form), trigger: form.trigger ?? 'enforce' };
}

/** Der Körper für `groupsApi.updateRule`: Name und Kommando. */
export function ruleUpdateBody(form: RuleForm): { name: string; command: string } {
  return { name: form.name.trim(), command: commandOf(form) };
}

/** Hat sich gegenüber dem Ausgangszustand etwas geändert? */
export function ruleFormChanged(initial: RuleForm, current: RuleForm): boolean {
  return JSON.stringify(initial) !== JSON.stringify(current);
}

/** Zusammenfassung der Auswirkungen einer Menge von Regeln auf die Server. */
export interface RuleEffects {
  reboot: boolean;
  maybeReboot: boolean;
  readOnly: boolean;
  /** Nichts, was sich nicht wieder zurücknehmen ließe (Lesen oder Soll-Zustand abgleichen). */
  reversible: boolean;
}

export function effectsOf(rules: Pick<Rule, 'type'>[]): RuleEffects {
  const effects = rules.map((rule) => typeInfo(rule.type)?.effect ?? 'change');
  return {
    reboot: effects.includes('reboot'),
    maybeReboot: effects.includes('maybe-reboot'),
    readOnly: effects.every((effect) => effect === 'read'),
    reversible: effects.every((effect) => effect === 'read' || effect === 'config'),
  };
}
