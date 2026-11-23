BEGIN;

-- USER alone cannot identify the capacity in which a multi-role actor acted.
-- NULL retains unknown/irrelevant origin on existing events; do not infer history
-- from mutable coordinator assignments or rewrite old reasons.
ALTER TABLE session_status_history ADD COLUMN cancellation_origin text,
    ADD CONSTRAINT session_history_cancellation_origin_check CHECK (
        cancellation_origin IS NULL OR (
            cancellation_origin IN ('PARTICIPANT', 'COORDINATOR_POLICY')
            AND from_status IS NOT NULL AND from_status IN ('REQUESTED', 'ACCEPTED')
            AND to_status = 'CANCELLED' AND actor_kind = 'USER'
            AND reason IS NOT NULL AND reason ~ '[^[:space:]]'
            AND octet_length(reason) BETWEEN 1 AND 2000
        )
    );

COMMIT;
