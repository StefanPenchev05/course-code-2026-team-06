BEGIN;

CREATE TABLE sponsors (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    branding_info text NOT NULL
);

CREATE TABLE sponsor_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sponsor_id uuid NOT NULL REFERENCES sponsors (id),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    prepared_by_coordinator_id uuid NOT NULL REFERENCES coordinators (user_id),
    period_start date NOT NULL,
    period_end date NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    active_users integer NOT NULL,
    sessions_held integer NOT NULL,
    average_feedback numeric(3,2),
    CHECK (period_end >= period_start),
    CHECK (active_users >= 0),
    CHECK (sessions_held >= 0),
    CHECK (average_feedback IS NULL OR average_feedback BETWEEN 1 AND 5),
    FOREIGN KEY (prepared_by_coordinator_id, faculty_id) REFERENCES coordinators (user_id, faculty_id)
);

CREATE INDEX sponsor_reports_faculty_id_period_start_period_end_idx ON sponsor_reports (faculty_id, period_start, period_end);

COMMIT;
