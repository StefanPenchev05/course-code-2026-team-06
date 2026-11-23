import { api, type Page } from '../../shared/api/client';
import type { BookingRequest, Session, SessionStatus } from './types';

export const sessionsApi = {
  /** Creates a REQUESTED session; both people's time is reserved until it is declined, expires or is cancelled. */
  request: (booking: BookingRequest) => api.post<{ data: Session }>('/sessions', booking).then((result) => result.data),
  /** Sessions where the caller is the student or the tutor, sorted by start. */
  mine: (status: SessionStatus | undefined, offset = 0, signal?: AbortSignal) =>
    api.get<Page<Session>>('/sessions/me', { query: { status, limit: 50, offset }, signal }),
  /** Either participant; a reason is required. Less than 24 hours before the start is a late cancellation. */
  cancel: (id: string, reason: string) =>
    api.post<{ data: Session }>(`/sessions/${encodeURIComponent(id)}/cancel`, { reason }).then((result) => result.data),
};
