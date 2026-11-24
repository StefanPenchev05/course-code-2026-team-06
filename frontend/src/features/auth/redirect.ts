/**
 * Where to go after logging in. Only internal paths are allowed, so a crafted
 * link can't send someone to another website (no open redirects).
 */
export function safeRedirect(state: unknown): string {
  const from = (state as { from?: { pathname?: unknown; search?: unknown } } | null)?.from;
  const pathname = from?.pathname;
  if (typeof pathname !== 'string' || !pathname.startsWith('/') || pathname.startsWith('//')) return '/';
  if (pathname === '/login' || pathname === '/register') return '/';
  const search = typeof from?.search === 'string' ? from.search : '';
  return pathname + search;
}
