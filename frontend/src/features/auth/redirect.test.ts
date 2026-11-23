import { describe, expect, it } from 'vitest';
import { safeRedirect } from './redirect';

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
