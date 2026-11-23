BEGIN;

-- Restore migration 11's validation behavior without dropping reports/history.
ALTER TABLE issue_reports DROP CONSTRAINT issue_reports_resolution_note_bounds_check;
DROP TRIGGER issue_reports_description_bounds ON issue_reports;
DROP FUNCTION validate_issue_description();
ALTER TABLE issue_reports ADD CONSTRAINT issue_reports_description_bounds_check CHECK (
    octet_length(description) BETWEEN 1 AND 2000
    AND btrim(description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') <> ''
) NOT VALID;

COMMIT;
