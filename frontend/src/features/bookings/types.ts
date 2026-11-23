import type { SessionMode } from '../../shared/utils/datetime';

/** Shapes from backend/internal/scheduling/domain/booking.go. Sessions carry IDs, not names or contact details. */

export type SessionStatus = 'REQUESTED' | 'ACCEPTED' | 'DECLINED' | 'EXPIRED' | 'CANCELLED' | 'COMPLETED' | 'NO_SHOW';

export type Purpose = 'INTRODUCTION' | 'HOMEWORK' | 'EXAM_PREPARATION' | 'COURSE_REVIEW' | 'OTHER';

/** A booked session's mode: the student picks one when the free time allows both. */
export type BookedMode = Exclude<SessionMode, 'BOTH'>;

export interface Session {
  id: string;
  student_id: string;
  tutor_id: string;
  subject_id: string;
  subject_name: string;
  offering_id: string;
  availability_slot_id?: string;
  semester_id: string;
  starts_at: string;
  ends_at: string;
  duration_minutes: number;
  mode: BookedMode;
  purpose: Purpose;
  description: string;
  status: SessionStatus;
  requested_at: string;
  /** A REQUESTED session expires at this instant if the tutor hasn't answered. */
  expires_at: string;
  responded_at?: string;
  decline_reason?: string;
  meeting_link?: string;
  meeting_room?: string;
  cancelled_at?: string;
  cancelled_by_user_id?: string;
  cancellation_reason?: string;
  is_late_cancellation: boolean;
  completed_at?: string;
  completed_by_tutor_id?: string;
  /** The caller's role in this session. */
  viewer_role?: 'STUDENT' | 'TUTOR';
}

/** POST /sessions */
export interface BookingRequest {
  offering_id: string;
  availability_slot_id: string;
  starts_at: string;
  duration_minutes: number;
  mode: BookedMode;
  purpose: Purpose;
  description: string;
}
