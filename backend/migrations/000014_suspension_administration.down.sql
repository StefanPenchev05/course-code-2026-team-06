BEGIN;

LOCK TABLE session_status_history, notifications IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM session_status_history WHERE suspension_history_id IS NOT NULL OR cancellation_origin='SUSPENSION')
       OR EXISTS (SELECT 1 FROM notifications WHERE type::text IN ('USER_SUSPENDED','USER_REINSTATED')) THEN
        RAISE EXCEPTION 'cannot roll back suspension administration while decision links or notifications remain';
    END IF;
END;
$$;

ALTER TABLE session_status_history
    DROP CONSTRAINT session_history_suspension_link,
    DROP COLUMN suspension_history_id,
    DROP CONSTRAINT session_history_cancellation_origin_check,
    ADD CONSTRAINT session_history_cancellation_origin_check CHECK (
        cancellation_origin IS NULL OR (
            cancellation_origin IN ('PARTICIPANT','COORDINATOR_POLICY')
            AND from_status IS NOT NULL AND from_status IN ('REQUESTED','ACCEPTED')
            AND to_status='CANCELLED' AND actor_kind='USER'
            AND reason IS NOT NULL AND reason ~ '[^[:space:]]'
            AND octet_length(reason) BETWEEN 1 AND 2000
        )
    );
DROP TRIGGER user_suspension_history_immutable ON user_suspension_history;
DROP TRIGGER user_suspension_history_no_truncate ON user_suspension_history;
DROP FUNCTION guard_user_suspension_history();
ALTER TABLE user_suspension_history DROP CONSTRAINT suspension_history_reason_bounds;
ALTER TABLE users DROP CONSTRAINT users_suspension_reason_bounds;

ALTER TYPE notification_type RENAME TO notification_type_with_suspension;
CREATE TYPE notification_type AS ENUM (
    'NEW_REQUEST','REQUEST_ACCEPTED','REQUEST_DECLINED','REQUEST_EXPIRED','SESSION_CANCELLED',
    'APPLICATION_APPROVED','APPLICATION_REJECTED','STUDENT_VERIFIED','STUDENT_REJECTED',
    'USER_FLAGGED','SESSION_REMINDER','ATTENDANCE_REPORTED','ATTENDANCE_DECIDED'
);
ALTER TABLE notifications ALTER COLUMN type TYPE notification_type USING type::text::notification_type;
DROP TYPE notification_type_with_suspension;

COMMIT;
