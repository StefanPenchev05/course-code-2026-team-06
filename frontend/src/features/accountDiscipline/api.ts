import { api, type Page } from '../../shared/api/client';
import type { AuditCategory, AuditEvent, FlaggedUser, SuspensionDecision } from './types';

const PAGE_SIZE = 25;

function userPath(id: string): string {
  return `/coordinator/users/${encodeURIComponent(id)}`;
}

/** Flag review, suspension and account history for coordinators (the server checks every scope). */
export const disciplineApi = {
  /** Flags stored for this faculty, newest first. Only limit and offset are accepted. */
  flagged: (offset: number, signal?: AbortSignal) =>
    api.get<Page<FlaggedUser>>('/coordinator/flagged-users', { query: { limit: PAGE_SIZE, offset }, signal }),

  /** One category per request, newest first. Unknown accounts and foreign history give an empty page. */
  history: (userId: string, category: AuditCategory, offset: number, signal?: AbortSignal) =>
    api.get<Page<AuditEvent>>(`${userPath(userId)}/audit-history`, { query: { category, limit: PAGE_SIZE, offset }, signal }),

  /**
   * Only the coordinator of the account's current home faculty (404 otherwise). Blocks all access and
   * cancels every future requested or confirmed session of the account. 409 if already suspended.
   */
  suspend: (userId: string, reason: string) =>
    api.post<{ data: SuspensionDecision }>(`${userPath(userId)}/suspend`, { reason }).then((response) => response.data),

  /** Restores access only; cancelled sessions and flags stay. 409 if not suspended. */
  reinstate: (userId: string, reason: string) =>
    api.post<{ data: SuspensionDecision }>(`${userPath(userId)}/reinstate`, { reason }).then((response) => response.data),
};
