/** Shapes from backend/internal/coordination (docs/coordination.md, "General issue reporting"). */

/** POST /issues. Leave session_id out for a report that is not about one session. */
export interface IssueReportInput {
  description: string;
  session_id?: string;
}

export interface IssueReport {
  id: string;
  reporter_user_id: string;
  faculty_id: string;
  session_id?: string;
  description: string;
  status: 'OPEN' | 'RESOLVED';
  created_at: string;
}

/** The fields of GET /sessions/{id} that the form shows. */
export interface ReportedSession {
  id: string;
  subject_name: string;
  starts_at: string;
  ends_at: string;
  status: string;
  viewer_role?: 'STUDENT' | 'TUTOR';
}
