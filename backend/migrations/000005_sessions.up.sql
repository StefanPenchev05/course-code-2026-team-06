BEGIN;

CREATE TYPE session_mode AS ENUM ('ONLINE', 'ONSITE');

CREATE TYPE booking_purpose AS ENUM ('INTRODUCTION', 'HOMEWORK', 'EXAM_PREPARATION', 'COURSE_REVIEW', 'OTHER');

CREATE TYPE session_status AS ENUM ('REQUESTED', 'ACCEPTED', 'DECLINED', 'EXPIRED', 'CANCELLED', 'COMPLETED', 'NO_SHOW');

CREATE TYPE session_participant_role AS ENUM ('STUDENT', 'TUTOR');

CREATE TABLE tutoring_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id uuid NOT NULL REFERENCES students (user_id),
    tutor_id uuid NOT NULL REFERENCES tutor_profiles (student_id),
    offering_id uuid NOT NULL,
    availability_slot_id uuid,
    policy_id uuid NOT NULL REFERENCES session_policies (id),
    semester_id uuid NOT NULL REFERENCES semesters (id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    duration_minutes smallint NOT NULL,
    mode session_mode NOT NULL,
    purpose booking_purpose NOT NULL,
    description text NOT NULL,
    status session_status NOT NULL DEFAULT 'REQUESTED',
    requested_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    responded_at timestamptz,
    decline_reason text,
    meeting_link text,
    meeting_room text,
    cancelled_at timestamptz,
    cancelled_by_user_id uuid REFERENCES users (id),
    cancellation_reason text,
    is_late_cancellation boolean NOT NULL DEFAULT false,
    completed_at timestamptz,
    completed_by_tutor_id uuid REFERENCES tutor_profiles (student_id),
    UNIQUE (id, semester_id),
    UNIQUE (id, faculty_id),
    CHECK (ends_at > starts_at),
    CHECK (student_id <> tutor_id),
    CHECK (duration_minutes IN (30, 60, 90, 120)),
    CHECK (ends_at - starts_at = duration_minutes * interval '1 minute'),
    CHECK ((purpose = 'INTRODUCTION' AND duration_minutes = 30) OR (purpose <> 'INTRODUCTION' AND duration_minutes IN (60, 90, 120))),
    CHECK (status <> 'DECLINED' OR (decline_reason IS NOT NULL AND btrim(decline_reason) <> '')),
    CHECK (status <> 'CANCELLED' OR (cancelled_at IS NOT NULL AND cancellation_reason IS NOT NULL AND btrim(cancellation_reason) <> '')),
    CHECK (NOT is_late_cancellation OR status = 'CANCELLED'),
    CHECK (status NOT IN ('ACCEPTED', 'COMPLETED', 'NO_SHOW') OR ((mode = 'ONLINE' AND meeting_link IS NOT NULL AND btrim(meeting_link) <> '') OR (mode = 'ONSITE' AND meeting_room IS NOT NULL AND btrim(meeting_room) <> ''))),
    CHECK (isfinite(starts_at) AND isfinite(ends_at)),
    FOREIGN KEY (offering_id, tutor_id) REFERENCES tutor_offerings (id, tutor_id),
    FOREIGN KEY (availability_slot_id, tutor_id) REFERENCES availability_slots (id, tutor_id) ON DELETE SET NULL (availability_slot_id),
    CHECK (status NOT IN ('REQUESTED', 'ACCEPTED') OR availability_slot_id IS NOT NULL),
    CHECK (isfinite(requested_at) AND starts_at - requested_at >= interval '3 hours'),
    CHECK (expires_at = CASE
        WHEN starts_at - requested_at <= interval '25 hours'
        THEN least(requested_at + interval '1 hour', starts_at - interval '2 hours')
        ELSE least(requested_at + interval '48 hours', starts_at - interval '24 hours')
    END),
    CHECK (responded_at >= requested_at),
    CHECK (cancelled_at >= requested_at),
    CHECK (completed_at IS NULL OR (completed_at >= ends_at AND completed_by_tutor_id IS NOT DISTINCT FROM tutor_id)),
    CHECK (status <> 'COMPLETED' OR completed_at IS NOT NULL)
);

CREATE INDEX tutoring_sessions_student_id_starts_at_idx ON tutoring_sessions (student_id, starts_at);

CREATE INDEX tutoring_sessions_tutor_id_starts_at_idx ON tutoring_sessions (tutor_id, starts_at);

CREATE INDEX tutoring_sessions_faculty_id_status_starts_at_idx ON tutoring_sessions (faculty_id, status, starts_at);

CREATE INDEX tutoring_sessions_semester_id_status_starts_at_idx ON tutoring_sessions (semester_id, status, starts_at);

CREATE INDEX tutoring_sessions_offering_id_idx ON tutoring_sessions (offering_id);

CREATE INDEX tutoring_sessions_availability_slot_id_idx ON tutoring_sessions (availability_slot_id);

CREATE INDEX tutoring_sessions_expires_at_idx ON tutoring_sessions (expires_at) WHERE status = 'REQUESTED';

