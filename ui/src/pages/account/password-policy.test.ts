import { describe, expect, it } from 'vitest';
import { checkPassword, passwordFeedback, PROBLEM_KEYS } from './password-policy';

describe('checkPassword', () => {
  it('stops at the length before checking the content', () => {
    expect(checkPassword('Ab1!')).toEqual({ ok: false, score: 0, problems: ['too_short'] });
    expect(checkPassword('a'.repeat(201)).problems).toEqual(['too_long']);
  });

  it('accepts a varied password and a long passphrase with two classes', () => {
    expect(checkPassword('Nordlicht-Wal!73x')).toMatchObject({ ok: true, problems: [] });
    expect(checkPassword('richtiges pferd batterie klammer').ok).toBe(true);
  });

  it('needs three classes below twenty characters', () => {
    expect(checkPassword('nordlichtwalfisch').problems).toContain('needs_classes');
  });

  it('rejects common passwords, also with leetspeak and trailing digits', () => {
    expect(checkPassword('P4ssw0rt!2026').problems).toContain('common_password');
  });

  it('finds sequences and repeats', () => {
    expect(checkPassword('Xy!abcd-Wolke9').problems).toContain('sequence');
    expect(checkPassword('Xy!aaab-Wolke9').problems).toContain('repeat');
    expect(checkPassword('Ab1!Ab1!Ab1!').problems).toContain('repeat');
  });

  it('rejects the own name or mail address', () => {
    const identity = { username: 'anna', email: 'anna.berg@example.com', lastName: 'Berg' };
    expect(checkPassword('Sonne-Anna-7!x', identity).problems).toContain('contains_identity');
    expect(checkPassword('Sonne-B3rg-7!x', identity).problems).toContain('contains_identity');
  });

  it('rejects passwords dominated by an obvious word', () => {
    expect(checkPassword('lcm-admin-2026').problems).toContain('context_word');
    expect(checkPassword('Nordlicht-lcm-Wal!').problems).not.toContain('context_word');
  });

  it('rejects spaces at the edges and control characters', () => {
    expect(checkPassword(' Nordlicht-Wal!73').problems).toContain('whitespace_edges');
    expect(checkPassword('Nordlicht\u0007Wal!73').problems).toContain('control_chars');
  });

  it('has a translation key for every problem', () => {
    for (const [code, key] of Object.entries(PROBLEM_KEYS)) expect(key).toBe(`password.problem.${code}`);
  });
});

describe('score', () => {
  it('stays at weak or below with a rule violation, however it looks', () => {
    expect(checkPassword('Passwort123!').score).toBeLessThanOrEqual(1);
    expect(checkPassword('Sonne-Anna-7!x', { username: 'anna' }).score).toBe(1);
    expect(checkPassword('Ab1!Ab1!Ab1!').score).toBe(1);
  });

  it('is zero from three violations on', () => {
    expect(checkPassword('aaaaaaaaaaaa').problems.length).toBeGreaterThanOrEqual(3);
    expect(checkPassword('aaaaaaaaaaaa').score).toBe(0);
  });

  it('grows with length and variety like the server score', () => {
    expect(checkPassword('Nordlicht-Wal!73x').score).toBe(4);
    expect(checkPassword('Nordlicht-W73').score).toBe(3);
    expect(checkPassword('Nordlichtwal73').score).toBe(2);
    expect(checkPassword('nordlicht walfisch samt Mond').score).toBe(4);
  });
});

describe('passwordFeedback', () => {
  it('names the reasons for a weak password and keeps an empty field quiet', () => {
    expect(passwordFeedback('', {})).toEqual({ score: 0, problems: [] });
    const { score, problems } = passwordFeedback('Sonne-Anna-7!x', { username: 'anna' });
    expect(score).toBe(1);
    expect(problems).toHaveLength(1);
  });
});
