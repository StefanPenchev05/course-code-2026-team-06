import type { SessionMode } from '../../shared/utils/datetime';

/** Shapes from backend/internal/feedback (docs/feedback.md). Entries never name the student who wrote them. */

/** POST /sessions/{id}/feedback. An empty comment is left out. */
export interface FeedbackInput {
  rating: number;
  comment?: string;
}

export interface FeedbackEntry {
  session_id: string;
  session_status: string;
  subject_id: string;
  subject_name: string;
  starts_at: string;
  /** Whole number from 1 to 5. */
  rating: number;
  comment?: string;
  created_at: string;
}

/** An accepted session of the tutor that has not started yet. */
export interface UpcomingSession {
  id: string;
  subject_id: string;
  subject_name: string;
  starts_at: string;
  ends_at: string;
  mode: Exclude<SessionMode, 'BOTH'>;
  meeting_link?: string;
  meeting_room?: string;
}

/** GET /tutors/me/activity. Totals count only sessions that are COMPLETED now; no-shows are left out. */
export interface TutorActivity {
  /** Rounded to two decimals; null while nobody has rated the tutor. */
  average_rating: number | null;
  rating_count: number;
  completed_sessions: number;
  /** Scheduled minutes of all completed sessions (rated or not) divided by 60. */
  hours_taught: number;
  upcoming_sessions: UpcomingSession[];
  pagination: { limit: number; offset: number; has_more: boolean };
}
