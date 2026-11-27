/**
 * Where to go after logging in. Only internal paths are allowed, so a crafted
 * link can't send someone to another website (no open redirects).
 */
export function safeRedirect(state: unknown): string {
  const from = (state as { from?: { pathname?: unknown; search?: unknown } } | null)?.from;
  return safePath(from?.pathname, from?.search);
}

/**
 * An internal path of this app, or '/' for anything else. Rejects other
 * websites ('//evil.example', 'https://…', '/\evil.example', which browsers
 * treat like '//'), and the login and register pages themselves.
 */
export function safePath(pathname: unknown, search: unknown = ''): string {
  if (typeof pathname !== 'string' || !pathname.startsWith('/') || pathname.startsWith('//')) return '/';
  if (pathname.includes('\\')) return '/';
  if (pathname === '/login' || pathname === '/register') return '/';
  const query = typeof search === 'string' && search.startsWith('?') ? search : '';
  return pathname + query;
}
