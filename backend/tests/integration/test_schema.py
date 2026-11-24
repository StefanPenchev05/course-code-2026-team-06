#!/usr/bin/env python3
"""Run Tutti's migration and integrity tests in a fresh PostgreSQL container."""

import os
from pathlib import Path
import select
import subprocess
import time

TESTS = Path(__file__).resolve().parent
MIGRATIONS = TESTS.parents[1] / "migrations"
EXPECTED_TABLES = set("""
    faculties programs subjects program_subjects users students coordinators
    account_activation_tokens student_verifications tutor_profiles tutor_applications
    tutor_qualifications tutor_offerings session_policies availability_rules
    availability_exceptions availability_slots tutoring_sessions session_participant_blocks
    feedback issue_reports semesters attendance_reports user_violations user_semester_flags
    notifications user_suspension_history session_status_history application_status_history
    sponsors sponsor_reports issue_status_history
""".split())
TABLE_QUERY = """
    SELECT tablename FROM pg_tables
    WHERE schemaname = 'public' AND tablename <> 'schema_migrations'
"""


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, timeout=60, **kwargs)


def output(*args):
    return run(*args, stdout=subprocess.PIPE).stdout.strip()


def wait_until(check, message, timeout=15):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(0.1)
    raise RuntimeError(message)


def check_tables(psql, expected):
    actual = set(output(*psql, "-Atc", TABLE_QUERY).splitlines())
    if actual != expected:
        raise RuntimeError(f"missing tables: {expected - actual}; unexpected tables: {actual - expected}")


def check_concurrency(psql):
    # These bookings share user 10 across opposite roles, and use different
    # availability slots. The second connection must block on the exclusion
    # conflict while the first transaction is still uncommitted.
    first_booking = "SELECT tutti_test.book(10, 11, '2030-02-01 14:00 UTC', 30::smallint, 'INTRODUCTION');"
    second_booking = "SELECT tutti_test.book(12, 10, '2030-02-01 14:00 UTC', 30::smallint, 'INTRODUCTION');"
    env = dict(os.environ, PGAPPNAME="tutti_integration_contender")
    with subprocess.Popen(
        [*psql, "-qAt"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True
    ) as first:
        try:
            first.stdin.write("BEGIN;\n" + first_booking + "\n")
            first.stdin.flush()
            if not select.select([first.stdout], [], [], 10)[0] or not first.stdout.readline().strip():
                raise RuntimeError("first connection failed to reserve the interval")
            with subprocess.Popen(
                [*psql, "-v", "VERBOSITY=verbose", "-c", second_booking],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env
            ) as second:
                try:
                    wait_until(lambda: output(*psql, "-Atc", """
                        SELECT EXISTS (
                            SELECT 1 FROM pg_stat_activity
                            WHERE application_name = 'tutti_integration_contender'
                            AND cardinality(pg_blocking_pids(pid)) > 0
                        )
                    """) == "t", "contending booking never waited for the first transaction", timeout=8)
                    first.communicate("COMMIT;\n", timeout=10)
                    if first.returncode != 0:
                        raise RuntimeError("first booking failed to commit")
                    _, error = second.communicate(timeout=10)
                    if second.returncode == 0 or "23P01" not in error:
                        raise RuntimeError(f"expected an exclusion violation, got: {error}")
                finally:
                    if second.poll() is None:
                        second.kill()
        finally:
            if first.poll() is None:
                first.kill()
    counts = output(*psql, "-Atc", """
        SELECT
            (SELECT count(*) FROM tutoring_sessions WHERE starts_at = '2030-02-01 14:00 UTC'),
            (SELECT count(*) FROM session_participant_blocks WHERE starts_at = '2030-02-01 14:00 UTC')
    """)
    if counts != "1|2":
        raise RuntimeError(f"expected one committed booking with two blocks, got {counts}")


def check_policy_cancellation_migration(psql, migrate):
    # Only this disposable schema fixture is used. Legacy rows have unknown
    # origin, including oversized old reasons that must remain untouched.
    legacy_id = output(*psql, "-qAtc", """
        INSERT INTO session_status_history
            (session_id,from_status,to_status,actor_kind,changed_by_user_id,reason)
        SELECT id,'REQUESTED','CANCELLED','USER',cancelled_by_user_id,repeat('x',2001)
        FROM tutoring_sessions WHERE status='CANCELLED' ORDER BY id LIMIT 1
        RETURNING id;
    """)
    if not legacy_id:
        raise RuntimeError("missing legacy cancellation fixture")
    legacy_query = f"SELECT to_jsonb(h)::text FROM session_status_history h WHERE id='{legacy_id}'"
    before = output(*psql, "-Atc", legacy_query)
    insert = """
        INSERT INTO session_status_history
            (session_id,from_status,to_status,actor_kind,changed_by_user_id,reason,cancellation_origin)
        SELECT id,'REQUESTED','CANCELLED','USER',cancelled_by_user_id,'Policy fixture','COORDINATOR_POLICY'
        FROM tutoring_sessions WHERE status='CANCELLED' ORDER BY id LIMIT 1;
    """
    for old, new in [("'COORDINATOR_POLICY'", "'CLIENT_SELECTED'"),
                     ("'Policy fixture'", "NULL"), ("'Policy fixture'", "'  '"),
                     ("'Policy fixture'", "repeat('x',2001)"),
                     ("'REQUESTED'", "'COMPLETED'"), ("'REQUESTED'", "NULL")]:
        statement = insert.replace(old, new)
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('23514',$test${statement}$test$)",
            stdout=subprocess.DEVNULL)
    for origin in ["PARTICIPANT", "COORDINATOR_POLICY"]:
        run(*psql, "-q", "-c", "BEGIN;" + insert.replace("COORDINATOR_POLICY", origin) + "ROLLBACK;")
    # Execute the actual down script with a synthetic marked event in the same
    # transaction. Its rejection rolls the entire fixture back on disconnect;
    # no history is deleted and migrate's version metadata stays clean.
    down = (MIGRATIONS / "000013_policy_cancellation.down.sql").read_text()
    rejected = subprocess.run([*psql, "-q", "-c", "BEGIN;" + insert + down],
                              text=True, capture_output=True, timeout=30)
    if rejected.returncode == 0 or "cannot remove cancellation origin" not in rejected.stderr:
        raise RuntimeError("migration 13 failed to protect recorded cancellation origins")
    if output(*psql, "-Atc", legacy_query) != before:
        raise RuntimeError("guarded rollback changed legacy cancellation history")
    run(*migrate, "down", "1")
    run(*migrate, "up", "1")  # Reapply only 13 before testing the next upgrade.
    if output(*psql, "-Atc", legacy_query) != before:
        raise RuntimeError("migration 13 rollback/reapply changed legacy cancellation history")


