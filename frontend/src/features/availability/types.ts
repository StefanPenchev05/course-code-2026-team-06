import type { SessionMode } from '../../shared/utils/datetime';

/** Shapes from backend/internal/scheduling/domain/availability.go (tutor's own view). */

/** A published window. The owner view shows original bounds, including time a student has booked. */
export interface Window {
  id: string;
  tutor_id: string;
  starts_at: string;
  ends_at: string;
  allowed_mode: SessionMode;
  /** Generated from a weekly rule (removing it skips that date) or a one-off. */
  is_recurring: boolean;
}

/** A weekly rule: local weekday and clock times in an IANA zone, for one semester. */
export interface Rule {
  id: string;
  semester_id: string;
  /** ISO weekday: Monday = 1 … Sunday = 7. */
  iso_weekday: number;
  /** Local time in time_zone. The API returns "HH:MM:SS"; requests use "HH:MM". */
  local_start_time: string;
  local_end_time: string;
  time_zone: string;
  allowed_mode: SessionMode;
}

/** PATCH /me/availability/rules/{id}: every field is optional. */
export type RulePatch = Partial<Pick<Rule, 'iso_weekday' | 'local_start_time' | 'local_end_time' | 'time_zone' | 'allowed_mode'>>;

/** A skipped local date of a weekly rule. */
export interface SkippedDate {
  local_date: string;
  reason?: string;
}

export interface NewWindow {
  starts_at: string;
  ends_at: string;
  allowed_mode: SessionMode;
}

/** POST /me/availability/rules. Times are "HH:MM" local times in time_zone. */
export type NewRule = Pick<Rule, 'semester_id' | 'iso_weekday' | 'local_start_time' | 'local_end_time' | 'time_zone' | 'allowed_mode'>;

/** GET /api/v1/semesters (tutors): semesters that have not ended yet. */
export interface Semester {
  id: string;
  name: string;
  starts_at: string;
  ends_at: string;
}
