import { api } from '../../shared/api/client';
import type { DataList, Faculty, Program } from './types';

// The /public/... endpoints work without logging in; they exist for the
// registration form (docs/catalog.md). Logged-in screens use the
// non-public versions.
export const catalogApi = {
  publicFaculties: (signal?: AbortSignal) =>
    api.get<DataList<Faculty>>('/public/faculties', { signal }).then((result) => result.data),
  publicPrograms: (facultyId: string, signal?: AbortSignal) =>
    api
      .get<DataList<Program>>(`/public/faculties/${encodeURIComponent(facultyId)}/programs`, { signal })
      .then((result) => result.data),
};
