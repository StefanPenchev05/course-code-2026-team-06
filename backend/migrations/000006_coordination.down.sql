BEGIN;

DROP TABLE user_semester_flags;

DROP TABLE user_violations;

DROP TABLE attendance_reports;

DROP FUNCTION validate_attendance_report();

DROP TABLE issue_reports;

DROP TABLE feedback;

DROP TYPE violation_type;

DROP TYPE attendance_report_status;

DROP TYPE issue_status;

COMMIT;