def check_suspension_migration(psql, migrate):
    run(*psql, "-q", "-c", """
        WITH account AS (
            INSERT INTO users(name,email,is_suspended,suspended_at,suspended_by_coordinator_id,suspension_reason)
            SELECT 'Legacy suspended user','legacy-suspension@example.test',true,now(),c.user_id,repeat('s',2001)
            FROM coordinators c WHERE c.employee_number='LEGACY-REVIEWER' RETURNING id
        ) INSERT INTO user_suspension_history(user_id,faculty_id,changed_by_coordinator_id,was_suspended,is_suspended,reason)
          SELECT account.id,c.faculty_id,c.user_id,false,true,repeat('s',2001)
          FROM account,coordinators c WHERE c.employee_number='LEGACY-REVIEWER';
    """)
    legacy = """
        SELECT jsonb_build_array(to_jsonb(u),to_jsonb(h))::text FROM users u
        JOIN user_suspension_history h ON h.user_id=u.id WHERE u.email='legacy-suspension@example.test'
    """
    before = output(*psql, "-Atc", legacy)
    run(*migrate, "up", "1")
    if output(*psql, "-Atc", legacy) != before:
        raise RuntimeError("migration 14 changed legacy suspension evidence")
    insert = """
        INSERT INTO user_suspension_history(user_id,faculty_id,changed_by_coordinator_id,was_suspended,is_suspended,reason)
        SELECT user_id,faculty_id,changed_by_coordinator_id,true,false,'Reinstatement fixture'
        FROM user_suspension_history ORDER BY id LIMIT 1;
    """
    for reason in ["' '", "U&'\\2003'", "repeat('x',2001)"]:
        statement = insert.replace("'Reinstatement fixture'", reason)
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('23514',$test${statement}$test$)", stdout=subprocess.DEVNULL)
    for statement in ["UPDATE user_suspension_history SET reason='tampered'",
                      "DELETE FROM user_suspension_history", "TRUNCATE user_suspension_history, session_status_history",
                      "UPDATE users SET suspension_reason=U&'\\2003' WHERE email='legacy-suspension@example.test'"]:
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('23514',$test${statement}$test$)", stdout=subprocess.DEVNULL)
    session_insert = """
        INSERT INTO session_status_history(session_id,from_status,to_status,actor_kind,changed_by_user_id,reason,cancellation_origin,suspension_history_id)
        SELECT s.id,'REQUESTED','CANCELLED','USER',s.cancelled_by_user_id,'Account suspension','SUSPENSION',h.id
        FROM tutoring_sessions s CROSS JOIN user_suspension_history h WHERE s.status='CANCELLED' ORDER BY s.id,h.id LIMIT 1;
    """
    for statement in [session_insert.replace("'SUSPENSION',h.id", "'SUSPENSION',NULL"),
                      session_insert.replace("'SUSPENSION'", "'PARTICIPANT'")]:
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('23514',$test${statement}$test$)", stdout=subprocess.DEVNULL)
    down = (MIGRATIONS / "000014_suspension_administration.down.sql").read_text()
    notice = """
        INSERT INTO notifications(recipient_user_id,type,subject,body,deduplication_key)
        SELECT id,'USER_SUSPENDED','Account notice','Generic notice','schema-suspension-guard'
        FROM users WHERE email='legacy-suspension@example.test';
    """
    for marker in [notice, session_insert]:
        rejected = subprocess.run([*psql, "-q", "-c", "BEGIN;" + marker + down], text=True, capture_output=True, timeout=30)
        if rejected.returncode == 0 or "cannot roll back suspension administration" not in rejected.stderr:
            raise RuntimeError("migration 14 failed to preserve decision links/notifications")
    run(*migrate, "down", "1")
    if output(*psql, "-Atc", legacy) != before:
        raise RuntimeError("migration 14 rollback changed legacy evidence")
    run(*migrate, "up", "1")
    if output(*psql, "-Atc", legacy) != before:
        raise RuntimeError("migration 14 reapplication changed legacy evidence")


