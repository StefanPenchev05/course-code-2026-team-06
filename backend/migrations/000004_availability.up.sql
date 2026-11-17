BEGIN;

CREATE TYPE slot_mode AS ENUM ('ONLINE', 'ONSITE', 'BOTH');

CREATE TABLE session_policies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cancellation_deadline_hours smallint NOT NULL DEFAULT 24,
    request_expiry_hours smallint NOT NULL DEFAULT 48,
    strike_limit smallint NOT NULL DEFAULT 2,
    min_booking_notice_hours smallint NOT NULL DEFAULT 3,
    urgent_notice_max_hours smallint NOT NULL DEFAULT 25,
    urgent_request_expiry_hours smallint NOT NULL DEFAULT 1,
    urgent_start_buffer_hours smallint NOT NULL DEFAULT 2,
    standard_start_buffer_hours smallint NOT NULL DEFAULT 24,
    CHECK (cancellation_deadline_hours = 24),
    CHECK (request_expiry_hours = 48),
    CHECK (strike_limit = 2),
    CHECK (min_booking_notice_hours = 3),
    CHECK (urgent_notice_max_hours = 25),
    CHECK (urgent_request_expiry_hours = 1),
    CHECK (urgent_start_buffer_hours = 2),
    CHECK (standard_start_buffer_hours = 24)
);

CREATE TABLE availability_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tutor_id uuid NOT NULL REFERENCES tutor_profiles (student_id),
    semester_id uuid NOT NULL REFERENCES semesters (id),
    iso_weekday smallint NOT NULL,
    local_start_time time NOT NULL,
    local_end_time time NOT NULL,
    time_zone text NOT NULL,
    allowed_mode slot_mode NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, tutor_id),
    CHECK (iso_weekday BETWEEN 1 AND 7),
    CHECK (local_end_time > local_start_time)
);

CREATE INDEX availability_rules_tutor_id_semester_id_iso_weekday_idx ON availability_rules (tutor_id, semester_id, iso_weekday);

CREATE TABLE availability_exceptions (
    rule_id uuid NOT NULL REFERENCES availability_rules (id) ON DELETE CASCADE,
    local_date date NOT NULL,
    reason text,
    PRIMARY KEY (rule_id, local_date)
);

CREATE TABLE availability_slots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tutor_id uuid NOT NULL REFERENCES tutor_profiles (student_id),
    rule_id uuid,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    allowed_mode slot_mode NOT NULL,
    UNIQUE (id, tutor_id),
    UNIQUE (rule_id, starts_at),
    CHECK (ends_at > starts_at),
    FOREIGN KEY (rule_id, tutor_id) REFERENCES availability_rules (id, tutor_id) ON DELETE SET NULL (rule_id),
    EXCLUDE USING gist (tutor_id WITH =, tstzrange(starts_at, ends_at, '[)') WITH &&),
    CHECK (isfinite(starts_at) AND isfinite(ends_at))
);

CREATE INDEX availability_slots_tutor_id_starts_at_idx ON availability_slots (tutor_id, starts_at);

-- Replace an unbooked occurrence to change its time or mode. The session FK
-- prevents replacing an occurrence that still backs an active booking.
CREATE FUNCTION guard_availability_slot_schedule() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.tutor_id, NEW.starts_at, NEW.ends_at, NEW.allowed_mode)
       IS DISTINCT FROM ROW(OLD.tutor_id, OLD.starts_at, OLD.ends_at, OLD.allowed_mode) THEN
        RAISE check_violation USING MESSAGE = 'replace the availability slot to change its schedule';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER availability_slots_guard_schedule
BEFORE UPDATE ON availability_slots
FOR EACH ROW EXECUTE FUNCTION guard_availability_slot_schedule();

COMMIT;
