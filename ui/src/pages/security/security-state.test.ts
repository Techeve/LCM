import { describe, expect, it } from 'vitest';
import {
  advisoryPatch,
  advisoryQueryFromUrl,
  cvePatch,
  cveQueryFromUrl,
  hasAdvisoryFilter,
  hasCveFilter,
  pageRange,
  parseTab,
  tabPath,
} from './security-state';

const params = (query: string) => new URLSearchParams(query);

describe('tabs', () => {
  it('falls back to the CVE list for unknown tabs', () => {
    expect(parseTab(undefined)).toBe('cve');
    expect(parseTab('quatsch')).toBe('cve');
    expect(parseTab('advisories')).toBe('advisories');
  });

  it('keeps the start page address short', () => {
    expect(tabPath('cve')).toBe('/security');
    expect(tabPath('caches')).toBe('/security/caches');
  });
});

describe('CVE filters', () => {
  it('has defaults for an empty address', () => {
    expect(cveQueryFromUrl(params(''))).toEqual({ source: '', page: 1 });
  });

  it('reads source and page', () => {
    expect(cveQueryFromUrl(params('source=docker&page=3'))).toEqual({ source: 'docker', page: 3 });
  });

  it('drops nonsense', () => {
    expect(cveQueryFromUrl(params('source=alles&page=-4'))).toEqual({ source: '', page: 1 });
    expect(cveQueryFromUrl(params('page=abc'))).toEqual({ source: '', page: 1 });
  });

  it('jumps back to page one when the filter changes', () => {
    expect(cvePatch({ source: 'os' })).toEqual({ source: 'os', page: null });
    expect(cvePatch({ source: '' })).toEqual({ source: null, page: null });
  });

  it('keeps the page when only the page changes', () => {
    expect(cvePatch({ page: 4 })).toEqual({ page: 4 });
    expect(cvePatch({ page: 1 })).toEqual({ page: null });
  });

  it('knows whether a filter is set', () => {
    expect(hasCveFilter({ source: '', page: 2 })).toBe(false);
    expect(hasCveFilter({ source: 'os', page: 1 })).toBe(true);
  });
});

describe('advisory filters', () => {
  it('reads every filter', () => {
    expect(advisoryQueryFromUrl(params('min=high&resolved=1&page=2'))).toEqual({ minSeverity: 'high', resolved: true, page: 2 });
  });

  it('drops nonsense', () => {
    expect(advisoryQueryFromUrl(params('min=schlimm&resolved=ja'))).toEqual({ minSeverity: '', resolved: false, page: 1 });
  });

  it('writes the defaults as missing parameters', () => {
    expect(advisoryPatch({ minSeverity: '', resolved: false })).toEqual({ min: null, resolved: null, page: null });
    expect(advisoryPatch({ resolved: true })).toEqual({ resolved: 1, page: null });
  });

  it('knows whether a filter is set', () => {
    expect(hasAdvisoryFilter({ minSeverity: '', resolved: false, page: 3 })).toBe(false);
    expect(hasAdvisoryFilter({ minSeverity: '', resolved: true, page: 1 })).toBe(true);
  });
});

describe('pageRange', () => {
  it('describes a middle page', () => {
    expect(pageRange(2, 50, 120)).toEqual({ from: 51, to: 100, more: true });
  });

  it('describes the last page', () => {
    expect(pageRange(3, 50, 120)).toEqual({ from: 101, to: 120, more: false });
  });

  it('has nothing for an empty list', () => {
    expect(pageRange(1, 50, 0)).toBeNull();
  });
});
