import { afterEach, describe, expect, it, vi } from 'vitest';
import { checkUniversitySignIn, clearAttempt, readAttempt, rememberAttempt, takeReturnPath, UNIVERSITY_LOGIN_PATH } from './sso';

// Unit tests with a stubbed fetch (test double). The real API is exercised by tests/e2e/sso.spec.ts.

function answer(response: Partial<Response>) {
  const fetchMock = vi.fn(async () => response as Response);
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
});

describe('checkUniversitySignIn', () => {
  it('asks the fixed Tutti address without following the redirect to the university', async () => {
    const fetchMock = answer({ type: 'opaqueredirect', status: 0 });

    expect(await checkUniversitySignIn()).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/oidc/login', expect.objectContaining({ redirect: 'manual', credentials: 'same-origin' }));
    expect(UNIVERSITY_LOGIN_PATH).toBe('/api/v1/auth/oidc/login');
  });

  it.each([
    [404, 'disabled'],
    [429, 'busy'],
    [503, 'unavailable'],
    [500, 'unavailable'],
    [403, 'unavailable'],
  ] as const)('reports HTTP %i as %s', async (status, problem) => {
    answer({ type: 'basic', status });
    expect(await checkUniversitySignIn()).toBe(problem);
  });

  it('reports a network failure as offline', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))));
    expect(await checkUniversitySignIn()).toBe('offline');
  });
});

describe('the remembered sign-in attempt', () => {
  const now = Date.UTC(2026, 10, 27, 12);

  it('returns the wanted page once, then forgets it', () => {
    rememberAttempt('/sessions?status=REQUESTED', now);

    expect(takeReturnPath(now + 60_000)).toBe('/sessions?status=REQUESTED');
    expect(takeReturnPath(now + 61_000)).toBeNull();
  });

  it('expires after 10 minutes, like the API attempt itself', () => {
    rememberAttempt('/sessions', now);

    expect(readAttempt(now + 10 * 60_000)).not.toBeNull();
    expect(readAttempt(now + 10 * 60_000 + 1)).toBeNull();
  });

  it('never returns to another website, even if storage was changed', () => {
    rememberAttempt('//evil.example', now);
    expect(readAttempt(now)?.returnTo).toBe('/');

    sessionStorage.setItem('tutti.universitySignIn', JSON.stringify({ returnTo: 'https://evil.example', startedAt: now }));
    expect(readAttempt(now)?.returnTo).toBe('/');

    sessionStorage.setItem('tutti.universitySignIn', '{not json');
    expect(readAttempt(now)).toBeNull();

    sessionStorage.setItem('tutti.universitySignIn', JSON.stringify({ returnTo: '/sessions', startedAt: now + 60_000 }));
    expect(readAttempt(now)).toBeNull(); // started "in the future": not trusted
  });

  it('keeps working when the browser blocks storage', () => {
    const blocked = () => {
      throw new DOMException('blocked', 'SecurityError');
    };
    vi.stubGlobal('sessionStorage', { getItem: blocked, setItem: blocked, removeItem: blocked });

    expect(() => rememberAttempt('/sessions', now)).not.toThrow();
    expect(readAttempt(now)).toBeNull();
    expect(() => clearAttempt()).not.toThrow();
  });
});
