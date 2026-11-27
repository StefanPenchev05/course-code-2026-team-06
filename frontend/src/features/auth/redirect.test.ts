import { describe, expect, it } from 'vitest';
import { safePath, safeRedirect } from './redirect';

describe('safeRedirect', () => {
  it('returns the page the user came from', () => {
    expect(safeRedirect({ from: { pathname: '/sessions', search: '?status=REQUESTED' } })).toBe('/sessions?status=REQUESTED');
  });

  it('rejects anything that could leave the app', () => {
    expect(safeRedirect({ from: { pathname: '//evil.example' } })).toBe('/');
    expect(safeRedirect({ from: { pathname: 'https://evil.example' } })).toBe('/');
    expect(safeRedirect(null)).toBe('/');
    expect(safeRedirect({ from: { pathname: '/login' } })).toBe('/');
  });
});


describe('safePath', () => {
  it('keeps internal paths with their query', () => {
    expect(safePath('/coordinator/flags')).toBe('/coordinator/flags');
    expect(safePath('/sessions', '?status=REQUESTED')).toBe('/sessions?status=REQUESTED');
  });

  it('rejects backslash tricks that browsers read as another website', () => {
    expect(safePath('/\\evil.example')).toBe('/');
    expect(safePath('/\\/evil.example')).toBe('/');
  });

  it('ignores a search part that is not a query', () => {
    expect(safePath('/sessions', '//evil.example')).toBe('/sessions');
  });

  it('never returns to the login or register page', () => {
    expect(safePath('/register')).toBe('/');
    expect(safePath(undefined)).toBe('/');
  });
});