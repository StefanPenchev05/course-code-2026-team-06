BEGIN;

ALTER TABLE users DROP CONSTRAINT users_suspending_coordinator_fk;

DROP TABLE student_verifications;

DROP TABLE account_activation_tokens;

DROP TABLE coordinators;

DROP TABLE students;

DROP TABLE users;

DROP TYPE verification_status;

COMMIT;
