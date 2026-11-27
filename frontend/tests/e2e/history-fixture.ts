import { expect } from '@playwright/test';
import { runSql } from './database';

/**
 * An accepted historical session cannot be created via the normal booking API
 * (three-hour notice, immutable schedule). Seed this controlled starting state
 * without changing clocks, constraints or production code. The reference must
 * be a real API-created session for this test's approved tutor and learner.
 * Only the subsequent browser completion is claimed as an end-to-end mutation.
 */
export function historicalAcceptedSession(referenceId: string, description: string): string {
  const id = runSql(`
    BEGIN;
    CREATE TEMP TABLE historical_fixture(id uuid) ON COMMIT DROP;
    WITH source AS (SELECT * FROM tutoring_sessions WHERE id=:'reference'::uuid),
    historic_window AS (
      INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode)
      SELECT tutor_id,now()-interval '2 hours',now()-interval '1 hour','ONLINE' FROM source
      RETURNING id,tutor_id,starts_at,ends_at
    ), seeded AS (
      INSERT INTO tutoring_sessions(student_id,tutor_id,offering_id,availability_slot_id,policy_id,
        semester_id,faculty_id,starts_at,ends_at,duration_minutes,mode,purpose,description,status,
        requested_at,expires_at,responded_at,meeting_link)
      SELECT s.student_id,s.tutor_id,s.offering_id,w.id,s.policy_id,s.semester_id,s.faculty_id,
        w.starts_at,w.ends_at,60,'ONLINE','EXAM_PREPARATION',:'description','ACCEPTED',
        w.starts_at-interval '72 hours',w.starts_at-interval '24 hours',w.starts_at-interval '48 hours',
        'https://meeting.example/e2e-history'
      FROM source s JOIN historic_window w ON w.tutor_id=s.tutor_id
      RETURNING id
    ) INSERT INTO historical_fixture SELECT id FROM seeded;
    INSERT INTO session_status_history(session_id,to_status,changed_at,actor_kind,changed_by_user_id)
      SELECT s.id,'REQUESTED',s.requested_at,'USER',s.student_id FROM tutoring_sessions s JOIN historical_fixture f ON f.id=s.id;
    INSERT INTO session_status_history(session_id,from_status,to_status,changed_at,actor_kind,changed_by_user_id)
      SELECT s.id,'REQUESTED','ACCEPTED',s.responded_at,'USER',s.tutor_id FROM tutoring_sessions s JOIN historical_fixture f ON f.id=s.id;
    SELECT id::text FROM historical_fixture;
    COMMIT;
  `, { reference: referenceId, description });
  expect(id, 'one historical fixture was inserted').toMatch(/^[0-9a-f-]{36}$/);
  return id;
}

/**
 * A REQUESTED session whose real answer deadline passes `secondsLeft` seconds from now (#94).
 * Requests expire after at least an hour, longer than a test run, and the database derives
 * `expires_at` from `requested_at`, which is immutable. So, like historicalAcceptedSession,
 * this inserts one request as if it had been sent 48 hours ago, for a new one-hour free time
 * of the same tutor six days ahead, copying participants, offering, policy, faculty and
 * semester from a real API-created booking. All constraints and triggers stay on (the
 * insert itself emits the usual booking.changed). Only the expiry by the real worker and
 * what the browsers show afterwards are claimed as end-to-end behaviour.
 */
export function staleRequest(referenceId: string, description: string, secondsLeft: number): string {
  const id = runSql(`
    BEGIN;
    CREATE TEMP TABLE stale_fixture(id uuid) ON COMMIT DROP;
    WITH source AS (SELECT * FROM tutoring_sessions WHERE id=:'reference'::uuid),
    later_window AS (
      INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode)
      SELECT tutor_id,date_trunc('hour',now())+interval '6 days',date_trunc('hour',now())+interval '6 days 1 hour','ONLINE' FROM source
      RETURNING id,tutor_id,starts_at,ends_at
    ), sent AS (
      SELECT now()-interval '48 hours'+make_interval(secs => :'seconds'::int) AS requested_at
    ), seeded AS (
      INSERT INTO tutoring_sessions(student_id,tutor_id,offering_id,availability_slot_id,policy_id,
        semester_id,faculty_id,starts_at,ends_at,duration_minutes,mode,purpose,description,status,
        requested_at,expires_at)
      SELECT s.student_id,s.tutor_id,s.offering_id,w.id,s.policy_id,s.semester_id,s.faculty_id,
        w.starts_at,w.ends_at,60,'ONLINE','EXAM_PREPARATION',:'description','REQUESTED',
        sent.requested_at,least(sent.requested_at+interval '48 hours',w.starts_at-interval '24 hours')
      FROM source s JOIN later_window w ON w.tutor_id=s.tutor_id CROSS JOIN sent
      RETURNING id
    ) INSERT INTO stale_fixture SELECT id FROM seeded;
    INSERT INTO session_status_history(session_id,to_status,changed_at,actor_kind,changed_by_user_id)
      SELECT s.id,'REQUESTED',s.requested_at,'USER',s.student_id FROM tutoring_sessions s JOIN stale_fixture f ON f.id=s.id;
    SELECT id::text FROM stale_fixture;
    COMMIT;
  `, { reference: referenceId, description, seconds: String(secondsLeft) });
  expect(id, 'one stale request was inserted').toMatch(/^[0-9a-f-]{36}$/);
  return id;
}
