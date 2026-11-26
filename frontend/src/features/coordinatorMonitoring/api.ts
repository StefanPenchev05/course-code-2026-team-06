import { api, type Page, type Pagination } from '../../shared/api/client';
import type { FacultyStatistics, FeedbackPage, PolicyCancellation, ScheduleEntry, TutorDetail, TutorSummary } from './types';

const PAGE_SIZE = 25;
/** The API's maximum page size, for lists that are short in practice (a tutor's subjects, one week). */
const MAX_PAGE = 100;

function tutorPath(id: string): string {
  return `/coordinator/tutors/${encodeURIComponent(id)}`;
}

export interface TutorFilter {
  programId?: string;
  subjectId?: string;
}

/** Coordinator monitoring and policy cancellation (docs/coordination.md). The server checks the faculty scope. */
export const monitoringApi = {
  /** Faculty-wide tutor counts and activity totals. */
  statistics: (signal?: AbortSignal) =>
    api.get<{ data: FacultyStatistics }>('/coordinator/statistics', { signal }).then((response) => response.data),

  /** Approved tutors whose current program is in the coordinator's faculty, sorted by name. */
  tutors: ({ programId, subjectId }: TutorFilter, offset: number, signal?: AbortSignal) =>
    api.get<Page<TutorSummary>>('/coordinator/tutors', {
      query: { program_id: programId, subject_id: subjectId, limit: PAGE_SIZE, offset },
      signal,
    }),

  /** One tutor with totals and their approved subjects. 404 if the tutor is not in this faculty. */
  tutor: (id: string, signal?: AbortSignal) =>
    api.get<{ data: TutorDetail; offerings_pagination: Pagination }>(tutorPath(id), {
      query: { limit: MAX_PAGE, offset: 0 },
      signal,
    }),

  /** Published availability and sessions overlapping [from, to); at most 31 days. */
  schedule: (id: string, from: Date, to: Date, signal?: AbortSignal) =>
    api.get<Page<ScheduleEntry>>(`${tutorPath(id)}/schedule`, {
      query: { from: from.toISOString(), to: to.toISOString(), limit: MAX_PAGE, offset: 0 },
      signal,
    }),

  /** Ratings and private comments on this faculty's completed sessions, newest first. */
  feedback: (id: string, offset: number, signal?: AbortSignal) =>
    api.get<FeedbackPage>(`${tutorPath(id)}/feedback`, { query: { limit: PAGE_SIZE, offset }, signal }),

  /** Policy cancellation of a REQUESTED or ACCEPTED session; the reason (1–2000 bytes) goes to both participants. */
  cancelSession: (sessionId: string, reason: string) =>
    api
      .post<{ data: PolicyCancellation }>(`/coordinator/sessions/${encodeURIComponent(sessionId)}/cancel`, { reason })
      .then((response) => response.data),
};