def check_audit_migration(psql, migrate):
    # Existing evidence is captured at version 14, without invented metadata.
    legacy = """SELECT jsonb_build_object(
        'violations',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM user_violations v),
        'applications',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM application_status_history a),
        'attendance',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM attendance_reports a),
        'suspensions',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM user_suspension_history h))::text"""
    before = output(*psql, "-Atc", legacy)
    run(*migrate, "up", "1")
    upgraded = legacy.replace("to_jsonb(v)", "(to_jsonb(v)-ARRAY['actor_kind','recorded_by_user_id','home_faculty_id','cancellation_history_id'])")
    if output(*psql, "-Atc", upgraded) != before:
        raise RuntimeError("audit migration changed original evidence")
    if output(*psql, "-Atc", "SELECT count(*) FROM user_violations WHERE actor_kind IS NOT NULL OR home_faculty_id IS NOT NULL OR recorded_by_user_id IS NOT NULL OR cancellation_history_id IS NOT NULL") != "0":
        raise RuntimeError("audit migration invented legacy attribution")
    for statement in ["UPDATE attendance_reports SET resolution_note='tampered'",
                      "UPDATE student_verifications SET faculty_id=faculty_id",
                      "DELETE FROM user_violations", "TRUNCATE application_status_history"]:
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('23514',$test${statement}$test$)", stdout=subprocess.DEVNULL)
    down = (MIGRATIONS / "000015_audit_preservation.down.sql").read_text()
    evidence = """INSERT INTO tutor_offering_deactivations(offering_id,qualification_id,faculty_id,program_id,actor_kind,changed_at,reason)
        SELECT o.id,o.qualification_id,p.faculty_id,p.id,'SYSTEM',now(),'Rollback guard fixture'
        FROM tutor_offerings o JOIN students s ON s.user_id=o.tutor_id JOIN programs p ON p.id=s.program_id LIMIT 1;"""
    rejected = subprocess.run([*psql,"-q","-c","BEGIN;"+evidence+down], text=True,capture_output=True,timeout=30)
    if rejected.returncode == 0 or "rollback would remove audit evidence" not in rejected.stderr:
        raise RuntimeError("audit rollback failed to preserve new evidence")
    run(*migrate, "down", "1")
    if output(*psql,"-Atc",legacy) != before:
        raise RuntimeError("audit rollback changed legacy evidence")
    run(*migrate, "up", "1")
    if output(*psql,"-Atc",upgraded) != before:
        raise RuntimeError("audit reapply changed legacy evidence")


