import { beforeAll, describe, expect, it } from 'vitest';
import { i18n } from '../../lib/i18n';
import { buildImpacts, serverNames } from './impacts';

const server = (id: number, name: string, off = false) => ({ id, name, host: '', user_sync_disabled: off });

beforeAll(async () => {
  await i18n.setLanguage('de');
});

describe('serverNames', () => {
  it('names the first servers and counts the rest', () => {
    const many = [1, 2, 3, 4, 5, 6].map((n) => server(n, `web0${n}`));
    expect(serverNames(many.slice(0, 2))).toBe('web01, web02');
    expect(serverNames(many)).toBe('web01, web02, web03, web04, … und 2 weitere');
  });
});

describe('buildImpacts', () => {
  it('says so when no server is touched', () => {
    const [servers] = buildImpacts({ servers: [] });
    expect(servers?.tone).toBe('ok');
  });

  it('separates servers with the user sync switched off', () => {
    const impacts = buildImpacts({ servers: [server(1, 'web01'), server(2, 'web02', true)], effect: 'x', reversible: false });
    expect(impacts.map((i) => i.tone)).toEqual(['warn', 'ok', undefined, 'crit']);
    expect(impacts[1]?.value).toBe('web02');
    expect(impacts[0]?.value).toContain('web01');
    expect(impacts[0]?.value).not.toContain('web02');
  });
});
