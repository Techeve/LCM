// DNS-01 bei Let's Encrypt: Entwurf der Anbieterwerte, Geheimfeld-Semantik, Pflichtfelder und der Rumpf der Anfrage.
// Welche Felder ein Anbieter hat, sagt die API; Beschriftungen kommen aus den Sprachdateien.

import type { AcmeState, DnsBody, DnsField, DnsProvider } from '../../../api/endpoints';
import type { FieldWidth } from '../../../components/field-styles';
import { splitList } from '../../../lib/validate';

/** Wie viele Prüf-Nameserver die API annimmt (maxDNSNameservers im Server). */
export const MAX_NAMESERVERS = 3;

/** Felder, die fast alle Anbieter kennen und die überall gleich heißen. */
const SHARED_FIELDS = ['propagation_timeout', 'polling_interval'];

export interface DnsDraft {
  /** Leer, solange keiner gewählt ist. */
  provider: string;
  /** Text, Auswahl und Zahl als Text; ein Schalter als `true` oder `false`. */
  values: Record<string, string>;
  /**
   * Neu eingegebene Geheimnisse. Ein Name ohne Eintrag behält den gespeicherten Wert, ein leerer Eintrag löscht
   * ihn beim Speichern.
   */
  secrets: Record<string, string | undefined>;
  /** Ein Server je Zeile. */
  nameservers: string;
}

export function providerOf(state: AcmeState, id: string): DnsProvider | undefined {
  return state.dnsProviders.find((provider) => provider.id === id);
}

export function dnsDraftOf(state: AcmeState): DnsDraft {
  return {
    provider: state.dnsProvider,
    values: Object.fromEntries(Object.entries(state.dnsValues).map(([name, value]) => [name, String(value)])),
    secrets: {},
    nameservers: state.dnsCheckNameservers.join('\n'),
  };
}

/** Ein anderer Anbieter bringt seine eigenen Felder mit: Die Eingaben des alten verfallen. */
export function switchProvider(dns: DnsDraft, state: AcmeState, id: string): DnsDraft {
  if (id === state.dnsProvider) return { ...dnsDraftOf(state), nameservers: dns.nameservers };
  return { ...dns, provider: id, values: {}, secrets: {} };
}

export function parseNameservers(input: string): string[] {
  return [...new Set(splitList(input))];
}

/** Zu diesem Geheimfeld liegt beim selben Anbieter ein gespeicherter Wert. */
export function secretStored(dns: DnsDraft, state: AcmeState, name: string): boolean {
  return dns.provider === state.dnsProvider && state.dnsSecretsSet[name] === true;
}

function hasValue(dns: DnsDraft, state: AcmeState, field: DnsField): boolean {
  switch (field.type) {
    case 'secret': {
      const entered = dns.secrets[field.name];
      return entered === undefined ? secretStored(dns, state, field.name) : entered.trim() !== '';
    }
    case 'bool':
      return true;
    case 'select':
      return field.default !== '' || (dns.values[field.name] ?? '').trim() !== '';
    default:
      return (dns.values[field.name] ?? '').trim() !== '';
  }
}

/** Das erste Pflichtfeld des gewählten Anbieters ohne Wert; `null`, wenn alle gefüllt sind. */
export function missingDnsField(dns: DnsDraft, state: AcmeState): string | null {
  const fields = providerOf(state, dns.provider)?.fields ?? [];
  return fields.find((field) => field.required && !hasValue(dns, state, field))?.name ?? null;
}

function valueOf(dns: DnsDraft, field: DnsField): string | number | boolean | undefined {
  const entered = (dns.values[field.name] ?? '').trim();
  switch (field.type) {
    case 'secret':
      return dns.secrets[field.name]?.trim();
    case 'bool':
      return dns.values[field.name] === 'true';
    case 'number':
      return entered === '' ? undefined : Number(entered);
    default:
      return entered === '' ? undefined : entered;
  }
}

/** Was die API über DNS-01 wissen muss; leere Felder bleiben weg, ein Geheimfeld ohne Eingabe auch (es bleibt dann). */
export function dnsBodyOf(dns: DnsDraft, state: AcmeState): DnsBody {
  const values: DnsBody['dns_values'] = {};
  for (const field of providerOf(state, dns.provider)?.fields ?? []) {
    const value = valueOf(dns, field);
    if (value !== undefined) values[field.name] = value;
  }
  return { dns_provider: dns.provider, dns_values: values, dns_check_nameservers: parseNameservers(dns.nameservers) };
}

export function dnsChanged(dns: DnsDraft, state: AcmeState): boolean {
  return JSON.stringify(dnsBodyOf(dns, state)) !== JSON.stringify(dnsBodyOf(dnsDraftOf(state), state));
}

/** Schlüsselteil der Sprachdatei: je Anbieter eigene Texte, die gemeinsamen Felder einmal. */
export function fieldKey(provider: string, name: string): string {
  return `settings.acme.dns.field.${SHARED_FIELDS.includes(name) ? 'common' : provider}.${name}`;
}

export function fieldWidth(field: DnsField): FieldWidth {
  if (field.type === 'number') return 'xs';
  if (field.type === 'secret') return 'l';
  return /url|base|endpoint/.test(field.name) ? 'l' : 'm';
}

/** Mehrere Namen brauchen bei diesen Anbietern je Name einen eigenen Durchgang, das dauert. */
export function runsSlowly(provider: string, nameCount: number): boolean {
  return nameCount > 1 && (provider === 'rfc2136' || provider === 'exec');
}
