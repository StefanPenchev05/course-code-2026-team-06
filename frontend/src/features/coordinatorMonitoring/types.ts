import type { Page } from '../../shared/api/client';
import type { SessionMode } from '../../shared/utils/datetime';

/**
 * Shapes from backend/internal/coordination (docs/coordination.md, "Tutor monitoring" and
 * "Policy-based session cancellation"). The server scopes everything to the coordinator's
 * faculty. Monitoring never contains grades, student identities or meeting details.
 */

/** Totals count only sessions of this faculty that are COMPLETED now; no-shows are left out. */
export interface ActivityTotals {
  completed_sessions: number;
  /** Scheduled minutes of completed sessions divided by 60. */
  hours_taught: number;
  /** Two decimals; null while there are no ratings. */
  average_rating: number | null;
  rating_count: number;
}

export interface TutorSummary extends ActivityTotals {
  id: string;
  name: string;
  program_id: string;
  program_name: string;
  faculty_id: string;
  approved_at: string;
  /** Can be booked now: active profile and account, verified, at least one active subject. */
  active: boolean;
}

export interface Offering {
  id: string;
  subject_id: string;
  subject_name: string;
  description: string;
  active: boolean;
}

export interface TutorDetail extends TutorSummary {
  offerings: Offering[];
}

export interface FacultyStatistics extends ActivityTotals {
  faculty_id: string;
  approved_tutors: number;
  active_tutors: number;
}

export type SessionStatus = 'REQUESTED' | 'ACCEPTED' | 'DECLINED' | 'EXPIRED' | 'CANCELLED' | 'COMPLETED' | 'NO_SHOW';

/** A published availability window or a session that overlaps the requested range. */
export interface ScheduleEntry {
  id: string;
  kind: 'AVAILABILITY' | 'SESSION';
  starts_at: string;
  ends_at: string;
  mode: SessionMode;
  /** Sessions only. */
  status?: SessionStatus;
  subject_id?: string;
  subject_name?: string;
  /** Weekly availability only. */
  rule_id?: string;
  time_zone?: string;
  semester_id?: string;
}

/** POST /coordinator/sessions/{id}/cancel answer. Never counts as a late cancellation. */
export interface PolicyCancellation {
  id: string;
  status: SessionStatus;
  starts_at: string;
  ends_at: string;
  cancelled_at: string;
  cancelled_by_user_id: string;
  cancellation_reason: string;
  cancellation_origin: string;
  is_late_cancellation: boolean;
}

/** Feedback on a completed session of this faculty; the comment is private to authorized viewers. */
export interface FeedbackEntry {
  session_id: string;
  session_status: string;
  subject_id: string;
  subject_name: string;
  starts_at: string;
  rating: number;
  comment?: string;
  created_at: string;
}

export interface FeedbackPage extends Page<FeedbackEntry> {
  average_rating: number | null;
  rating_count: number;
}
