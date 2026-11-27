/**
 * Shapes from backend/internal/coordination (docs/coordination.md: flagged-user review #48,
 * suspension #25; docs/audit-history.md: audit history #26). Accounts are named only by ID:
 * the API exposes no names, emails, grades or evidence here.
 */

/** A semester flag stored for the coordinator's faculty (two or more violations in that semester). */
export interface FlaggedUser {
  flag_id: string;
  user_id: string;
  semester_id: string;
  /** Current number of violations for that user and semester, including later ones. */
  strike_count: number;
  flagged_at: string;
  /** Current account state, not derived from the flag. */
  is_suspended: boolean;
}

/** POST /coordinator/users/{id}/suspend | reinstate answer. */
export interface SuspensionDecision {
  user_id: string;
  decision_id: string;
  is_suspended: boolean;
  changed_at: string;
  /** Future requested or confirmed sessions cancelled by a suspension (0 for a reinstatement). */
  cancelled_sessions: number;
}

export type AuditCategory = 'SUSPENSION' | 'VIOLATION' | 'FLAG';

export const AUDIT_CATEGORIES: AuditCategory[] = ['SUSPENSION', 'VIOLATION', 'FLAG'];

/** One history event visible to this faculty; which optional fields are set depends on the category. */
export interface AuditEvent {
  id: string;
  user_id: string;
  faculty_id: string;
  category: AuditCategory;
  recorded_at: string;
  /** USER or SYSTEM; null for legacy records whose actor is unknown. */
  actor_kind: string | null;
  actor_user_id: string | null;
  /** SUSPENSION */
  was_suspended?: boolean;
  is_suspended?: boolean;
  /** SUSPENSION: the private administrative reason. */
  reason?: string;
  /** VIOLATION: NO_SHOW or LATE_CANCELLATION. */
  violation_kind?: string;
  semester_id?: string;
  session_id?: string;
  occurred_at?: string;
  attendance_report_id?: string;
  cancellation_history_id?: string;
  /** FLAG: the two violations that created it. */
  first_violation_id?: string;
  second_violation_id?: string;
}
