BEGIN;

DROP INDEX notifications_pending_delivery_idx;

ALTER TABLE notifications
    DROP CONSTRAINT notifications_delivery_lease_check,
    DROP COLUMN attempt_count,
    DROP COLUMN next_attempt_at,
    DROP COLUMN lease_until,
    DROP COLUMN claim_token,
    DROP COLUMN last_error;

COMMIT;
