BEGIN;

-- Stop issue writers and archive history before a deliberate rollback.
DROP TABLE issue_status_history;
DROP FUNCTION guard_issue_status_history();
ALTER TABLE issue_reports DROP CONSTRAINT issue_reports_description_bounds_check;

COMMIT;
