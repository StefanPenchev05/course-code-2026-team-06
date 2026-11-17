BEGIN;

CREATE TYPE notification_type AS ENUM ('NEW_REQUEST', 'REQUEST_ACCEPTED', 'REQUEST_DECLINED', 'REQUEST_EXPIRED', 'SESSION_CANCELLED', 'APPLICATION_APPROVED', 'APPLICATION_REJECTED', 'STUDENT_VERIFIED', 'STUDENT_REJECTED', 'USER_FLAGGED', 'SESSION_REMINDER');

CREATE TYPE email_status AS ENUM ('PENDING', 'SENT', 'FAILED');

CREATE TYPE actor_kind AS ENUM ('USER', 'SYSTEM');

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL REFERENCES users (id),
    type notification_type NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    email_status email_status NOT NULL DEFAULT 'PENDING',
    created_at timestamptz NOT NULL DEFAULT now(),
    email_sent_at timestamptz,
    session_id uuid REFERENCES tutoring_sessions (id),
    application_id uuid REFERENCES tutor_applications (id),
    flag_id uuid REFERENCES user_semester_flags (id),
    verification_id uuid REFERENCES student_verifications (id),
    deduplication_key text NOT NULL,
    email_failed_at timestamptz,
    UNIQUE (recipient_user_id, deduplication_key),
    CHECK (email_status <> 'SENT' OR email_sent_at IS NOT NULL),
    CHECK (email_status <> 'FAILED' OR email_failed_at IS NOT NULL),
    CHECK (email_sent_at >= created_at AND email_failed_at >= created_at)
);

CREATE INDEX notifications_email_status_created_at_idx ON notifications (email_status, created_at);

CREATE INDEX notifications_session_id_idx ON notifications (session_id);

CREATE INDEX notifications_application_id_idx ON notifications (application_id);

CREATE TABLE user_suspension_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    changed_by_coordinator_id uuid NOT NULL,
    was_suspended boolean NOT NULL,
    is_suspended boolean NOT NULL,
    reason text NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (changed_by_coordinator_id, faculty_id) REFERENCES coordinators (user_id, faculty_id),
    CHECK (was_suspended <> is_suspended AND btrim(reason) <> '')
);

CREATE INDEX user_suspension_history_user_id_changed_at_idx ON user_suspension_history (user_id, changed_at);

CREATE INDEX user_suspension_history_faculty_id_changed_at_idx ON user_suspension_history (faculty_id, changed_at);

CREATE TABLE session_status_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES tutoring_sessions (id),
    from_status session_status,
    to_status session_status NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    actor_kind actor_kind NOT NULL,
    changed_by_user_id uuid REFERENCES users (id),
    reason text,
    CHECK ((actor_kind = 'USER' AND changed_by_user_id IS NOT NULL) OR (actor_kind = 'SYSTEM' AND changed_by_user_id IS NULL))
);

CREATE INDEX session_status_history_session_id_changed_at_idx ON session_status_history (session_id, changed_at);

CREATE TABLE application_status_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES tutor_applications (id),
    from_status application_status,
    to_status application_status NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    actor_kind actor_kind NOT NULL,
    changed_by_user_id uuid REFERENCES users (id),
    reason text,
    CHECK ((actor_kind = 'USER' AND changed_by_user_id IS NOT NULL) OR (actor_kind = 'SYSTEM' AND changed_by_user_id IS NULL))
);

CREATE INDEX application_status_history_application_id_changed_at_idx ON application_status_history (application_id, changed_at);

COMMIT;
