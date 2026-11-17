BEGIN;

ALTER TABLE notifications
    ADD COLUMN attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN lease_until timestamptz,
    ADD COLUMN claim_token uuid,
    ADD COLUMN last_error text,
    ADD CONSTRAINT notifications_delivery_lease_check CHECK (
        (lease_until IS NULL) = (claim_token IS NULL)
        AND (claim_token IS NULL OR email_status = 'PENDING')
    );

UPDATE notifications SET next_attempt_at = created_at;

CREATE INDEX notifications_pending_delivery_idx
    ON notifications (next_attempt_at, id) WHERE email_status = 'PENDING';

COMMIT;
