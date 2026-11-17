BEGIN;

CREATE EXTENSION btree_gist;

CREATE TYPE degree_level AS ENUM ('BACHELOR', 'MASTER');

CREATE TABLE faculties (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL
);

CREATE TABLE programs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    faculty_id uuid NOT NULL REFERENCES faculties (id),
    name text NOT NULL,
    level degree_level NOT NULL
);

CREATE INDEX programs_faculty_id_level_idx ON programs (faculty_id, level);

CREATE TABLE subjects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE,
    name text NOT NULL
);

CREATE TABLE program_subjects (
    program_id uuid NOT NULL REFERENCES programs (id),
    subject_id uuid NOT NULL REFERENCES subjects (id),
    PRIMARY KEY (program_id, subject_id)
);

CREATE INDEX program_subjects_subject_id_program_id_idx ON program_subjects (subject_id, program_id);

CREATE TABLE semesters (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    CHECK (ends_at > starts_at),
    CHECK (isfinite(starts_at) AND isfinite(ends_at))
);

COMMIT;
