BEGIN;

CREATE TYPE verification_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED');

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    email text NOT NULL,
    password_hash text,
    is_suspended boolean NOT NULL DEFAULT false,
    suspended_at timestamptz,
    suspended_by_coordinator_id uuid,
    suspension_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    CHECK (NOT is_suspended OR (suspended_at IS NOT NULL AND suspended_by_coordinator_id IS NOT NULL AND suspension_reason IS NOT NULL AND btrim(suspension_reason) <> ''))
);

CREATE UNIQUE INDEX users_email_ci_idx ON users (lower(email));

CREATE TABLE students (
    user_id uuid PRIMARY KEY REFERENCES users (id),
    student_number text NOT NULL UNIQUE,
    program_id uuid NOT NULL REFERENCES programs (id)
);

CREATE INDEX students_program_id_idx ON students (program_id);

CREATE TABLE coordinators (
    user_id uuid PRIMARY KEY REFERENCES users (id),
    employee_number text NOT NULL UNIQUE,
    faculty_id uuid NOT NULL UNIQUE REFERENCES faculties (id),
    UNIQUE (user_id, faculty_id)
);

CREATE TABLE account_activation_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    coordinator_id uuid NOT NULL REFERENCES coordinators (user_id),
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CHECK (octet_length(token_hash) > 0),
    CHECK (expires_at > created_at),
    CHECK (used_at >= created_at AND used_at < expires_at)
);

CREATE INDEX account_activation_tokens_coordinator_id_expires_at_idx ON account_activation_tokens (coordinator_id, expires_at);

CREATE TABLE student_verifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id uuid NOT NULL REFERENCES students (user_id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    status verification_status NOT NULL DEFAULT 'PENDING',
    submitted_at timestamptz NOT NULL DEFAULT now(),
    reviewed_at timestamptz,
    reviewed_by_coordinator_id uuid REFERENCES coordinators (user_id),
    rejection_reason text,
    CHECK (status = 'PENDING' OR (reviewed_at IS NOT NULL AND reviewed_by_coordinator_id IS NOT NULL)),
    CHECK (status <> 'REJECTED' OR (rejection_reason IS NOT NULL AND btrim(rejection_reason) <> '')),
    FOREIGN KEY (reviewed_by_coordinator_id, faculty_id) REFERENCES coordinators (user_id, faculty_id),
    CHECK (reviewed_at >= submitted_at),
    CHECK (status <> 'PENDING' OR (reviewed_at IS NULL AND reviewed_by_coordinator_id IS NULL))
);

CREATE INDEX student_verifications_faculty_id_status_submitted_at_idx ON student_verifications (faculty_id, status, submitted_at);

CREATE INDEX student_verifications_student_id_idx ON student_verifications (student_id);

ALTER TABLE users ADD CONSTRAINT users_suspending_coordinator_fk
    FOREIGN KEY (suspended_by_coordinator_id) REFERENCES coordinators (user_id);

CREATE UNIQUE INDEX student_verifications_one_pending_idx
    ON student_verifications (student_id) WHERE status = 'PENDING';

COMMIT;