def check_oidc_binding_migration(psql, migrate):
    # Dedicated synthetic owners make the new FK's RESTRICT behavior observable.
    run(*psql, "-q", "-c", """
        INSERT INTO users(id,name,email) VALUES
        ('00000000-0000-0000-0000-00000000fa01','OIDC schema A','oidc-schema-a@example.test'),
        ('00000000-0000-0000-0000-00000000fa02','OIDC schema B','oidc-schema-b@example.test');
    """)
    existing = EXPECTED_TABLES | {"tutor_offering_deactivations"}
    def snapshot():
        return {table: output(*psql, "-Atc", f"SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]')::text) FROM {table} t")
                for table in sorted(existing)}
    before = snapshot()
    run(*migrate, "up", "1")
    check_tables(psql, existing | {"oidc_identities"})
    if snapshot() != before:
        raise RuntimeError("OIDC migration changed existing accounts/history")
    insert = """INSERT INTO oidc_identities(user_id,issuer,subject) VALUES
        ('00000000-0000-0000-0000-00000000fa01','http://localhost:8180/realms/tutti-demo','schema-subject')"""
    run(*psql, "-q", "-c", insert)
    invalid = [
        ("23505", insert),
        ("23505", insert.replace("fa01", "fa02")),
        ("23505", insert.replace("schema-subject", "other-subject")),
        ("23503", insert.replace("fa01", "fa03").replace("schema-subject", "unknown-owner")),
        ("23503", "DELETE FROM users WHERE id='00000000-0000-0000-0000-00000000fa01'"),
        ("23514", insert.replace("fa01", "fa02").replace("schema-subject", "")),
        ("23514", insert.replace("fa01", "fa02").replace("schema-subject", " ")),
        ("23514", insert.replace("fa01", "fa02").replace("http://localhost:8180/realms/tutti-demo", "not-a-url")),
    ]
    for state, statement in invalid:
        run(*psql, "-q", "-c", f"SELECT tutti_test.expect_state('{state}',$test${statement}$test$)", stdout=subprocess.DEVNULL)
    # Exact issuer casing identifies a different identity; no email/name matching.
    run(*psql, "-q", "-c", insert.replace("fa01", "fa02").replace("tutti-demo", "Tutti-demo"))
    if output(*psql, "-Atc", "SELECT count(*) FROM oidc_identities") != "2":
        raise RuntimeError("OIDC pair equality is not exact")
    down = (MIGRATIONS / "000016_oidc_identities.down.sql").read_text()
    rejected = subprocess.run([*psql, "-q", "-c", down], text=True, capture_output=True, timeout=30)
    if rejected.returncode == 0 or "while mappings exist" not in rejected.stderr:
        raise RuntimeError("OIDC rollback failed to preserve active mappings")
    # Only these fixture mappings exist in this disposable database.
    run(*psql, "-q", "-c", "DELETE FROM oidc_identities")
    run(*migrate, "down", "1")
    check_tables(psql, existing)
    if snapshot() != before:
        raise RuntimeError("OIDC rollback changed existing accounts/history")
    run(*migrate, "up", "1")
    if snapshot() != before:
        raise RuntimeError("OIDC reapply changed existing accounts/history")


