import { api, type Page } from '../../shared/api/client';
import type { Verification } from './types';

const PAGE_SIZE = 25;

function path(id: string): string {
  return `/coordinator/verifications/${encodeURIComponent(id)}`;
}

/** Coordinator review of student registrations (docs/identity.md). The server checks the faculty. */
export const verificationsApi = {
  /**
   * Pending verifications of the coordinator's faculty, oldest first. The API answers with a
   * bare array and no pagination, so ask for one extra row to know whether there are more.
   */
  pending: (offset: number, signal?: AbortSignal): Promise<Page<Verification>> =>
    api
      .get<Verification[]>('/coordinator/verifications', { query: { limit: PAGE_SIZE + 1, offset }, signal })
      .then((rows) => ({
        data: rows.slice(0, PAGE_SIZE),
        pagination: { limit: PAGE_SIZE, offset, has_more: rows.length > PAGE_SIZE },
      })),

  /** One verification of this faculty, pending or decided (the API does not wrap it in { data }). */
  detail: (id: string, signal?: AbortSignal) => api.get<Verification>(path(id), { signal }),

  /** 204. 409 when it was already decided. */
  approve: (id: string) => api.post<void>(`${path(id)}/approve`),

  /** 204. The student sees the reason (1–2000 bytes) in Tutti and can resubmit. */
  reject: (id: string, reason: string) => api.post<void>(`${path(id)}/reject`, { reason }),
};
