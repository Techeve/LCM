import { describe, expect, it } from 'vitest';
import type { ContainerImage, ContainerItem } from '../../api/containers';
import { parseTab, queryFromUrl, queryPatch, seriousCount, slicePage, tabPath, updateCount, visibleContainers, visibleImages } from './containers-state';

const params = (query: string) => new URLSearchParams(query);

const image = (patch: Partial<ContainerImage>): ContainerImage => ({ id: 'a:1', name: 'a:1', tag: '1', servers: [], serverCount: 0, inUse: 0, status: 'current', critical: 0, high: 0, ...patch });
const container = (state: string): ContainerItem => ({ id: state, serverId: 1, serverName: 's', name: state, image: '', state, status: '', ports: '', project: '', service: '' });

describe('tabs', () => {
  it('reads the tab from the rest of the path', () => {
    expect(parseTab(undefined)).toBe('images');
    expect(parseTab('')).toBe('images');
    expect(parseTab('instances')).toBe('instances');
    expect(parseTab('compose/extra')).toBe('compose');
    expect(parseTab('quatsch')).toBe('images');
  });

  it('keeps the start page address short', () => {
    expect(tabPath('images')).toBe('/containers');
    expect(tabPath('compose')).toBe('/containers/compose');
  });
});

describe('query', () => {
  it('has defaults for an empty address', () => {
    expect(queryFromUrl(params(''))).toEqual({ onlyUpdates: false, showAll: false, page: 1 });
  });

  it('reads filters and page', () => {
    expect(queryFromUrl(params('updates=1&all=1&page=3'))).toEqual({ onlyUpdates: true, showAll: true, page: 3 });
  });

  it('drops nonsense', () => {
    expect(queryFromUrl(params('updates=ja&page=-1'))).toEqual({ onlyUpdates: false, showAll: false, page: 1 });
  });

  it('writes defaults as missing parameters and resets the page', () => {
    expect(queryPatch({ onlyUpdates: true })).toEqual({ updates: 1, page: null });
    expect(queryPatch({ showAll: false })).toEqual({ all: null, page: null });
    expect(queryPatch({ page: 2 })).toEqual({ page: 2 });
    expect(queryPatch({ page: 1 })).toEqual({ page: null });
  });
});

describe('filters', () => {
  it('keeps only images with an update', () => {
    const images = [image({ id: '1', status: 'update' }), image({ id: '2' })];
    expect(visibleImages(images, true).map((i) => i.id)).toEqual(['1']);
    expect(visibleImages(images, false)).toHaveLength(2);
  });

  it('keeps only running containers unless all are wanted', () => {
    const list = [container('running'), container('exited'), container('restarting')];
    expect(visibleContainers(list, false).map((c) => c.state)).toEqual(['running']);
    expect(visibleContainers(list, true)).toHaveLength(3);
  });

  it('counts updates and serious findings', () => {
    const images = [image({ status: 'update' }), image({ critical: 1 }), image({ high: 2, status: 'update' }), image({})];
    expect(updateCount(images)).toBe(2);
    expect(seriousCount(images)).toBe(2);
  });
});

describe('slicePage', () => {
  const items = Array.from({ length: 7 }, (_, i) => i);

  it('cuts a page', () => {
    expect(slicePage(items, 2, 3)).toEqual({ items: [3, 4, 5], page: 2, total: 7, from: 4, to: 6 });
  });

  it('pulls a page behind the end back to the last one', () => {
    expect(slicePage(items, 9, 3)).toEqual({ items: [6], page: 3, total: 7, from: 7, to: 7 });
  });

  it('handles an empty list', () => {
    expect(slicePage([], 1, 3)).toEqual({ items: [], page: 1, total: 0, from: 0, to: 0 });
  });
});
