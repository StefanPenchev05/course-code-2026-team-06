BEGIN;

CREATE TYPE application_status AS ENUM ('PENDING', 'APPROVED', 'REJECTED');

CREATE TABLE tutor_profiles (
    student_id uuid PRIMARY KEY REFERENCES students (user_id),
    description text NOT NULL,
    approved_at timestamptz NOT NULL,
    is_active boolean NOT NULL DEFAULT true,
    approval_ended_at timestamptz,
    CHECK (approval_ended_at >= approved_at)
);

CREATE TABLE tutor_applications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id uuid NOT NULL REFERENCES students (user_id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    description text NOT NULL,
    status application_status NOT NULL DEFAULT 'PENDING',
    submitted_at timestamptz NOT NULL DEFAULT now(),
    reviewed_at timestamptz,
    reviewed_by_coordinator_id uuid REFERENCES coordinators (user_id),
    rejection_reason text,
    UNIQUE (id, student_id),
    CHECK (status = 'PENDING' OR (reviewed_at IS NOT NULL AND reviewed_by_coordinator_id IS NOT NULL)),
    CHECK (status <> 'REJECTED' OR (rejection_reason IS NOT NULL AND btrim(rejection_reason) <> '')),
    FOREIGN KEY (reviewed_by_coordinator_id, faculty_id) REFERENCES coordinators (user_id, faculty_id),
    CHECK (reviewed_at >= submitted_at),
    CHECK (status <> 'PENDING' OR (reviewed_at IS NULL AND reviewed_by_coordinator_id IS NULL))
);

CREATE INDEX tutor_applications_faculty_id_status_submitted_at_idx ON tutor_applications (faculty_id, status, submitted_at);

CREATE INDEX tutor_applications_student_id_idx ON tutor_applications (student_id);

CREATE TABLE tutor_qualifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES tutor_applications (id),
    subject_id uuid NOT NULL REFERENCES subjects (id),
    grade numeric(4,2) NOT NULL,
    student_id uuid NOT NULL,
    UNIQUE (application_id),
    UNIQUE (id, subject_id, student_id),
    CHECK (grade BETWEEN 15 AND 20),
    FOREIGN KEY (application_id, student_id) REFERENCES tutor_applications (id, student_id)
);

CREATE TABLE tutor_offerings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tutor_id uuid NOT NULL REFERENCES tutor_profiles (student_id),
    subject_id uuid NOT NULL REFERENCES subjects (id),
    qualification_id uuid NOT NULL,
    description text NOT NULL,
    is_active boolean NOT NULL DEFAULT true,
    deactivated_at timestamptz,
    deactivation_reason text,
    UNIQUE (tutor_id, subject_id),
    UNIQUE (id, tutor_id),
    UNIQUE (qualification_id),
    CHECK ((is_active AND deactivated_at IS NULL AND deactivation_reason IS NULL) OR (NOT is_active AND deactivated_at IS NOT NULL AND NULLIF(btrim(deactivation_reason), '') IS NOT NULL)),
    FOREIGN KEY (qualification_id, subject_id, tutor_id) REFERENCES tutor_qualifications (id, subject_id, student_id)
);

CREATE INDEX tutor_offerings_subject_id_tutor_id_idx ON tutor_offerings (subject_id, tutor_id);

CREATE UNIQUE INDEX tutor_applications_one_pending_idx
    ON tutor_applications (student_id) WHERE status = 'PENDING';

COMMIT;
