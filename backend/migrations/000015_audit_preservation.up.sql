BEGIN;

-- NULL on pre-15 rows means unknown, never an inferred historical attribution.
ALTER TABLE user_violations
    ADD COLUMN actor_kind actor_kind,
    ADD COLUMN recorded_by_user_id uuid REFERENCES users(id),
    ADD COLUMN home_faculty_id uuid REFERENCES faculties(id),
    ADD COLUMN cancellation_history_id uuid REFERENCES session_status_history(id),
    ADD CONSTRAINT violation_audit_metadata CHECK (
        actor_kind IS NOT NULL AND home_faculty_id IS NOT NULL
        AND ((kind='NO_SHOW' AND actor_kind='USER' AND recorded_by_user_id IS NOT NULL
              AND recorded_by_user_id=confirmed_by_coordinator_id AND cancellation_history_id IS NULL)
          OR (kind='LATE_CANCELLATION' AND actor_kind='SYSTEM' AND recorded_by_user_id IS NULL
              AND cancellation_history_id IS NOT NULL))
    ) NOT VALID,
    ADD CONSTRAINT violation_evidence_key UNIQUE(id,user_id,semester_id);

ALTER TABLE user_semester_flags
    ADD COLUMN actor_kind actor_kind,
    ADD COLUMN first_violation_id uuid,
    ADD COLUMN second_violation_id uuid,
    ADD CONSTRAINT flag_first_evidence FOREIGN KEY(first_violation_id,user_id,semester_id)
        REFERENCES user_violations(id,user_id,semester_id),
    ADD CONSTRAINT flag_second_evidence FOREIGN KEY(second_violation_id,user_id,semester_id)
        REFERENCES user_violations(id,user_id,semester_id),
    ADD CONSTRAINT flag_audit_metadata CHECK (
        actor_kind IS NOT NULL AND actor_kind='SYSTEM'
        AND first_violation_id IS NOT NULL AND second_violation_id IS NOT NULL
        AND first_violation_id<>second_violation_id
    ) NOT VALID;

-- Approval is already audited by the application and its immutable qualification.
-- Only automated eligibility withdrawal lacked durable evidence after reapproval.
CREATE TABLE tutor_offering_deactivations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    offering_id uuid NOT NULL REFERENCES tutor_offerings(id),
    qualification_id uuid NOT NULL REFERENCES tutor_qualifications(id),
    faculty_id uuid NOT NULL REFERENCES faculties(id),
    program_id uuid NOT NULL REFERENCES programs(id),
    actor_kind actor_kind NOT NULL CHECK(actor_kind='SYSTEM'),
    changed_at timestamptz NOT NULL,
    reason text NOT NULL CHECK(btrim(reason)<>'')
);
CREATE INDEX tutor_offering_deactivations_offering_idx ON tutor_offering_deactivations(offering_id,changed_at,id);
CREATE INDEX user_suspension_history_faculty_user_time_idx ON user_suspension_history(faculty_id,user_id,changed_at,id);
CREATE INDEX user_violations_faculty_user_time_idx ON user_violations(home_faculty_id,user_id,recorded_at,id);
CREATE INDEX user_semester_flags_faculty_user_time_idx ON user_semester_flags(faculty_id,user_id,flagged_at,id);

CREATE FUNCTION guard_audit_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE check_violation USING MESSAGE='audit evidence is append-only';
END;
$$;
DO $$
DECLARE tab text;
BEGIN
    FOREACH tab IN ARRAY ARRAY['session_status_history','application_status_history','user_violations',
        'user_semester_flags','tutor_qualifications','tutor_offering_deactivations'] LOOP
        EXECUTE format('CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_audit_evidence()',tab);
        EXECUTE format('CREATE TRIGGER audit_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION guard_audit_evidence()',tab);
    END LOOP;
END;
$$;

