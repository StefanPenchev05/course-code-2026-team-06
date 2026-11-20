/**
 * Typed client for the Tutti Go API (/api/v1).
 *
 * - Cookies: the session cookie is HttpOnly and same-origin, so every request
 *   uses credentials: 'same-origin'. JavaScript never reads or stores the token.
 * - Same-origin writes: browsers add the Origin header to POST/PATCH/DELETE
 *   automatically; the API compares it with PUBLIC_ORIGIN. Nothing to set here.
 * - Errors: the API answers {"error": "..."}; we turn that into an ApiError
 *   carrying the HTTP status. Status 0 means the server could not be reached.
 */

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

/** Pagination wrapper used by list endpoints such as /subjects. */
export interface Pagination {
  limit: number;
  offset: number;
  has_more: boolean;
}

export interface Page<T> {
  data: T[];
  pagination: Pagination;
}

export type Query = Record<string, string | number | boolean | null | undefined>;

export interface RequestOptions {
  query?: Query;
  body?: unknown;
  signal?: AbortSignal;
}

export const API_BASE = '/api/v1';

let unauthorizedHandler: (() => void) | null = null;

/** Called whenever the API answers 401, e.g. when the session expired. */
export function onUnauthorized(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

export function buildUrl(path: string, query?: Query): string {
  const params = new URLSearchParams();
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== null && value !== '') {
        params.set(key, String(value));
      }
    }
  }
  const search = params.toString();
  return `${API_BASE}${path}${search ? `?${search}` : ''}`;
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

function serverMessage(payload: unknown): string {
  if (typeof payload === 'object' && payload !== null && 'error' in payload) {
    const { error } = payload as { error: unknown };
    if (typeof error === 'string') return error;
  }
  return '';
}

export async function request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  let body: string | undefined;
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(options.body);
  }

  let response: Response;
  try {
    response = await fetch(buildUrl(path, options.query), {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: options.signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error;
    throw new ApiError(0, '');
  }

  if (response.status === 204) return undefined as T;

  const payload = await readBody(response);
  if (!response.ok) {
    if (response.status === 401) unauthorizedHandler?.();
    throw new ApiError(response.status, serverMessage(payload));
  }
  return payload as T;
}

export const api = {
  get: <T>(path: string, options?: Omit<RequestOptions, 'body'>) => request<T>('GET', path, options),
  post: <T = void>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('POST', path, { ...options, body }),
  patch: <T>(path: string, body?: unknown, options?: RequestOptions) => request<T>('PATCH', path, { ...options, body }),
  delete: <T = void>(path: string, options?: RequestOptions) => request<T>('DELETE', path, options),
};
