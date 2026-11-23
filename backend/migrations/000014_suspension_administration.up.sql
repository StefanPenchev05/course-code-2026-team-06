BEGIN;

ALTER TYPE notification_type ADD VALUE 'USER_SUSPENDED';
ALTER TYPE notification_type ADD VALUE 'USER_REINSTATED';

-- Preserve legacy text verbatim; enforce the current Unicode/UTF-8 byte bounds
-- on new decisions and suspended account writes. No synthetic history backfill.
ALTER TABLE user_suspension_history ADD CONSTRAINT suspension_history_reason_bounds CHECK (
    octet_length(reason) BETWEEN 1 AND 2000
    AND btrim(reason, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') <> ''
) NOT VALID;
ALTER TABLE users ADD CONSTRAINT users_suspension_reason_bounds CHECK (
    NOT is_suspended OR (
        suspension_reason IS NOT NULL AND octet_length(suspension_reason) BETWEEN 1 AND 2000
        AND btrim(suspension_reason, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') <> ''
    )
) NOT VALID;

CREATE FUNCTION guard_user_suspension_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE check_violation USING MESSAGE = 'suspension history is append-only';
END;
$$;
CREATE TRIGGER user_suspension_history_immutable BEFORE UPDATE OR DELETE ON user_suspension_history
    FOR EACH ROW EXECUTE FUNCTION guard_user_suspension_history();
CREATE TRIGGER user_suspension_history_no_truncate BEFORE TRUNCATE ON user_suspension_history
    FOR EACH STATEMENT EXECUTE FUNCTION guard_user_suspension_history();

ALTER TABLE session_status_history
    ADD COLUMN suspension_history_id uuid REFERENCES user_suspension_history(id),
    DROP CONSTRAINT session_history_cancellation_origin_check,
    ADD CONSTRAINT session_history_cancellation_origin_check CHECK (
        cancellation_origin IS NULL OR (
            cancellation_origin IN ('PARTICIPANT','COORDINATOR_POLICY','SUSPENSION')
            AND from_status IS NOT NULL AND from_status IN ('REQUESTED','ACCEPTED')
            AND to_status='CANCELLED' AND actor_kind='USER'
            AND reason IS NOT NULL AND reason ~ '[^[:space:]]'
            AND octet_length(reason) BETWEEN 1 AND 2000
        )
    ),
    ADD CONSTRAINT session_history_suspension_link CHECK (
        (cancellation_origin IS NOT DISTINCT FROM 'SUSPENSION') = (suspension_history_id IS NOT NULL)
    );

COMMIT;