-- A submission plus its single final decision is itself durable audit evidence.
-- Preserve the original snapshot; only the pending -> terminal decision may change.
CREATE FUNCTION guard_audit_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE mutable text[];
BEGIN
    IF TG_OP<>'UPDATE' THEN
        RAISE check_violation USING MESSAGE='decision evidence cannot be deleted';
    END IF;
    IF TG_TABLE_NAME='issue_reports' THEN
        mutable := ARRAY['status','resolved_at','handled_by_coordinator_id','resolution_note'];
        IF OLD.status::text<>'OPEN' OR NEW.status::text<>'RESOLVED' THEN
            RAISE check_violation USING MESSAGE='issue decision is final';
        END IF;
    ELSE
        mutable := ARRAY['status','reviewed_at','reviewed_by_coordinator_id','rejection_reason'];
        IF TG_TABLE_NAME='attendance_reports' THEN
            mutable := ARRAY['status','reviewed_at','reviewed_by_coordinator_id','resolution_note'];
        END IF;
        IF OLD.status::text<>'PENDING' OR NEW.status::text NOT IN ('APPROVED','REJECTED','CONFIRMED') THEN
            RAISE check_violation USING MESSAGE='review decision is final';
        END IF;
    END IF;
    IF (to_jsonb(OLD)-mutable) IS DISTINCT FROM (to_jsonb(NEW)-mutable) THEN
        RAISE check_violation USING MESSAGE='original submission evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$;
DO $$
DECLARE tab text;
BEGIN
    FOREACH tab IN ARRAY ARRAY['student_verifications','tutor_applications','attendance_reports','issue_reports'] LOOP
        EXECUTE format('CREATE TRIGGER audit_decision BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION guard_audit_decision()',tab);
        EXECUTE format('CREATE TRIGGER audit_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION guard_audit_evidence()',tab);
    END LOOP;
END;
$$;

CREATE FUNCTION check_violation_cause() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.kind='LATE_CANCELLATION' AND NEW.cancellation_history_id IS NULL THEN
        SELECT h.id INTO NEW.cancellation_history_id FROM session_status_history h
        WHERE h.session_id=NEW.session_id AND h.to_status='CANCELLED'
          AND h.cancellation_origin='PARTICIPANT' AND h.actor_kind='USER'
          AND h.changed_by_user_id=NEW.user_id AND h.changed_at=NEW.occurred_at
        ORDER BY h.id LIMIT 1;
    END IF;
    IF NEW.kind='LATE_CANCELLATION' AND NOT EXISTS (
        SELECT 1 FROM session_status_history h JOIN tutoring_sessions s ON s.id=h.session_id
        WHERE h.id=NEW.cancellation_history_id AND h.session_id=NEW.session_id
          AND h.to_status='CANCELLED' AND h.cancellation_origin='PARTICIPANT'
          AND h.actor_kind='USER' AND h.changed_by_user_id=NEW.user_id
          AND h.changed_at=NEW.occurred_at AND NEW.recorded_at=h.changed_at
          AND s.is_late_cancellation AND s.cancelled_by_user_id=NEW.user_id
    ) THEN
        RAISE check_violation USING MESSAGE='violation requires authentic participant cancellation history';
    END IF;
    IF NEW.kind='NO_SHOW' AND NOT EXISTS (
        SELECT 1 FROM attendance_reports a JOIN tutoring_sessions s ON s.id=a.session_id
        WHERE a.id=NEW.attendance_report_id AND a.status='CONFIRMED'
          AND a.absent_user_id=NEW.user_id AND a.session_id=NEW.session_id
          AND a.reviewed_by_coordinator_id=NEW.recorded_by_user_id
          AND a.reviewed_at=NEW.recorded_at AND s.ends_at=NEW.occurred_at
    ) THEN
        RAISE check_violation USING MESSAGE='violation requires authentic attendance decision';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER violation_cause BEFORE INSERT ON user_violations FOR EACH ROW EXECUTE FUNCTION check_violation_cause();

COMMIT;
