BEGIN;

-- A NOT VALID check still rejects updates to legacy long descriptions. Enforce
-- the same bound on inserts and actual description changes, so resolution can
-- preserve the original evidence without rewriting it.
ALTER TABLE issue_reports DROP CONSTRAINT issue_reports_description_bounds_check;

CREATE FUNCTION validate_issue_description() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.description IS NOT DISTINCT FROM OLD.description THEN
            RETURN NEW;
        END IF;
    END IF;
    IF octet_length(NEW.description) NOT BETWEEN 1 AND 2000
       OR btrim(NEW.description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') = '' THEN
        RAISE check_violation USING MESSAGE = 'invalid issue description';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_reports_description_bounds
BEFORE INSERT OR UPDATE OF description ON issue_reports
FOR EACH ROW EXECUTE FUNCTION validate_issue_description();

-- Keep historical resolution notes, but require bounded, meaningful notes on
-- every new resolution. The existing check also requires resolver/time fields.
ALTER TABLE issue_reports ADD CONSTRAINT issue_reports_resolution_note_bounds_check CHECK (
    status <> 'RESOLVED' OR (
        resolution_note IS NOT NULL
        AND octet_length(resolution_note) BETWEEN 1 AND 2000
        AND btrim(resolution_note, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') <> ''
    )
) NOT VALID;

COMMIT;