CREATE TABLE session_participant_blocks (
    session_id uuid NOT NULL REFERENCES tutoring_sessions (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES students (user_id),
    participant_role session_participant_role NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    PRIMARY KEY (session_id, user_id),
    UNIQUE (session_id, participant_role),
    CHECK (ends_at > starts_at),
    EXCLUDE USING gist (user_id WITH =, tstzrange(starts_at, ends_at, '[)') WITH &&),
    CHECK (isfinite(starts_at) AND isfinite(ends_at))
);

CREATE INDEX session_participant_blocks_user_id_starts_at_idx ON session_participant_blocks (user_id, starts_at);

-- A session's participants and reserved interval are fixed when requested.
-- To schedule a different interval, release this request and create a new one.
CREATE FUNCTION validate_tutoring_session() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    slot availability_slots%ROWTYPE;
    semester semesters%ROWTYPE;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF ROW(NEW.student_id, NEW.tutor_id, NEW.offering_id, NEW.semester_id,
               NEW.faculty_id, NEW.starts_at, NEW.ends_at, NEW.requested_at, NEW.mode)
           IS DISTINCT FROM
           ROW(OLD.student_id, OLD.tutor_id, OLD.offering_id, OLD.semester_id,
               OLD.faculty_id, OLD.starts_at, OLD.ends_at, OLD.requested_at, OLD.mode) THEN
            RAISE check_violation USING MESSAGE = 'session identity and schedule are immutable';
        END IF;
        IF NEW.availability_slot_id IS DISTINCT FROM OLD.availability_slot_id
           AND NEW.availability_slot_id IS NOT NULL THEN
            RAISE check_violation USING MESSAGE = 'cannot replace a session source slot';
        END IF;
    ELSE
        SELECT * INTO semester FROM semesters WHERE id = NEW.semester_id FOR SHARE;
        IF NOT FOUND THEN
            RAISE foreign_key_violation USING MESSAGE = 'session semester does not exist';
        END IF;
        IF NEW.starts_at < semester.starts_at OR NEW.ends_at > semester.ends_at THEN
            RAISE check_violation USING MESSAGE = 'session must fit its semester';
        END IF;
        IF NEW.availability_slot_id IS NULL THEN
            RAISE check_violation USING MESSAGE = 'new session requires a source slot';
        END IF;
        SELECT * INTO slot FROM availability_slots WHERE id = NEW.availability_slot_id FOR SHARE;
        IF NOT FOUND THEN
            RAISE foreign_key_violation USING MESSAGE = 'session source slot does not exist';
        END IF;
        IF NEW.starts_at < slot.starts_at OR NEW.ends_at > slot.ends_at
           OR (slot.allowed_mode <> 'BOTH' AND slot.allowed_mode::text <> NEW.mode::text) THEN
            RAISE check_violation USING MESSAGE = 'session time and mode must fit its source slot';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER tutoring_sessions_validate
BEFORE INSERT OR UPDATE ON tutoring_sessions
FOR EACH ROW EXECUTE FUNCTION validate_tutoring_session();

-- Store both roles in one exclusion index. Inserting either conflicting block
-- rolls back the parent mutation and any block already inserted by this trigger.
CREATE FUNCTION sync_session_participant_blocks() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status IN ('REQUESTED', 'ACCEPTED') THEN
            IF NEW.status NOT IN ('REQUESTED', 'ACCEPTED') THEN
                DELETE FROM session_participant_blocks WHERE session_id = NEW.id;
            END IF;
            RETURN NEW;
        END IF;
    END IF;
    IF NEW.status IN ('REQUESTED', 'ACCEPTED') THEN
        INSERT INTO session_participant_blocks
            (session_id, user_id, participant_role, starts_at, ends_at)
        SELECT NEW.id, p.user_id, p.participant_role, NEW.starts_at, NEW.ends_at
        FROM (VALUES (NEW.student_id, 'STUDENT'::session_participant_role),
                     (NEW.tutor_id, 'TUTOR'::session_participant_role)) AS p(user_id, participant_role)
        ORDER BY p.user_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER tutoring_sessions_sync_blocks
AFTER INSERT OR UPDATE OF status ON tutoring_sessions
FOR EACH ROW EXECUTE FUNCTION sync_session_participant_blocks();

CREATE FUNCTION guard_session_participant_blocks() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    session tutoring_sessions%ROWTYPE;
BEGIN
    IF TG_OP IN ('UPDATE', 'TRUNCATE') THEN
        RAISE check_violation USING MESSAGE = 'participant blocks are managed by session changes';
    END IF;
    IF TG_OP = 'DELETE' THEN
        SELECT * INTO session FROM tutoring_sessions WHERE id = OLD.session_id FOR UPDATE;
        IF session.status IN ('REQUESTED', 'ACCEPTED') THEN
            RAISE check_violation USING MESSAGE = 'cannot remove an active participant block';
        END IF;
        RETURN OLD;
    END IF;

    SELECT * INTO session FROM tutoring_sessions WHERE id = NEW.session_id FOR UPDATE;
    IF NOT FOUND OR session.status NOT IN ('REQUESTED', 'ACCEPTED')
       OR NEW.starts_at <> session.starts_at OR NEW.ends_at <> session.ends_at
       OR NEW.user_id <> (CASE NEW.participant_role WHEN 'STUDENT' THEN session.student_id ELSE session.tutor_id END) THEN
        RAISE check_violation USING MESSAGE = 'participant block must match an active session';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER session_participant_blocks_guard
BEFORE INSERT OR UPDATE OR DELETE ON session_participant_blocks
FOR EACH ROW EXECUTE FUNCTION guard_session_participant_blocks();

CREATE TRIGGER session_participant_blocks_guard_truncate
BEFORE TRUNCATE ON session_participant_blocks
FOR EACH STATEMENT EXECUTE FUNCTION guard_session_participant_blocks();

COMMIT;