def check_realtime_migration(psql, migrate):
    existing = EXPECTED_TABLES | {"tutor_offering_deactivations", "oidc_identities"}
    def snapshot():
        return {table: output(*psql, "-Atc", f"SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]')::text) FROM {table} t")
                for table in sorted(existing)}
    before = snapshot()
    run(*migrate, "up", "1")
    check_tables(psql, existing | {"realtime_events"})
    if snapshot() != before or output(*psql, "-Atc", "SELECT count(*) FROM realtime_events") != "0":
        raise RuntimeError("realtime migration changed/backfilled existing records")
    run(*psql, "-q", "-c", """
        BEGIN;
        INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode)
          VALUES(tutti_test.fixture_id(10),'2099-01-01 09:00Z','2099-01-01 10:00Z','ONLINE');
        INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode)
          VALUES(tutti_test.fixture_id(10),'2099-01-02 09:00Z','2099-01-02 10:00Z','ONLINE');
        DO $$ BEGIN IF (SELECT count(*) FROM realtime_events) <> 1 THEN
          RAISE EXCEPTION 'same-transaction invalidations did not coalesce'; END IF; END $$;
        ROLLBACK;
    """)
    if snapshot() != before or output(*psql, "-Atc", "SELECT count(*) FROM realtime_events") != "0":
        raise RuntimeError("rolled-back availability leaked a realtime event")
    run(*psql, "-q", "-c", "SELECT enqueue_realtime_event('booking.changed',tutti_test.fixture_id(10))")
    run(*psql, "-q", "-c", "SELECT tutti_test.expect_state('23514',$test$UPDATE realtime_events SET entity_id=tutti_test.fixture_id(11)$test$)")
    down = (MIGRATIONS / "000017_realtime_outbox.down.sql").read_text()
    rejected = subprocess.run([*psql, "-q", "-c", down], text=True, capture_output=True, timeout=30)
    if rejected.returncode == 0 or "drain pending realtime events" not in rejected.stderr:
        raise RuntimeError("realtime rollback did not protect pending events")
    # Synthetic fixture outbox only, inside this runner's disposable container.
    run(*psql, "-q", "-c", "DELETE FROM realtime_events")
    run(*migrate, "down", "1")
    check_tables(psql, existing)
    if snapshot() != before:
        raise RuntimeError("realtime rollback changed business data")
    run(*migrate, "up", "1")


def check_domain_realtime_migration(psql, migrate):
    existing = EXPECTED_TABLES | {"tutor_offering_deactivations", "oidc_identities"}
    def snapshot():
        return {table: output(*psql, "-Atc", f"SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]')::text) FROM {table} t")
                for table in sorted(existing)}
    before = snapshot()
    run(*migrate, "up", "1")
    if snapshot() != before or output(*psql, "-Atc", "SELECT count(*) FROM realtime_events") != "0":
        raise RuntimeError("domain event migration modified/backfilled business records")
    run(*psql, "-q", "-c", "SELECT enqueue_realtime_event('account.access_changed',tutti_test.fixture_id(10))")
    down = (MIGRATIONS / "000018_realtime_domain_events.down.sql").read_text()
    rejected = subprocess.run([*psql, "-q", "-c", down], text=True, capture_output=True, timeout=30)
    if rejected.returncode == 0 or "drain domain realtime events" not in rejected.stderr:
        raise RuntimeError("domain rollback did not protect pending work")
    run(*psql, "-q", "-c", "UPDATE realtime_events SET dispatched_at=now()")
    run(*migrate, "down", "1")
    if snapshot() != before:
        raise RuntimeError("domain rollback changed business/audit/email records")
    run(*migrate, "up", "1")
    if snapshot() != before:
        raise RuntimeError("domain reapply changed business records")


