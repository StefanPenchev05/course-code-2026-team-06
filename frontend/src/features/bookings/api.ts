import { api, type Page } from '../../shared/api/client';
import type { BookingRequest, Session, SessionStatus } from './types';

export const sessionsApi = {
  /** Creates a REQUESTED session; both people's time is reserved until it is declined, expires or is cancelled. */
  request: (booking: BookingRequest) => api.post<{ data: Session }>('/sessions', booking).then((result) => result.data),
  /** One session the caller takes part in (404 otherwise). */
  get: (id: string, signal?: AbortSignal) =>
    api.get<{ data: Session }>(`/sessions/${encodeURIComponent(id)}`, { signal }).then((result) => result.data),
  /** Sessions where the caller is the student or the tutor, sorted by start. */
  mine: (status: SessionStatus | undefined, offset = 0, signal?: AbortSignal) =>
    api.get<Page<Session>>('/sessions/me', { query: { status, limit: 50, offset }, signal }),
  /** The tutor's own sessions by status (default REQUESTED: requests waiting for an answer). */
  tutorQueue: (status: 'REQUESTED' | 'ACCEPTED', offset = 0, signal?: AbortSignal) =>
    api.get<Page<Session>>('/tutors/me/session-requests', { query: { status, limit: 50, offset }, signal }),
  /** Online sessions need a meeting_link (http/https), on-site sessions a meeting_room. */
  accept: (id: string, meeting: { meeting_link: string } | { meeting_room: string }) =>
    api
      .post<{ data: Session }>(`/tutors/me/session-requests/${encodeURIComponent(id)}/accept`, meeting)
      .then((result) => result.data),
  decline: (id: string, reason: string) =>
    api
      .post<{ data: Session }>(`/tutors/me/session-requests/${encodeURIComponent(id)}/decline`, { reason })
      .then((result) => result.data),
  /** Only at or after the scheduled end of an ACCEPTED session; the server uses its own clock. */
  complete: (id: string) =>
    api.post<{ data: Session }>(`/tutors/me/sessions/${encodeURIComponent(id)}/complete`).then((result) => result.data),
  /** Either participant; a reason is required. Less than 24 hours before the start is a late cancellation. */
  cancel: (id: string, reason: string) =>
    api.post<{ data: Session }>(`/sessions/${encodeURIComponent(id)}/cancel`, { reason }).then((result) => result.data),
};
