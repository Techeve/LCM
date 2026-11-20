// lcm-select und lcm-switch übernehmen eine Wahl nicht selbst, das macht der Aufrufer.

/** Für Katalog-Beispiele ohne eigenen Zustand: übernimmt die gemeldete Wahl direkt am Element. */
export function followChange(event: Event): void {
  const { value, checked } = (event as CustomEvent<{ value?: string; checked?: boolean }>).detail;
  const target = event.currentTarget as HTMLElement & { value?: string; checked?: boolean };
  if (value !== undefined) target.value = value;
  if (checked !== undefined) target.checked = checked;
}
