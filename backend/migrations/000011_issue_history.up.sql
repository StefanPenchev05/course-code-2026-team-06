BEGIN;

-- Match Coordination's existing 2000 UTF-8 byte limit and Go's Unicode
-- whitespace handling. Preserve legacy descriptions without rewriting them;
-- NOT VALID still checks every new insert and update.
ALTER TABLE issue_reports ADD CONSTRAINT issue_reports_description_bounds_check CHECK (
    octet_length(description) BETWEEN 1 AND 2000
    AND btrim(description, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000') <> ''
) NOT VALID;

CREATE TABLE issue_status_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_report_id uuid NOT NULL REFERENCES issue_reports (id),
    from_status issue_status,
    to_status issue_status NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now(),
    actor_kind actor_kind NOT NULL,
    changed_by_user_id uuid REFERENCES users (id),
    CHECK ((actor_kind = 'USER' AND changed_by_user_id IS NOT NULL)
        OR (actor_kind = 'SYSTEM' AND changed_by_user_id IS NULL)),
    CHECK (from_status IS DISTINCT FROM to_status),
    CHECK (from_status IS NOT NULL OR to_status = 'OPEN')
);

CREATE INDEX issue_status_history_issue_report_id_changed_at_idx
    ON issue_status_history (issue_report_id, changed_at, id);
CREATE UNIQUE INDEX issue_status_history_one_creation_idx
    ON issue_status_history (issue_report_id) WHERE from_status IS NULL;

-- No synthetic backfill: legacy records do not prove their historical actors
-- or transitions. New application writes create report and history atomically.
CREATE FUNCTION guard_issue_status_history() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE check_violation USING MESSAGE = 'issue history is append-only';
END;
$$;

CREATE TRIGGER issue_status_history_immutable
BEFORE UPDATE OR DELETE ON issue_status_history
FOR EACH ROW EXECUTE FUNCTION guard_issue_status_history();
CREATE TRIGGER issue_status_history_no_truncate
BEFORE TRUNCATE ON issue_status_history
FOR EACH STATEMENT EXECUTE FUNCTION guard_issue_status_history();

COMMIT;
