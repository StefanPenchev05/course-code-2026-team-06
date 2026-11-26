import { api, type Page } from '../../shared/api/client';
import type { NoShowReport, NoShowStatus } from './types';

const PAGE_SIZE = 25;

function path(id: string): string {
  return `/coordinator/attendance-reports/${encodeURIComponent(id)}`;
}

/** No-show reports of sessions taught in the coordinator's faculty (docs/coordination.md). Others answer 404. */
export const noShowApi = {
  /** Oldest first. Only status, limit and offset are accepted as query keys. */
  list: (status: NoShowStatus, offset: number, signal?: AbortSignal) =>
    api.get<Page<NoShowReport>>('/coordinator/attendance-reports', { query: { status, limit: PAGE_SIZE, offset }, signal }),

  detail: (id: string, signal?: AbortSignal) => api.get<{ data: NoShowReport }>(path(id), { signal }).then((response) => response.data),

  /**
   * Records a violation for the absent participant and, on the first confirmation, corrects the
   * session to NO_SHOW. 409 if already decided or the session outcome does not allow it.
   */
  confirm: (id: string, note: string) =>
    api.post<{ data: NoShowReport }>(`${path(id)}/confirm`, { resolution_note: note }).then((response) => response.data),

  /** Keeps the session outcome as it is. */
  reject: (id: string, note: string) =>
    api.post<{ data: NoShowReport }>(`${path(id)}/reject`, { resolution_note: note }).then((response) => response.data),
};
