BEGIN;
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM user_violations WHERE actor_kind IS NOT NULL OR home_faculty_id IS NOT NULL OR recorded_by_user_id IS NOT NULL OR cancellation_history_id IS NOT NULL)
       OR EXISTS(SELECT 1 FROM user_semester_flags WHERE actor_kind IS NOT NULL OR first_violation_id IS NOT NULL OR second_violation_id IS NOT NULL)
       OR EXISTS(SELECT 1 FROM tutor_offering_deactivations) THEN
        RAISE EXCEPTION 'migration 15 rollback would remove audit evidence; retain schema and use a forward repair';
    END IF;
END;
$$;
DROP TRIGGER violation_cause ON user_violations;
DROP FUNCTION check_violation_cause();
DO $$
DECLARE tab text;
BEGIN
    FOREACH tab IN ARRAY ARRAY['session_status_history','application_status_history','user_violations','user_semester_flags','tutor_qualifications','tutor_offering_deactivations'] LOOP
        EXECUTE format('DROP TRIGGER audit_immutable ON %I',tab);
        EXECUTE format('DROP TRIGGER audit_no_truncate ON %I',tab);
    END LOOP;
    FOREACH tab IN ARRAY ARRAY['student_verifications','tutor_applications','attendance_reports','issue_reports'] LOOP
        EXECUTE format('DROP TRIGGER audit_decision ON %I',tab);
        EXECUTE format('DROP TRIGGER audit_no_truncate ON %I',tab);
    END LOOP;
END;
$$;
DROP FUNCTION guard_audit_decision();
DROP FUNCTION guard_audit_evidence();
DROP TABLE tutor_offering_deactivations;
DROP INDEX user_suspension_history_faculty_user_time_idx;
DROP INDEX user_violations_faculty_user_time_idx;
DROP INDEX user_semester_flags_faculty_user_time_idx;
ALTER TABLE user_semester_flags DROP CONSTRAINT flag_first_evidence, DROP CONSTRAINT flag_second_evidence, DROP CONSTRAINT flag_audit_metadata,
    DROP COLUMN actor_kind, DROP COLUMN first_violation_id, DROP COLUMN second_violation_id;
ALTER TABLE user_violations DROP CONSTRAINT violation_audit_metadata, DROP CONSTRAINT violation_evidence_key,
    DROP COLUMN actor_kind, DROP COLUMN recorded_by_user_id, DROP COLUMN home_faculty_id, DROP COLUMN cancellation_history_id;
COMMIT;
