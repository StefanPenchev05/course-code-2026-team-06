import { api, type Page } from '../../shared/api/client';
import type {
  ApplicationCreated,
  ApplicationReviewDetail,
  ApplicationStatus,
  ApplicationSubmission,
  TutorApplication,
} from './types';

const PAGE_SIZE = 25;
const COORDINATOR_PATH = '/coordinator/tutor-applications';

function reviewPath(id: string): string {
  return `${COORDINATOR_PATH}/${encodeURIComponent(id)}`;
}

/** Tutor-application endpoints (docs/tutoring.md). The server checks every permission and the faculty scope. */
export const applicationsApi = {
  /** Verified students only. 409 if another application is pending or the student already tutors the subject. */
  submit: (submission: ApplicationSubmission) => api.post<ApplicationCreated>('/tutor-applications', submission),

  /** The logged-in student's applications, newest first. */
  mine: (offset: number, signal?: AbortSignal) =>
    api.get<Page<TutorApplication>>('/tutor-applications/me', { query: { limit: PAGE_SIZE, offset }, signal }),

  /** Coordinator queue for their own faculty, newest first. */
  queue: (status: ApplicationStatus, offset: number, signal?: AbortSignal) =>
    api.get<Page<TutorApplication>>(COORDINATOR_PATH, { query: { status, limit: PAGE_SIZE, offset }, signal }),

  /** Coordinator detail including the private grade and the description. 404 outside the coordinator's faculty. */
  reviewDetail: (id: string, signal?: AbortSignal) =>
    api.get<{ data: ApplicationReviewDetail }>(reviewPath(id), { signal }).then((response) => response.data),

  /** 204 on success. 409 if already decided or the applicant left this faculty. */
  approve: (id: string) => api.post(`${reviewPath(id)}/approve`),

  /** 204 on success. The reason (1–2000 bytes) is shown to the student. */
  reject: (id: string, reason: string) => api.post(`${reviewPath(id)}/reject`, { reason }),
};
