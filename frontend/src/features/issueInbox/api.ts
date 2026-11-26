import { api, type Page } from '../../shared/api/client';
import type { Issue, IssueStatus } from './types';

const PAGE_SIZE = 25;

function path(id: string): string {
  return `/coordinator/issues/${encodeURIComponent(id)}`;
}

/** Faculty-scoped issue inbox (docs/coordination.md). Other faculties' reports answer 404. */
export const issuesApi = {
  /** Oldest first. Only status, limit and offset are accepted as query keys. */
  inbox: (status: IssueStatus, offset: number, signal?: AbortSignal) =>
    api.get<Page<Issue>>('/coordinator/issues', { query: { status, limit: PAGE_SIZE, offset }, signal }),

  detail: (id: string, signal?: AbortSignal) => api.get<{ data: Issue }>(path(id), { signal }).then((response) => response.data),

  /** One-time; 409 when it was already resolved. The note is 1–2000 UTF-8 bytes after trimming. */
  resolve: (id: string, note: string) =>
    api.post<{ data: Issue }>(`${path(id)}/resolve`, { resolution_note: note }).then((response) => response.data),
};
