import { beforeEach, describe, expect, it, vi } from 'vitest';
import { getDensity, getTheme, initTheme, onThemeChange, setDensity, setTheme } from './theme';

function stubSystemTheme(dark: boolean) {
  let notify: () => void = () => {};
  const query = {
    matches: dark,
    addEventListener: (_: string, listener: () => void) => (notify = listener),
  };
  vi.stubGlobal('matchMedia', () => query);
  return {
    switchTo(value: boolean) {
      query.matches = value;
      notify();
    },
  };
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-density');
});

describe('theme', () => {
  it('starts automatic and comfortable', () => {
    stubSystemTheme(false);
    initTheme();
    expect(document.documentElement.dataset.theme).toBe('auto');
    expect(document.documentElement.dataset.density).toBe('comfortable');
  });

  it('applies and remembers the choice', () => {
    stubSystemTheme(false);
    initTheme();
    setTheme('dark');
    setDensity('compact');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.density).toBe('compact');
    expect(localStorage.getItem('lcm.theme')).toBe('dark');
    expect(getTheme()).toBe('dark');
    expect(getDensity()).toBe('compact');
  });

  it('ignores stored garbage', () => {
    localStorage.setItem('lcm.theme', 'purple');
    stubSystemTheme(false);
    initTheme();
    expect(document.documentElement.dataset.theme).toBe('auto');
  });

  it('follows the system in automatic mode and announces changes', () => {
    const system = stubSystemTheme(false);
    initTheme();
    const changed = vi.fn();
    onThemeChange(changed);
    system.switchTo(true);
    expect(changed).toHaveBeenCalledOnce();
  });

  it('ignores the system once the theme is fixed', () => {
    const system = stubSystemTheme(true);
    initTheme();
    setTheme('light');
    const changed = vi.fn();
    onThemeChange(changed);
    system.switchTo(false);
    expect(changed).not.toHaveBeenCalled();
  });

  it('works without usable storage', () => {
    stubSystemTheme(false);
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    });
    initTheme();
    expect(() => setTheme('dark')).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe('dark');
    vi.unstubAllGlobals();
  });
});
