import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockApi } from '../../test/mockApi';
import { api, ApiError, buildUrl, onForbidden, onUnauthorized } from './client';
import { errorMessage } from './errors';

afterEach(() => {
  vi.unstubAllGlobals();
  onUnauthorized(null);
  onForbidden(null);
});

describe('buildUrl', () => {
  it('prefixes the API base and skips empty query values', () => {
    expect(buildUrl('/subjects', { q: 'algo rithms', limit: 10, offset: undefined, level: '' })).toBe(
      '/api/v1/subjects?q=algo+rithms&limit=10',
    );
  });
});

describe('api client', () => {
  it('sends JSON with same-origin credentials', async () => {
    const { fetchMock } = mockApi({ 'POST /api/v1/auth/login': () => json(200, { id: '1' }) });

    const result = await api.post<{ id: string }>('/auth/login', { email: 'a@uni.lu', password: 'secret' });

    expect(result).toEqual({ id: '1' });
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.credentials).toBe('same-origin');
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(init.body).toBe(JSON.stringify({ email: 'a@uni.lu', password: 'secret' }));
  });

  it('returns undefined for 204 No Content', async () => {
    mockApi({ 'POST /api/v1/auth/logout': () => json(204) });
    await expect(api.post('/auth/logout')).resolves.toBeUndefined();
  });

  it('turns {"error"} responses into ApiError with the status', async () => {
    mockApi({ 'POST /api/v1/sessions': () => json(409, { error: 'conflict' }) });

    const error = await api.post('/sessions', {}).catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(409);
    expect((error as ApiError).message).toBe('conflict');
  });

  it('reports network failures as status 0', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))));
    const error = await api.get('/auth/me').catch((e: unknown) => e);
    expect((error as ApiError).status).toBe(0);
    expect(errorMessage(error)).toMatch(/could not reach the server/i);
  });

  it('notifies the auth layer on 401', async () => {
    const handler = vi.fn();
    onUnauthorized(handler);
    mockApi({ 'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }) });

    await expect(api.get('/auth/me')).rejects.toBeInstanceOf(ApiError);
    expect(handler).toHaveBeenCalledOnce();
  });
});

describe('onForbidden', () => {
  it('reports 403 responses with the request path', async () => {
    const handler = vi.fn();
    onForbidden(handler);
    mockApi({ 'GET /api/v1/tutors': () => json(403, { error: 'permission denied' }) });

    await expect(api.get('/tutors', { query: { subject_id: 'x' } })).rejects.toBeInstanceOf(ApiError);
    expect(handler).toHaveBeenCalledWith('/tutors');
  });
});

describe('errorMessage', () => {
  it('prefers screen-specific overrides, then 422 details, then defaults', () => {
    expect(errorMessage(new ApiError(401, ''), { 401: 'Wrong password.' })).toBe('Wrong password.');
    expect(errorMessage(new ApiError(422, 'grade must be between 15 and 20'))).toBe('Grade must be between 15 and 20');
    expect(errorMessage(new ApiError(403, 'forbidden'))).toBe("You don't have permission to do that.");
    expect(errorMessage(new Error('boom'))).toBe('Something went wrong. Please try again.');
  });
});
