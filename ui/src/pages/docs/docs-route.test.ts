import { describe, expect, it } from 'vitest';
import type { PageRoute, RouteMatch } from '../../lib/route-match';
import { DOCS_TAG, docsSlug, slugFromPath } from './docs-route';

function match(tag: string, rest?: string): RouteMatch<PageRoute> {
  const params: Record<string, string> = rest === undefined ? {} : { '*': rest };
  return { route: { path: '/x', tag, load: () => Promise.resolve() }, params };
}

describe('slugFromPath', () => {
  it('nimmt das erste Segment', () => {
    expect(slugFromPath('ssh-schluessel/mehr')).toBe('ssh-schluessel');
    expect(slugFromPath(undefined)).toBe('');
  });
});

describe('docsSlug', () => {
  it('liefert die Seite der Doku-Route', () => {
    expect(docsSlug(match(DOCS_TAG, 'gruppen'))).toBe('gruppen');
    expect(docsSlug(match(DOCS_TAG, ''))).toBe('');
  });

  it('ignoriert fremde Routen, damit die Doku nicht zurückleitet', () => {
    expect(docsSlug(match('lcm-page-servers'))).toBeNull();
    expect(docsSlug(match('lcm-page-groups', 'abc'))).toBeNull();
    expect(docsSlug(null)).toBeNull();
  });
});
