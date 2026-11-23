import { api, type Page } from '../../shared/api/client';
import type { FreeSlot, TutorMatch, TutorProfile, TutorSearchFilters } from './types';

export const PAGE_SIZE = 20;

export const tutorsApi = {
  /** Approved, active tutors for a subject that is in the student's own program (otherwise 422). */
  search: (filters: TutorSearchFilters, signal?: AbortSignal) =>
    api.get<Page<TutorMatch>>('/tutors', { query: { ...filters, limit: PAGE_SIZE }, signal }),
  profile: (id: string, signal?: AbortSignal) =>
    api.get<{ data: TutorProfile }>(`/tutors/${encodeURIComponent(id)}`, { signal }).then((result) => result.data),
  /** Free intervals in [from, to), sorted by start. */
  availability: (id: string, from: Date, to: Date, offset = 0, signal?: AbortSignal) =>
    api.get<Page<FreeSlot>>(`/tutors/${encodeURIComponent(id)}/availability`, {
      query: { from: from.toISOString(), to: to.toISOString(), limit: 100, offset },
      signal,
    }),
};
