BEGIN;

DROP TABLE availability_slots;

DROP FUNCTION guard_availability_slot_schedule();

DROP TABLE availability_exceptions;

DROP TABLE availability_rules;

DROP TABLE session_policies;

DROP TYPE slot_mode;

COMMIT;
