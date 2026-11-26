import { api } from '../../shared/api/client';
import type { IssueReport, IssueReportInput, ReportedSession } from './types';

export const issueReportsApi = {
  /**
   * Verified students (tutors included). A report about a session goes to the tutor's faculty,
   * any other report to the reporter's faculty. 404 if the session is not the caller's.
   */
  create: (input: IssueReportInput) => api.post<{ data: IssueReport }>('/issues', input).then((response) => response.data),

  /** The caller's own session (as student or tutor), to show what the report is about. */
  session: (id: string, signal?: AbortSignal) =>
    api.get<{ data: ReportedSession }>(`/sessions/${encodeURIComponent(id)}`, { signal }).then((response) => response.data),
};
