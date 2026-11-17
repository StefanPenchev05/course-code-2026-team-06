BEGIN;

DROP TABLE application_status_history;

DROP TABLE session_status_history;

DROP TABLE user_suspension_history;

DROP TABLE notifications;

DROP TYPE actor_kind;

DROP TYPE email_status;

DROP TYPE notification_type;

COMMIT;
