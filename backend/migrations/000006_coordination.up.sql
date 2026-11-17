BEGIN;

CREATE TYPE issue_status AS ENUM ('OPEN', 'RESOLVED');

CREATE TYPE attendance_report_status AS ENUM ('PENDING', 'CONFIRMED', 'REJECTED');

CREATE TYPE violation_type AS ENUM ('LATE_CANCELLATION', 'NO_SHOW');

CREATE TABLE feedback (
    session_id uuid PRIMARY KEY REFERENCES tutoring_sessions (id),
    rating smallint NOT NULL,
    comment text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (rating BETWEEN 1 AND 5)
);

CREATE TABLE issue_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_user_id uuid NOT NULL REFERENCES users (id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    session_id uuid REFERENCES tutoring_sessions (id),
    reported_tutor_id uuid REFERENCES tutor_profiles (student_id),
    subject_user_id uuid REFERENCES users (id),
    handled_by_coordinator_id uuid REFERENCES coordinators (user_id),
    description text NOT NULL,
    status issue_status NOT NULL DEFAULT 'OPEN',
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    resolution_note text,
    CHECK (btrim(description) <> ''),
    CHECK (status <> 'RESOLVED' OR (resolved_at IS NOT NULL AND handled_by_coordinator_id IS NOT NULL AND resolution_note IS NOT NULL AND btrim(resolution_note) <> '')),
    FOREIGN KEY (session_id, faculty_id) REFERENCES tutoring_sessions (id, faculty_id),
    FOREIGN KEY (handled_by_coordinator_id, faculty_id) REFERENCES coordinators (user_id, faculty_id),
    CHECK (resolved_at >= created_at)
);

CREATE INDEX issue_reports_faculty_id_status_created_at_idx ON issue_reports (faculty_id, status, created_at);

CREATE INDEX issue_reports_session_id_idx ON issue_reports (session_id);

CREATE TABLE attendance_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES tutoring_sessions (id),
    reporter_user_id uuid NOT NULL REFERENCES students (user_id),
    absent_user_id uuid NOT NULL REFERENCES students (user_id),
    status attendance_report_status NOT NULL DEFAULT 'PENDING',
    details text,
    reported_at timestamptz NOT NULL DEFAULT now(),
    reviewed_at timestamptz,
    reviewed_by_coordinator_id uuid REFERENCES coordinators (user_id),
    resolution_note text,
    UNIQUE (session_id, reporter_user_id),
    UNIQUE (id, session_id, absent_user_id, reviewed_by_coordinator_id, status),
    CHECK (reporter_user_id <> absent_user_id),
    CHECK ((status = 'PENDING' AND reviewed_at IS NULL AND reviewed_by_coordinator_id IS NULL AND resolution_note IS NULL)
        OR (status <> 'PENDING' AND reviewed_at IS NOT NULL AND reviewed_by_coordinator_id IS NOT NULL
            AND NULLIF(btrim(resolution_note), '') IS NOT NULL)),
    CHECK (reviewed_at >= reported_at)
);

CREATE INDEX attendance_reports_absent_user_id_reported_at_idx ON attendance_reports (absent_user_id, reported_at);

CREATE INDEX attendance_reports_status_reported_at_idx ON attendance_reports (status, reported_at);

CREATE TABLE user_violations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES students (user_id),
    semester_id uuid NOT NULL REFERENCES semesters (id),
    session_id uuid NOT NULL REFERENCES tutoring_sessions (id),
    kind violation_type NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    issue_report_id uuid REFERENCES issue_reports (id),
    attendance_report_id uuid REFERENCES attendance_reports (id),
    confirmed_by_coordinator_id uuid REFERENCES coordinators (user_id),
    attendance_confirmation attendance_report_status
        GENERATED ALWAYS AS (CASE WHEN kind = 'NO_SHOW' THEN 'CONFIRMED'::attendance_report_status END) STORED,
    UNIQUE (user_id, session_id),
    FOREIGN KEY (session_id, semester_id) REFERENCES tutoring_sessions (id, semester_id),
    FOREIGN KEY (attendance_report_id, session_id, user_id, confirmed_by_coordinator_id, attendance_confirmation)
        REFERENCES attendance_reports (id, session_id, absent_user_id, reviewed_by_coordinator_id, status),
    CHECK ((kind = 'NO_SHOW' AND attendance_report_id IS NOT NULL AND confirmed_by_coordinator_id IS NOT NULL)
        OR (kind = 'LATE_CANCELLATION' AND attendance_report_id IS NULL))
);

CREATE INDEX user_violations_user_id_semester_id_idx ON user_violations (user_id, semester_id);

CREATE INDEX user_violations_issue_report_id_idx ON user_violations (issue_report_id);

CREATE TABLE user_semester_flags (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES students (user_id),
    semester_id uuid NOT NULL REFERENCES semesters (id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    flagged_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, semester_id)
);

CREATE INDEX user_semester_flags_faculty_id_flagged_at_idx ON user_semester_flags (faculty_id, flagged_at);

CREATE FUNCTION validate_attendance_report() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    session tutoring_sessions%ROWTYPE;
BEGIN
    SELECT * INTO session FROM tutoring_sessions WHERE id = NEW.session_id;
    IF NOT FOUND THEN
        RAISE foreign_key_violation USING MESSAGE = 'attendance report session does not exist';
    END IF;
    IF NEW.reporter_user_id NOT IN (session.student_id, session.tutor_id)
       OR NEW.absent_user_id NOT IN (session.student_id, session.tutor_id)
       OR NEW.reported_at < session.ends_at
       OR NEW.reported_at > session.ends_at + interval '168 hours' THEN
        RAISE check_violation USING MESSAGE = 'attendance report must concern opposite participants within seven days';
    END IF;
    IF NEW.reviewed_by_coordinator_id IS NOT NULL THEN
        PERFORM 1 FROM coordinators
        WHERE user_id = NEW.reviewed_by_coordinator_id AND faculty_id = session.faculty_id
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE check_violation USING MESSAGE = 'attendance review requires the tutor faculty coordinator';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER attendance_reports_validate
BEFORE INSERT OR UPDATE ON attendance_reports
FOR EACH ROW EXECUTE FUNCTION validate_attendance_report();

COMMIT;
