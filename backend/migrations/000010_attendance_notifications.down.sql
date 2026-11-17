BEGIN;
-- The cast deliberately refuses rollback while attendance emails are retained.
ALTER TYPE notification_type RENAME TO notification_type_with_attendance;
CREATE TYPE notification_type AS ENUM (
    'NEW_REQUEST', 'REQUEST_ACCEPTED', 'REQUEST_DECLINED', 'REQUEST_EXPIRED',
    'SESSION_CANCELLED', 'APPLICATION_APPROVED', 'APPLICATION_REJECTED',
    'STUDENT_VERIFIED', 'STUDENT_REJECTED', 'USER_FLAGGED', 'SESSION_REMINDER'
);
ALTER TABLE notifications ALTER COLUMN type TYPE notification_type USING type::text::notification_type;
DROP TYPE notification_type_with_attendance;
COMMIT;
