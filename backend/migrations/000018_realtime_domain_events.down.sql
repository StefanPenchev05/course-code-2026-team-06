BEGIN;
-- Stop writers and API dispatchers before rollback. Never drop pending work.
LOCK TABLE realtime_events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM realtime_events WHERE dispatched_at IS NULL
               AND event_type NOT IN ('booking.changed','availability.changed')) THEN
        RAISE EXCEPTION 'drain domain realtime events before rollback';
    END IF;
END $$;

DROP TRIGGER realtime_application_change ON tutor_applications;
DROP TRIGGER realtime_offering_change ON tutor_offerings;
DROP TRIGGER realtime_issue_change ON issue_reports;
DROP TRIGGER realtime_attendance_change ON attendance_reports;
DROP TRIGGER realtime_violation_change ON user_violations;
DROP TRIGGER realtime_flag_change ON user_semester_flags;
DROP TRIGGER realtime_suspension_change ON user_suspension_history;
DROP TRIGGER realtime_policy_change ON session_status_history;
DROP TRIGGER realtime_completion_change ON tutoring_sessions;
DROP TRIGGER realtime_feedback_change ON feedback;
DROP TRIGGER realtime_verification_change ON student_verifications;
DROP TRIGGER realtime_account_change ON users;
DROP TRIGGER realtime_student_change ON students;
DROP TRIGGER realtime_coordinator_change ON coordinators;
DROP TRIGGER realtime_tutor_change ON tutor_profiles;
DROP FUNCTION realtime_application_change(),realtime_offering_change(),realtime_case_change(),
    realtime_policy_change(),realtime_activity_change(),realtime_verification_change(),
    realtime_account_change(),realtime_membership_change();

-- Transient dispatched invalidations only; no business/audit/email data removed.
DELETE FROM realtime_events WHERE event_type NOT IN ('booking.changed','availability.changed');
ALTER TABLE realtime_events DROP CONSTRAINT realtime_events_event_type_check,
    DROP CONSTRAINT realtime_events_resource_kind_check,
    DROP CONSTRAINT realtime_events_transaction_resource_key,
    DROP COLUMN resource_kind,
    ADD CONSTRAINT realtime_events_event_type_check CHECK (event_type IN ('booking.changed','availability.changed')),
    ADD UNIQUE (producer_xid,event_type,entity_id);
CREATE OR REPLACE FUNCTION enqueue_realtime_event(kind text, entity uuid) RETURNS void LANGUAGE sql AS $$
    INSERT INTO realtime_events(event_type,entity_id) VALUES(kind,entity)
    ON CONFLICT(producer_xid,event_type,entity_id) DO NOTHING;
$$;
DROP FUNCTION enqueue_realtime_event(text,uuid,text);
CREATE OR REPLACE FUNCTION guard_realtime_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.producer_xid,NEW.event_type,NEW.entity_id,NEW.occurred_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.producer_xid,OLD.event_type,OLD.entity_id,OLD.occurred_at) THEN
        RAISE check_violation USING MESSAGE='realtime event identity and payload are immutable';
    END IF;
    RETURN NEW;
END;
$$;
COMMIT;
