CREATE SCHEMA tutti_test;
SET search_path = tutti_test, public;

CREATE FUNCTION expect_state(expected text, statement text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        IF SQLSTATE <> expected THEN
            RAISE EXCEPTION 'expected %, got % (%): %', expected, SQLSTATE, SQLERRM, statement;
        END IF;
        RETURN;
    END;
    RAISE EXCEPTION 'expected failure %: %', expected, statement;
END;
$$;

-- Fixed fixture identities for reuse across the two concurrency connections.
CREATE FUNCTION fixture_id(n int) RETURNS uuid
LANGUAGE sql IMMUTABLE AS $$ SELECT lpad(n::text, 32, '0')::uuid $$;

CREATE FUNCTION book(student int, tutor int, start_time timestamptz,
                     minutes smallint, purpose booking_purpose,
                     notice interval DEFAULT interval '3 hours',
                     expiry_shift interval DEFAULT interval '0 seconds') RETURNS uuid
LANGUAGE sql SET search_path = tutti_test, public AS $$
    INSERT INTO tutoring_sessions
        (student_id, tutor_id, offering_id, availability_slot_id, policy_id, semester_id,
         faculty_id, starts_at, ends_at, duration_minutes, mode, purpose, description,
         requested_at, expires_at)
    VALUES (fixture_id(student), fixture_id(tutor), fixture_id(tutor + 30),
            fixture_id(tutor + 40), fixture_id(51), fixture_id(52), fixture_id(1),
            start_time, start_time + minutes * interval '1 minute', minutes, 'ONLINE',
            purpose, 'Test request', start_time - notice,
            CASE WHEN notice <= interval '25 hours' THEN start_time - notice + interval '1 hour'
                 WHEN notice <= interval '72 hours' THEN start_time - interval '24 hours'
                 ELSE start_time - notice + interval '48 hours' END + expiry_shift)
    RETURNING id
$$;

INSERT INTO faculties (id, name) VALUES (fixture_id(1), 'Science'), (fixture_id(2), 'Other');
INSERT INTO programs VALUES (fixture_id(3), fixture_id(1), 'Computing', 'BACHELOR');
INSERT INTO subjects VALUES (fixture_id(4), 'CS101', 'Computing'), (fixture_id(5), 'CS102', 'Data');
INSERT INTO program_subjects VALUES (fixture_id(3), fixture_id(4)), (fixture_id(3), fixture_id(5));
INSERT INTO users (id, name, email)
SELECT fixture_id(n), 'User ' || n, 'u' || n || '@example.test' FROM generate_series(10, 14) AS n;
INSERT INTO students
SELECT fixture_id(n), n::text, fixture_id(3) FROM generate_series(10, 12) AS n;
INSERT INTO coordinators VALUES (fixture_id(13), 'E13', fixture_id(1)), (fixture_id(14), 'E14', fixture_id(2));
INSERT INTO student_verifications (student_id, faculty_id) VALUES (fixture_id(10), fixture_id(1));
INSERT INTO tutor_profiles (student_id, description, approved_at)
SELECT fixture_id(n), 'Tutor ' || n, now() FROM generate_series(10, 12) AS n;
INSERT INTO tutor_applications (id, student_id, faculty_id, description, status, reviewed_at, reviewed_by_coordinator_id)
SELECT fixture_id(n + 10), fixture_id(n), fixture_id(1), 'Application', 'APPROVED', now(), fixture_id(13)
FROM generate_series(10, 12) AS n;
INSERT INTO tutor_qualifications (id, application_id, student_id, subject_id, grade)
SELECT fixture_id(n + 20), fixture_id(n + 10), fixture_id(n), fixture_id(4), 15.25
FROM generate_series(10, 12) AS n;
INSERT INTO tutor_offerings (id, tutor_id, subject_id, qualification_id, description)
SELECT fixture_id(n + 30), fixture_id(n), fixture_id(4), fixture_id(n + 20), 'Offering'
FROM generate_series(10, 12) AS n;
INSERT INTO session_policies (id) VALUES (fixture_id(51));
INSERT INTO semesters VALUES (fixture_id(52), 'Test term', '2030-01-01 UTC', '2030-06-01 UTC');
INSERT INTO availability_slots (id, tutor_id, starts_at, ends_at, allowed_mode)
SELECT fixture_id(n + 40), fixture_id(n), '2030-02-01 08:00 UTC', '2030-02-01 22:00 UTC', 'BOTH'
FROM generate_series(10, 12) AS n;

SELECT expect_state('23503', 'INSERT INTO programs (faculty_id, name, level) VALUES (fixture_id(99), ''Bad'', ''MASTER'')');
SELECT expect_state('23505', 'INSERT INTO subjects (code, name) VALUES (''CS101'', ''Duplicate'')');
SELECT expect_state('23505', 'INSERT INTO users (name, email) VALUES (''Duplicate'', ''U10@EXAMPLE.TEST'')');
SELECT expect_state('23505', 'INSERT INTO coordinators VALUES (fixture_id(10), ''E10'', fixture_id(1))');
SELECT expect_state('23505', 'INSERT INTO student_verifications (student_id, faculty_id) VALUES (fixture_id(10), fixture_id(1))');
INSERT INTO tutor_applications (id, student_id, faculty_id, description)
VALUES (fixture_id(60), fixture_id(10), fixture_id(1), 'Another subject');
SELECT expect_state('23505', 'INSERT INTO tutor_applications (student_id, faculty_id, description) VALUES (fixture_id(10), fixture_id(1), ''Duplicate'')');
SELECT expect_state('23514', 'INSERT INTO tutor_qualifications (application_id, student_id, subject_id, grade) VALUES (fixture_id(60), fixture_id(10), fixture_id(5), 14.99)');
SELECT expect_state('23514', 'UPDATE tutor_qualifications SET grade = 20.01 WHERE id = fixture_id(30)');
SELECT expect_state('23503', 'INSERT INTO tutor_qualifications (application_id, student_id, subject_id, grade) VALUES (fixture_id(60), fixture_id(11), fixture_id(5), 20.00)');
INSERT INTO tutor_qualifications (id, application_id, student_id, subject_id, grade)
VALUES (fixture_id(61), fixture_id(60), fixture_id(10), fixture_id(5), 20.00);
SELECT expect_state('23505', 'INSERT INTO tutor_qualifications (application_id, student_id, subject_id, grade) VALUES (fixture_id(60), fixture_id(10), fixture_id(4), 15.00)');
SELECT expect_state('23503', 'UPDATE tutor_offerings SET qualification_id = fixture_id(61) WHERE id = fixture_id(40)');
SELECT expect_state('23503', 'INSERT INTO tutor_offerings (tutor_id, subject_id, qualification_id, description) VALUES (fixture_id(11), fixture_id(5), fixture_id(61), ''Wrong owner'')');
SELECT expect_state('23P01', 'INSERT INTO availability_slots (tutor_id, starts_at, ends_at, allowed_mode) VALUES (fixture_id(10), ''2030-02-01 09:00 UTC'', ''2030-02-01 10:00 UTC'', ''BOTH'')');

SELECT expect_state('23514', 'SELECT book(10, 10, ''2030-02-01 08:00 UTC'', 30::smallint, ''INTRODUCTION'')');
SELECT expect_state('23514', 'SELECT book(10, 11, ''2030-02-01 08:00 UTC'', 60::smallint, ''INTRODUCTION'')');
SELECT expect_state('23514', 'SELECT book(10, 11, ''2030-02-01 08:00 UTC'', 30::smallint, ''OTHER'')');
SELECT expect_state('23514', 'SELECT book(10, 11, ''2030-02-01 08:00 UTC'', 30::smallint, ''INTRODUCTION'', interval ''2 hours 59 minutes'')');
SELECT expect_state('23514', 'SELECT book(10, 11, ''2030-02-01 08:00 UTC'', 30::smallint, ''INTRODUCTION'', interval ''25 hours 1 second'', interval ''1 second'')');
SELECT book(10, 11, '2030-02-01 08:00 UTC', 30::smallint, 'INTRODUCTION') AS booking_id \gset
SELECT book(12, 10, '2030-02-01 08:30 UTC', 60::smallint, 'OTHER', interval '25 hours');
SELECT expect_state('23P01', 'SELECT book(12, 10, ''2030-02-01 08:00 UTC'', 30::smallint, ''INTRODUCTION'')');
SELECT expect_state('23514', 'DELETE FROM availability_slots WHERE id = fixture_id(51)');
SELECT expect_state('23514', 'DELETE FROM session_participant_blocks WHERE user_id = fixture_id(11)');
SELECT expect_state('23514', 'TRUNCATE session_participant_blocks');
SELECT expect_state('22P02', 'UPDATE tutoring_sessions SET status = ''TYPO''');
SELECT expect_state('23502', 'UPDATE tutoring_sessions SET status = NULL');

DO $$
BEGIN
    IF (SELECT count(*) FROM tutoring_sessions) <> 2
       OR (SELECT count(*) FROM session_participant_blocks) <> 4 THEN
        RAISE EXCEPTION 'expected exactly two sessions and four blocks after failed writes';
    END IF;
END;
$$;

UPDATE tutoring_sessions SET status = 'ACCEPTED', meeting_link = 'test-link' WHERE id = :'booking_id';
SELECT expect_state('23P01', 'SELECT book(12, 10, ''2030-02-01 08:00 UTC'', 30::smallint, ''INTRODUCTION'')');
UPDATE tutoring_sessions SET status = 'COMPLETED', completed_at = ends_at, completed_by_tutor_id = tutor_id WHERE id = :'booking_id';
DELETE FROM availability_slots WHERE id = fixture_id(51);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM tutoring_sessions
                   WHERE status = 'COMPLETED' AND availability_slot_id IS NULL) THEN
        RAISE EXCEPTION 'deleting a historical slot must preserve its session';
    END IF;
