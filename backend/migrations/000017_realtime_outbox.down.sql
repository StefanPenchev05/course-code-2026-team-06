BEGIN;
LOCK TABLE realtime_events IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM realtime_events WHERE dispatched_at IS NULL) THEN
        RAISE EXCEPTION 'drain pending realtime events before rollback';
    END IF;
END $$;
DROP TRIGGER realtime_reservation_change ON session_participant_blocks;
DROP TRIGGER realtime_exception_change ON availability_exceptions;
DROP TRIGGER realtime_slot_change ON availability_slots;
DROP TRIGGER realtime_rule_change ON availability_rules;
DROP TRIGGER realtime_session_change ON tutoring_sessions;
DROP FUNCTION realtime_reservation_change();
DROP FUNCTION realtime_exception_change();
DROP FUNCTION realtime_availability_change();
DROP FUNCTION realtime_session_change();
DROP FUNCTION enqueue_realtime_event(text,uuid);
DROP TABLE realtime_events;
DROP FUNCTION guard_realtime_payload();
COMMIT;
