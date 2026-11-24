import { api, type Page } from '../../shared/api/client';
import type { DataList, Faculty, Program, Subject } from './types';

// The /public/... endpoints work without logging in; they exist for the
// registration form (docs/catalog.md). Logged-in screens use the others.
export const catalogApi = {
  publicFaculties: (signal?: AbortSignal) =>
    api.get<DataList<Faculty>>('/public/faculties', { signal }).then((result) => result.data),
  publicPrograms: (facultyId: string, signal?: AbortSignal) =>
    api
      .get<DataList<Program>>(`/public/faculties/${encodeURIComponent(facultyId)}/programs`, { signal })
      .then((result) => result.data),

  faculties: (signal?: AbortSignal) => api.get<DataList<Faculty>>('/faculties', { signal }).then((result) => result.data),
  /** Search all subjects by code or name (q: at most 100 characters). */
  searchSubjects: (q: string, signal?: AbortSignal) =>
    api.get<Page<Subject>>('/subjects', { query: { q, limit: 10 }, signal }),
  /** Subjects of one program, optionally filtered by code or name. Empty q lists them all. */
  programSubjects: (programId: string, q: string, offset = 0, signal?: AbortSignal) =>
    api.get<Page<Subject>>(`/programs/${encodeURIComponent(programId)}/subjects`, {
      query: { q, limit: 25, offset },
      signal,
    }),
  subject: (id: string, signal?: AbortSignal) =>
    api.get<{ data: Subject }>(`/subjects/${encodeURIComponent(id)}`, { signal }).then((result) => result.data),
};
