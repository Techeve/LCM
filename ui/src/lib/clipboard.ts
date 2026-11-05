import { toast } from '../components/lcm-toaster';

/** Kopiert Text in die Zwischenablage; gibt `false` zurück, wenn der Browser das nicht erlaubt (etwa ohne HTTPS). */
export async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    return false;
  }
}

/** Kopiert und meldet das Ergebnis als Hinweis; `copied` und `failed` sind schon übersetzte Titel. */
export async function copyWithToast(value: string, titles: { copied: string; failed: string }): Promise<void> {
  const copied = await copyText(value);
  toast({ tone: copied ? 'ok' : 'crit', title: copied ? titles.copied : titles.failed, immediate: true });
}
