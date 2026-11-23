BEGIN;

-- Refuse to erase recorded actor capacity. Archive/retention is a separate,
-- authorized workflow; never delete history just to force a rollback.
LOCK TABLE session_status_history IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM session_status_history WHERE cancellation_origin IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove cancellation origin while recorded cancellation origins remain';
    END IF;
END;
$$;
ALTER TABLE session_status_history DROP COLUMN cancellation_origin;

COMMIT;
