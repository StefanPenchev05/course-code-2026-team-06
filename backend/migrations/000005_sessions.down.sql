BEGIN;

DROP TRIGGER tutoring_sessions_sync_blocks ON tutoring_sessions;
DROP TRIGGER tutoring_sessions_validate ON tutoring_sessions;

DROP TABLE session_participant_blocks;

DROP TABLE tutoring_sessions;

DROP FUNCTION guard_session_participant_blocks();
DROP FUNCTION sync_session_participant_blocks();
DROP FUNCTION validate_tutoring_session();

DROP TYPE session_participant_role;

DROP TYPE session_status;

DROP TYPE booking_purpose;

DROP TYPE session_mode;

COMMIT;
