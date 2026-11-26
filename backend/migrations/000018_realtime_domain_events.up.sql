BEGIN;

-- entity_id is a PRIVATE routing target for account events. The Go adapter
-- projects it out of both claims and public envelopes. Case kinds never go on wire.
ALTER TABLE realtime_events
    DROP CONSTRAINT realtime_events_event_type_check,
    DROP CONSTRAINT realtime_events_producer_xid_event_type_entity_id_key,
    ADD COLUMN resource_kind text NOT NULL DEFAULT '',
    ADD CONSTRAINT realtime_events_event_type_check CHECK (event_type IN (
        'booking.changed','availability.changed','tutor_application.changed',
        'coordination.changed','activity.changed','account.access_changed')),
    ADD CONSTRAINT realtime_events_resource_kind_check CHECK (
        (event_type <> 'coordination.changed' AND resource_kind = '') OR
        (event_type = 'coordination.changed' AND resource_kind IN (
            'issue','attendance','violation','flag','policy','suspension','verification'))),
    ADD CONSTRAINT realtime_events_transaction_resource_key UNIQUE (producer_xid,event_type,resource_kind,entity_id);

CREATE OR REPLACE FUNCTION guard_realtime_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.producer_xid,NEW.event_type,NEW.entity_id,NEW.occurred_at,NEW.resource_kind)
       IS DISTINCT FROM ROW(OLD.id,OLD.producer_xid,OLD.event_type,OLD.entity_id,OLD.occurred_at,OLD.resource_kind) THEN
        RAISE check_violation USING MESSAGE='realtime event identity and payload are immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE FUNCTION enqueue_realtime_event(kind text, entity uuid, resource text) RETURNS void LANGUAGE sql AS $$
    INSERT INTO realtime_events(event_type,entity_id,resource_kind) VALUES(kind,entity,resource)
    ON CONFLICT(producer_xid,event_type,resource_kind,entity_id) DO NOTHING;
$$;
CREATE OR REPLACE FUNCTION enqueue_realtime_event(kind text, entity uuid) RETURNS void LANGUAGE sql AS $$
    SELECT enqueue_realtime_event(kind,entity,'');
$$;

CREATE FUNCTION realtime_application_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM enqueue_realtime_event('tutor_application.changed',NEW.id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_application_change AFTER INSERT OR UPDATE ON tutor_applications
FOR EACH ROW EXECUTE FUNCTION realtime_application_change();

CREATE FUNCTION realtime_offering_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE application uuid;
BEGIN
    IF TG_OP = 'INSERT' OR ROW(NEW.is_active,NEW.qualification_id) IS DISTINCT FROM ROW(OLD.is_active,OLD.qualification_id) THEN
        SELECT application_id INTO application FROM tutor_qualifications WHERE id=NEW.qualification_id;
        PERFORM enqueue_realtime_event('tutor_application.changed',application);
        IF TG_OP = 'UPDATE' AND OLD.qualification_id IS DISTINCT FROM NEW.qualification_id THEN
            SELECT application_id INTO application FROM tutor_qualifications WHERE id=OLD.qualification_id;
            PERFORM enqueue_realtime_event('tutor_application.changed',application);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_offering_change AFTER INSERT OR UPDATE ON tutor_offerings
FOR EACH ROW EXECUTE FUNCTION realtime_offering_change();

CREATE FUNCTION realtime_case_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.status IS NOT DISTINCT FROM OLD.status THEN RETURN NULL; END IF;
    END IF;
    PERFORM enqueue_realtime_event('coordination.changed',NEW.id,TG_ARGV[0]);
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_issue_change AFTER INSERT OR UPDATE ON issue_reports
FOR EACH ROW EXECUTE FUNCTION realtime_case_change('issue');
CREATE TRIGGER realtime_attendance_change AFTER INSERT OR UPDATE ON attendance_reports
FOR EACH ROW EXECUTE FUNCTION realtime_case_change('attendance');
CREATE TRIGGER realtime_violation_change AFTER INSERT ON user_violations
FOR EACH ROW EXECUTE FUNCTION realtime_case_change('violation');
CREATE TRIGGER realtime_flag_change AFTER INSERT ON user_semester_flags
FOR EACH ROW EXECUTE FUNCTION realtime_case_change('flag');
CREATE TRIGGER realtime_suspension_change AFTER INSERT ON user_suspension_history
FOR EACH ROW EXECUTE FUNCTION realtime_case_change('suspension');

CREATE FUNCTION realtime_policy_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cancellation_origin = 'COORDINATOR_POLICY' THEN
        PERFORM enqueue_realtime_event('coordination.changed',NEW.id,'policy');
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_policy_change AFTER INSERT ON session_status_history
FOR EACH ROW EXECUTE FUNCTION realtime_policy_change();

CREATE FUNCTION realtime_activity_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tutor uuid;
BEGIN
    IF TG_TABLE_NAME = 'feedback' THEN
        SELECT tutor_id INTO tutor FROM tutoring_sessions WHERE id=NEW.session_id;
        PERFORM enqueue_realtime_event('activity.changed',tutor);
    ELSIF (NEW.status = 'COMPLETED') IS DISTINCT FROM (OLD.status = 'COMPLETED') THEN
        PERFORM enqueue_realtime_event('activity.changed',NEW.tutor_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_completion_change AFTER UPDATE ON tutoring_sessions
FOR EACH ROW EXECUTE FUNCTION realtime_activity_change();
CREATE TRIGGER realtime_feedback_change AFTER INSERT ON feedback
FOR EACH ROW EXECUTE FUNCTION realtime_activity_change();

CREATE FUNCTION realtime_verification_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM enqueue_realtime_event('account.access_changed',NEW.student_id);
        PERFORM enqueue_realtime_event('coordination.changed',NEW.id,'verification');
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_verification_change AFTER INSERT OR UPDATE ON student_verifications
FOR EACH ROW EXECUTE FUNCTION realtime_verification_change();

CREATE FUNCTION realtime_account_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.is_suspended,NEW.closed_at) IS DISTINCT FROM ROW(OLD.is_suspended,OLD.closed_at) THEN
        PERFORM enqueue_realtime_event('account.access_changed',NEW.id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_account_change AFTER UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION realtime_account_change();

CREATE FUNCTION realtime_membership_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;
BEGIN
    -- Narrow UPDATE triggers below avoid notifications for private profile edits.
    IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
    IF TG_TABLE_NAME = 'tutor_profiles' THEN
        IF TG_OP = 'DELETE' THEN target := OLD.student_id; ELSE target := NEW.student_id; END IF;
    ELSE
        IF TG_OP = 'DELETE' THEN target := OLD.user_id; ELSE target := NEW.user_id; END IF;
    END IF;
    PERFORM enqueue_realtime_event('account.access_changed',target);
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_student_change AFTER INSERT OR DELETE OR UPDATE OF program_id ON students
FOR EACH ROW EXECUTE FUNCTION realtime_membership_change();
CREATE TRIGGER realtime_coordinator_change AFTER INSERT OR DELETE OR UPDATE OF faculty_id ON coordinators
FOR EACH ROW EXECUTE FUNCTION realtime_membership_change();
CREATE TRIGGER realtime_tutor_change AFTER INSERT OR DELETE OR UPDATE OF is_active,approval_ended_at ON tutor_profiles
FOR EACH ROW EXECUTE FUNCTION realtime_membership_change();

COMMIT;
