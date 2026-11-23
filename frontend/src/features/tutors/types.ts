/**
 * Shapes returned by the Tutoring and Scheduling APIs
 * (backend/internal/tutoring/domain/tutoring.go, backend/internal/scheduling/domain/availability.go).
 * They contain no grades, emails or student numbers: the API leaves those out of public views.
 */

export interface Offering {
  id: string;
  subject_id: string;
  subject_name: string;
  description: string;
  active: boolean;
}

export interface Tutor {
  id: string;
  name: string;
  description: string;
  active: boolean;
  /** null when nobody has rated this tutor yet. */
  average_rating: number | null;
  rating_count: number;
}

/** One search result: a tutor and the offering that matches the searched subject. */
export interface TutorMatch extends Tutor {
  offering: Offering;
}

export interface TutorProfile extends Tutor {
  offerings: Offering[];
}

export type SessionMode = 'ONLINE' | 'ONSITE' | 'BOTH';

/** A free interval the student can book. Booked time is already subtracted by the API. */
export interface FreeSlot {
  id: string;
  tutor_id: string;
  starts_at: string;
  ends_at: string;
  allowed_mode: SessionMode;
  is_recurring: boolean;
}

export interface TutorSearchFilters {
  subject_id: string;
  faculty_id?: string;
  level?: 'BACHELOR' | 'MASTER';
  offset?: number;
}
