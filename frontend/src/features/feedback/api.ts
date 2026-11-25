import { api, ApiError, type Page } from '../../shared/api/client';
import type { FeedbackEntry, FeedbackInput, TutorActivity } from './types';

const FEEDBACK_PAGE = 25;
/** The activity endpoint pages the upcoming sessions; the totals are the same on every page. */
const UPCOMING_LIMIT = 50;

function sessionPath(sessionId: string): string {
  return `/sessions/${encodeURIComponent(sessionId)}/feedback`;
}

/** Feedback endpoints (docs/feedback.md). The server checks every permission. */
export const feedbackApi = {
  /** The session's student, once, and only while the session is COMPLETED. 409 if already rated. */
  submit: (sessionId: string, input: FeedbackInput) =>
    api.post<{ data: FeedbackEntry }>(sessionPath(sessionId), input).then((response) => response.data),

  /** The feedback on one session for its student or tutor, or null when there is none yet (404). */
  forSession: (sessionId: string, signal?: AbortSignal) =>
    api.get<{ data: FeedbackEntry }>(sessionPath(sessionId), { signal }).then(
      (response): FeedbackEntry | null => response.data,
      (error: unknown) => {
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      },
    ),

  /** Active tutors: feedback received on completed sessions, newest first. */
  received: (offset: number, signal?: AbortSignal) =>
    api.get<Page<FeedbackEntry>>('/tutors/me/feedback', { query: { limit: FEEDBACK_PAGE, offset }, signal }),

  /** Active tutors: totals and the next accepted sessions. */
  activity: (signal?: AbortSignal) =>
    api.get<{ data: TutorActivity }>('/tutors/me/activity', { query: { limit: UPCOMING_LIMIT, offset: 0 }, signal }).then(
      (response) => response.data,
    ),
};
