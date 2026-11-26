/** Shapes from backend/internal/coordination (docs/coordination.md, "Coordinator issue inbox and resolution"). */

export type IssueStatus = 'OPEN' | 'RESOLVED';

export const ISSUE_STATUSES: IssueStatus[] = ['OPEN', 'RESOLVED'];

/**
 * A general problem report stored for the coordinator's faculty. It names the reporter and the
 * session only by ID: the API joins no names, grades or private feedback.
 */
export interface Issue {
  id: string;
  reporter_user_id: string;
  faculty_id: string;
  session_id?: string;
  description: string;
  status: IssueStatus;
  created_at: string;
  resolved_at?: string;
  handled_by_coordinator_id?: string;
  resolution_note?: string;
}
