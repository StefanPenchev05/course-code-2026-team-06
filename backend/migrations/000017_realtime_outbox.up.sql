BEGIN;

-- Dedicated invalidation outbox, independent of email delivery and business history.
CREATE TABLE realtime_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid() CHECK(id<>'00000000-0000-0000-0000-000000000000'),
    producer_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
    event_type text NOT NULL CHECK (event_type IN ('booking.changed','availability.changed')),
    entity_id uuid NOT NULL CHECK(entity_id<>'00000000-0000-0000-0000-000000000000'),
    occurred_at timestamptz NOT NULL DEFAULT statement_timestamp() CHECK (isfinite(occurred_at)),
    next_attempt_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    claim_token uuid,
    lease_until timestamptz,
    dispatched_at timestamptz,
    last_error text CHECK (last_error IN ('fanout_failed')),
    UNIQUE (producer_xid, event_type, entity_id),
    CHECK ((claim_token IS NULL) = (lease_until IS NULL)),
    CHECK (dispatched_at IS NULL OR claim_token IS NULL)
);
CREATE INDEX realtime_events_pending_idx ON realtime_events(next_attempt_at,id)
    WHERE dispatched_at IS NULL;

CREATE FUNCTION guard_realtime_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.producer_xid,NEW.event_type,NEW.entity_id,NEW.occurred_at)
       IS DISTINCT FROM ROW(OLD.id,OLD.producer_xid,OLD.event_type,OLD.entity_id,OLD.occurred_at) THEN
        RAISE check_violation USING MESSAGE='realtime event identity and payload are immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER realtime_payload_immutable BEFORE UPDATE ON realtime_events
FOR EACH ROW EXECUTE FUNCTION guard_realtime_payload();

-- No network IO in producer transactions. Multiple slot/reservation writes in
-- the SAME transaction coalesce while retaining the first stable event UUID.
CREATE FUNCTION enqueue_realtime_event(kind text, entity uuid) RETURNS void
LANGUAGE sql AS $$
    INSERT INTO realtime_events(event_type,entity_id) VALUES(kind,entity)
    ON CONFLICT(producer_xid,event_type,entity_id) DO NOTHING;
$$;

CREATE FUNCTION realtime_session_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' OR NEW.status IS DISTINCT FROM OLD.status THEN
        PERFORM enqueue_realtime_event('booking.changed',NEW.id);
        PERFORM enqueue_realtime_event('availability.changed',NEW.tutor_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_session_change AFTER INSERT OR UPDATE ON tutoring_sessions
FOR EACH ROW EXECUTE FUNCTION realtime_session_change();

CREATE FUNCTION realtime_availability_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tutor uuid;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
    IF TG_OP = 'DELETE' THEN tutor := OLD.tutor_id; ELSE tutor := NEW.tutor_id; END IF;
    PERFORM enqueue_realtime_event('availability.changed',tutor);
    IF TG_OP = 'UPDATE' AND OLD.tutor_id IS DISTINCT FROM NEW.tutor_id THEN
        PERFORM enqueue_realtime_event('availability.changed',OLD.tutor_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_rule_change AFTER INSERT OR UPDATE OR DELETE ON availability_rules
FOR EACH ROW EXECUTE FUNCTION realtime_availability_change();
CREATE TRIGGER realtime_slot_change AFTER INSERT OR UPDATE OR DELETE ON availability_slots
FOR EACH ROW EXECUTE FUNCTION realtime_availability_change();

CREATE FUNCTION realtime_exception_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE tutor uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN
        SELECT tutor_id INTO tutor FROM availability_rules WHERE id=OLD.rule_id;
    ELSE
        SELECT tutor_id INTO tutor FROM availability_rules WHERE id=NEW.rule_id;
    END IF;
    -- During parent deletion the rule trigger already captures its tutor.
    IF tutor IS NOT NULL THEN PERFORM enqueue_realtime_event('availability.changed',tutor); END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_exception_change AFTER INSERT OR UPDATE OR DELETE ON availability_exceptions
FOR EACH ROW EXECUTE FUNCTION realtime_exception_change();

CREATE FUNCTION realtime_reservation_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE participant uuid;
BEGIN
    IF TG_OP = 'DELETE' THEN participant := OLD.user_id; ELSE participant := NEW.user_id; END IF;
    -- A student may also teach. Their student-role booking changes the free
    -- projection of THEIR published tutor windows as well as the assigned tutor's.
    IF EXISTS(SELECT 1 FROM tutor_profiles WHERE student_id=participant) THEN
        PERFORM enqueue_realtime_event('availability.changed',participant);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER realtime_reservation_change AFTER INSERT OR DELETE ON session_participant_blocks
FOR EACH ROW EXECUTE FUNCTION realtime_reservation_change();

COMMIT;
