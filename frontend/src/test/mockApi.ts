import { vi } from 'vitest';

export interface MockCall {
  method: string;
  url: string;
  body: unknown;
}

type Responder = (call: MockCall) => Response | Promise<Response>;

export function json(status: number, body?: unknown): Response {
  if (body === undefined) return new Response(null, { status });
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

/**
 * Replaces fetch with a small router: keys are "METHOD /api/v1/path" (query string ignored).
 * Unmatched requests answer 404 so a missing mock is easy to spot.
 */
export function mockApi(routes: Record<string, Responder>) {
  const calls: MockCall[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = (init?.method ?? 'GET').toUpperCase();
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
    const call = { method, url, body };
    calls.push(call);
    const responder = routes[`${method} ${url.split('?')[0]}`];
    return responder ? responder(call) : json(404, { error: 'not found' });
  });
  vi.stubGlobal('fetch', fetchMock);
  return { calls, fetchMock };
}

export const verifiedStudent = {
  id: '11111111-1111-1111-1111-111111111111',
  name: 'Ana Student',
  email: 'ana@uni.lu',
  roles: ['student'],
  verified_student: true,
  verification_status: 'APPROVED',
};
