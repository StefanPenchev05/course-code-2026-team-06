/** Shapes from backend/internal/coordination (docs/coordination.md, "Attendance API" and "Reporting and adjudication"). */

export type NoShowStatus = 'PENDING' | 'CONFIRMED' | 'REJECTED';

export const NO_SHOW_STATUSES: NoShowStatus[] = ['PENDING', 'CONFIRMED', 'REJECTED'];

/**
 * One participant's allegation that the other one missed a session. People and the session
 * are named only by ID. Opposing reports about the same session are separate records.
 */
export interface NoShowReport {
  id: string;
  session_id: string;
  reporter_user_id: string;
  absent_user_id: string;
  status: NoShowStatus;
  description: string;
  reported_at: string;
  reviewed_at?: string;
  reviewed_by_coordinator_id?: string;
  resolution_note?: string;
}