END;
$$;
SELECT book(12, 10, '2030-02-01 08:00 UTC', 30::smallint, 'INTRODUCTION', interval '25 hours 1 second');
SELECT book(12, 10, '2030-02-01 09:30 UTC', 30::smallint, 'INTRODUCTION', interval '48 hours');
SELECT book(12, 10, '2030-02-01 10:00 UTC', 30::smallint, 'INTRODUCTION', interval '72 hours');
SELECT book(12, 10, '2030-02-01 10:30 UTC', 30::smallint, 'INTRODUCTION', interval '24 hours');
-- Recreate the vacated tutor window for the independent concurrency fixture.
INSERT INTO availability_slots (id, tutor_id, starts_at, ends_at, allowed_mode)
VALUES (fixture_id(51), fixture_id(11), '2030-02-01 08:00 UTC', '2030-02-01 22:00 UTC', 'BOTH');

INSERT INTO feedback (session_id, rating) VALUES (:'booking_id', 5);
SELECT expect_state('23505', 'INSERT INTO feedback (session_id, rating) SELECT session_id, 4 FROM feedback');
INSERT INTO attendance_reports (id, session_id, reporter_user_id, absent_user_id, reported_at)
VALUES (fixture_id(70), :'booking_id', fixture_id(10), fixture_id(11), '2030-02-01 08:30 UTC');
SELECT expect_state('23514', 'UPDATE attendance_reports SET status = ''CONFIRMED'', reviewed_at = reported_at, resolution_note = ''Wrong faculty'', reviewed_by_coordinator_id = fixture_id(14)');
UPDATE attendance_reports SET status = 'REJECTED', reviewed_at = reported_at, resolution_note = 'Rejected', reviewed_by_coordinator_id = fixture_id(13);
SELECT expect_state('23503', 'INSERT INTO user_violations (user_id, semester_id, session_id, kind, occurred_at, attendance_report_id, confirmed_by_coordinator_id) SELECT absent_user_id, fixture_id(52), session_id, ''NO_SHOW'', reported_at, id, fixture_id(13) FROM attendance_reports');
UPDATE attendance_reports SET status = 'CONFIRMED', resolution_note = 'Confirmed in fixture';
INSERT INTO user_violations (user_id, semester_id, session_id, kind, occurred_at, attendance_report_id, confirmed_by_coordinator_id)
SELECT absent_user_id, fixture_id(52), session_id, 'NO_SHOW', reported_at, id, fixture_id(13) FROM attendance_reports;
SELECT expect_state('23505', 'INSERT INTO user_violations (user_id, semester_id, session_id, kind, occurred_at, attendance_report_id, confirmed_by_coordinator_id) SELECT user_id, semester_id, session_id, kind, occurred_at, attendance_report_id, confirmed_by_coordinator_id FROM user_violations');


SELECT book(10, 11, '2030-02-01 15:00 UTC', 30::smallint, 'INTRODUCTION') AS release_id \gset
UPDATE tutoring_sessions SET status = 'DECLINED', decline_reason = 'Declined' WHERE id = :'release_id';
SELECT book(10, 11, '2030-02-01 15:00 UTC', 30::smallint, 'INTRODUCTION') AS release_id \gset
UPDATE tutoring_sessions SET status = 'EXPIRED' WHERE id = :'release_id';
SELECT book(10, 11, '2030-02-01 15:00 UTC', 30::smallint, 'INTRODUCTION') AS release_id \gset
UPDATE tutoring_sessions
SET status = 'CANCELLED', cancelled_at = requested_at,
    cancelled_by_user_id = student_id, cancellation_reason = 'Cancelled'
WHERE id = :'release_id';
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM session_participant_blocks WHERE starts_at = '2030-02-01 15:00 UTC') THEN
        RAISE EXCEPTION 'terminal sessions must release both participant blocks';
    END IF;
END;
$$;
