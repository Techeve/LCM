// Findet ein Element über Shadow-Grenzen hinweg. Die Einführung zeigt auf Stellen, die tief in den Komponenten der
// Seite liegen; ein Selektor allein kommt dort nicht hin.

/** Das erste Element, das `selector` trifft, in `root` oder in einem darin liegenden Shadow-Baum. */
function findDeep(selector: string, root: ParentNode): Element | null {
  const direct = root.querySelector(selector);
  if (direct) return direct;
  for (const element of root.querySelectorAll('*')) {
    const inner = element.shadowRoot ? findDeep(selector, element.shadowRoot) : null;
    if (inner) return inner;
  }
  return null;
}

/**
 * Löst einen Pfad auf: Selektoren, durch `>>>` getrennt. Jeder Teil wird innerhalb des Treffers des vorigen gesucht,
 * auch in dessen Shadow-Baum (`lcm-tabs >>> [role="tab"]`). `null`, wenn ein Teil fehlt.
 */
export function deepQuery(path: string, root: ParentNode = document): Element | null {
  let scope: ParentNode | null = root;
  let hit: Element | null = null;
  for (const part of path.split('>>>').map((piece) => piece.trim())) {
    if (!scope) return null;
    hit = findDeep(part, scope);
    if (!hit) return null;
    scope = hit.shadowRoot ?? hit;
  }
  return hit;
}