def main():
    network = os.environ.get("TUTTI_TEST_DOCKER_NETWORK")
    network_args = ["--network", network, "--network-alias", "schema-postgres"] if network else ["-p", "127.0.0.1::5432"]
    container = output(
        "docker", "create", "--tmpfs", "/var/lib/postgresql/data:rw,size=256m",
        *network_args, "-e", "POSTGRES_PASSWORD=tutti_test",
        "-e", "POSTGRES_DB=tutti_test", "postgres:17",
    )
    try:
        run("docker", "start", container, stdout=subprocess.DEVNULL)
        address = "schema-postgres:5432" if network else output("docker", "port", container, "5432/tcp")
        url = f"postgres://postgres:tutti_test@{address}/tutti_test?sslmode=disable"
        psql = ["psql", "-X", "-v", "ON_ERROR_STOP=1", url]
        wait_until(
            lambda: subprocess.run(
                ["pg_isready", "-d", url, "-t", "1"],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=3,
            ).returncode == 0,
            "disposable PostgreSQL did not start",
        )
        migrate = ["migrate", "-path", str(MIGRATIONS), "-database", url]
        run(*migrate, "up", "10")
        # Migration 11 must preserve pre-existing descriptions without inventing
        # audit events for historical writes whose sequence is unknown.
        run(*psql, "-q", "-c", """
            WITH faculty AS (INSERT INTO faculties(name) VALUES('Legacy issue faculty') RETURNING id),
                 reporter AS (INSERT INTO users(name,email) VALUES('Legacy reporter','legacy-issue@example.test') RETURNING id)
            INSERT INTO issue_reports(reporter_user_id,faculty_id,description)
            SELECT reporter.id,faculty.id,repeat('x',2001) FROM reporter,faculty;
        """)
        run(*migrate, "up", "1")
        if output(*psql, "-Atc", """
            SELECT (SELECT count(*) FROM issue_reports WHERE description=repeat('x',2001)),
                   (SELECT count(*) FROM issue_status_history)
        """) != "1|0":
            raise RuntimeError("issue upgrade changed legacy data or invented history")
        run(*migrate, "up", "1")  # 12: test its independent rollback before 13.
        run(*psql, "-q", "-c", """
            BEGIN;
            WITH reviewer AS (
                INSERT INTO users(name,email) VALUES('Legacy reviewer','legacy-reviewer@example.test') RETURNING id
            ) INSERT INTO coordinators(user_id,employee_number,faculty_id)
              SELECT reviewer.id,'LEGACY-REVIEWER',f.id FROM reviewer,faculties f WHERE f.name='Legacy issue faculty';
            UPDATE issue_reports r SET status='RESOLVED',resolution_note='Reviewed authentic legacy evidence',
                resolved_at=statement_timestamp(),handled_by_coordinator_id=c.user_id
              FROM coordinators c WHERE c.faculty_id=r.faculty_id AND c.employee_number='LEGACY-REVIEWER';
            INSERT INTO issue_status_history(issue_report_id,from_status,to_status,changed_at,actor_kind,changed_by_user_id)
              SELECT id,'OPEN',status,resolved_at,'USER',handled_by_coordinator_id FROM issue_reports;
            COMMIT;
        """)
        legacy_query = """
            SELECT (SELECT count(*) FROM issue_reports WHERE description=repeat('x',2001) AND status='RESOLVED'),
                   (SELECT count(*) FROM issue_status_history WHERE from_status='OPEN' AND to_status='RESOLVED')
        """
        if output(*psql, "-Atc", legacy_query) != "1|1":
            raise RuntimeError("legacy report could not be resolved without losing evidence")
        # Rolling back only 12 must preserve reports and history, including the
        # legacy text that migration 11 intentionally left NOT VALID.
        run(*migrate, "down", "1")
        if output(*psql, "-Atc", legacy_query) != "1|1":
            raise RuntimeError("resolution rollback changed evidence/history")
        run(*migrate, "up", "2")  # Reapply 12 and apply 13; test 14 separately.
        check_tables(psql, EXPECTED_TABLES)
        run(*psql, "-q", "-f", str(TESTS / "schema.sql"), stdout=subprocess.DEVNULL)
        check_policy_cancellation_migration(psql, migrate)
        check_suspension_migration(psql, migrate)
        check_audit_migration(psql, migrate)
        check_oidc_binding_migration(psql, migrate)
        check_realtime_migration(psql, migrate)
        check_domain_realtime_migration(psql, migrate)
        check_concurrency(psql)
        # Only this disposable runner generated these ephemeral invalidations.
        run(*psql, "-q", "-c", "DELETE FROM realtime_events")
        run(*psql, "-q", "-c", "DROP SCHEMA tutti_test CASCADE", stderr=subprocess.DEVNULL)
        run(*migrate, "down", "-all")
        check_tables(psql, set())
        run(*migrate, "up")
        check_tables(psql, EXPECTED_TABLES | {"tutor_offering_deactivations", "oidc_identities", "realtime_events"})
        print("PASS: all 35 tables, realtime transactional capture/coalescing/rollback/reapply, OIDC binding constraints/preservation/guarded rollback/reapply, legacy issue/suspension/disciplinary evidence, migrations 12/13/14/15 rollback, audit retention guards, concurrent exclusion, complete rollback and reapply.")
    except Exception:
        run("docker", "logs", "--tail", "20", container)
        raise
    finally:
        run("docker", "rm", "-fv", container, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
