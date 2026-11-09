// Aus der Befehlsvorschau des Backends (`GET /servers/:id/actions/:action/preview`) wird, was die
// Erklärblase und der Bestätigungsdialog zeigen. Die Befehle kommen aus demselben Code, der sie
// später ausführt; hier wird nur gekürzt und übersetzt, nie umgeschrieben.

import type { ActionLabels, ActionPreview } from '../../components/lcm-server-action';
import type { Impact } from '../../components/lcm-dialog';
import type { ActionPreviewData, PreviewNote } from '../../api/servers';
import { formatDuration } from '../../lib/format';
import { i18n, t } from '../../lib/i18n';

/** Längere Skripte (der APT-Wrapper hat gut 20 Zeilen) werden in der Blase gekürzt. */
export const BUBBLE_COMMAND_LINES = 6;
export const BUBBLE_PACKAGES = 6;

export function actionLabels(): ActionLabels {
  return {
    meaning: t('servers.preview.labels.meaning'),
    executes: t('servers.preview.labels.executes'),
    commands: t('servers.preview.labels.commands'),
    packages: t('servers.preview.labels.packages'),
    reboot: t('servers.preview.labels.reboot'),
    reversible: t('servers.preview.labels.reversible'),
    duration: t('servers.preview.labels.duration'),
    yes: t('servers.preview.labels.yes'),
    no: t('servers.preview.labels.no'),
    docs: t('servers.preview.labels.docs'),
    loading: t('servers.preview.labels.loading'),
  };
}

/** Die ersten Zeilen eines Befehls; der Rest wird mit „… n weitere Zeilen“ benannt. */
export function clipCommand(run: string, maxLines = BUBBLE_COMMAND_LINES): string {
  const lines = run.replace(/\s+$/, '').split('\n');
  if (lines.length <= maxLines) return lines.join('\n');
  return [...lines.slice(0, maxLines), t('servers.preview.moreLines', { count: lines.length - maxLines })].join('\n');
}

/** „name 1.0 → 2.0“ bzw. nur der Name, wo es keine Zielversion gibt (Entfernen). */
export function packageLine(pkg: { name: string; from: string; to: string }): string {
  if (pkg.from && pkg.to) return `${pkg.name} ${pkg.from} → ${pkg.to}`;
  return pkg.from ? `${pkg.name} ${pkg.from}` : pkg.name;
}

/** Dauer als Spanne („30 Sek. bis 2 Min.“); fällt die Spanne zusammen, nur ein Wert. */
export function durationText(duration: ActionPreviewData['duration']): string {
  if (!duration || (duration.min <= 0 && duration.max <= 0)) return '';
  if (duration.min === duration.max || duration.min <= 0) return t('servers.preview.durationAbout', { value: formatDuration(duration.max) });
  return t('servers.preview.durationRange', { min: formatDuration(duration.min), max: formatDuration(duration.max) });
}

/** Der Code der Notiz kommt als `preview.<name>`; der Katalog führt sie unter `servers.preview.notes.<name>`. */
export function noteKey(code: string): string {
  return `servers.preview.notes.${code.replace(/^preview\./, '')}`;
}

/** Die Hinweise der Konfigurations-Aktionen stehen im eigenen Block `previews.notes`. */
export function noteText(note: PreviewNote): string {
  const name = note.code.replace(/^preview\./, '');
  return i18n.tryT(noteKey(note.code), note.params) ?? i18n.tryT(`previews.notes.${name}`, note.params) ?? note.code;
}

/** Bedingung eines Schritts (`when` der Konfigurations-Vorschau); ohne Bedingung leer. */
export function whenText(command: { when?: string }): string {
  return command.when ? (i18n.tryT(`previews.when.${command.when}`) ?? command.when) : '';
}

/** Kopfzeile eines Befehls im Dialog: Benutzer und, falls vorhanden, die Bedingung. */
function commandHeader(command: { as: string; when?: string }): string {
  return [command.as, whenText(command)].filter(Boolean).join(' · ');
}

function limited(lines: string[], max: number): string[] {
  if (lines.length <= max) return lines;
  return [...lines.slice(0, max), t('servers.preview.moreItems', { count: lines.length - max })];
}

/** Für die Erklärblase. „Neustart vielleicht“ ist kein Ja und kein Nein und steht deshalb in den Hinweisen. */
export function toBubblePreview(data: ActionPreviewData): ActionPreview {
  const packages = limited(data.packages.map(packageLine), BUBBLE_PACKAGES);
  const duration = durationText(data.duration);
  // Rollback-Schritte laufen nur im Fehlerfall; sie stehen im Dialog, nicht in der Blase.
  const commands = (data.commands as Array<{ run: string; when?: string }>).filter((command) => command.when !== 'on_failure');
  return {
    commands: commands.map((command) => {
      const when = whenText(command);
      return clipCommand(when ? `# ${when}\n${command.run}` : command.run);
    }),
    packages: packages.length > 0 ? packages : undefined,
    reboot: data.reboot === 'maybe' ? undefined : data.reboot === 'yes',
    reversible: data.reversible,
    duration: duration || undefined,
  };
}

/** Sätze unter der Vorschau: Neustart vielleicht und alle Notizen. */
export function previewNotes(data: ActionPreviewData): string[] {
  return [...(data.reboot === 'maybe' ? [t('servers.preview.rebootMaybe')] : []), ...data.notes.map(noteText)];
}

function rebootImpact(reboot: ActionPreviewData['reboot']): Impact {
  const value = t(`servers.confirm.reboot.${reboot}`);
  return { label: t('servers.confirm.rebootLabel'), value, tone: reboot === 'no' ? 'ok' : 'warn' };
}

/** Für den Bestätigungsdialog: welcher Server, was läuft, Neustart, umkehrbar, Dauer. */
export function toImpacts(serverName: string, data: ActionPreviewData | undefined): Impact[] {
  const impacts: Impact[] = [{ label: t('servers.confirm.server'), value: serverName }];
  if (!data) return impacts;
  if (data.packages.length > 0) impacts.push({ label: t('servers.confirm.packages'), value: t('servers.confirm.packagesValue', { count: data.packages.length }) });
  impacts.push(rebootImpact(data.reboot));
  impacts.push({ label: t('servers.confirm.reversible'), value: t(data.reversible ? 'servers.confirm.yes' : 'servers.confirm.no'), tone: data.reversible ? 'ok' : 'warn' });
  const duration = durationText(data.duration);
  if (duration) impacts.push({ label: t('servers.confirm.duration'), value: duration });
  return impacts;
}

/**
 * Wirkung der Konfigurations-Aktionen: umkehrbar und Dauer. Server und Neustart fehlen bewusst - den Server nennt der
 * Dialog selbst, und diese Aktionen starten den Server nie neu.
 */
export function effectImpacts(data: ActionPreviewData | undefined): Impact[] {
  if (!data) return [];
  const impacts: Impact[] = [{ label: t('servers.confirm.reversible'), value: t(data.reversible ? 'servers.confirm.yes' : 'servers.confirm.no'), tone: data.reversible ? 'ok' : 'warn' }];
  const duration = durationText(data.duration);
  if (duration) impacts.push({ label: t('servers.confirm.duration'), value: duration });
  return impacts;
}

/** Alle Befehle ungekürzt, mit dem Benutzer, unter dem sie laufen (für den Dialog). */
export function fullCommands(data: ActionPreviewData | undefined): string[] {
  return (data?.commands ?? []).map((command) => {
    const header = commandHeader(command);
    return header ? `# ${header}\n${command.run}` : command.run;
  });
}
