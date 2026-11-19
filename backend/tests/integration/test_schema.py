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
    run(*migrate, "up")
    if output(*psql, "-Atc", legacy_query) != before:
        raise RuntimeError("migration 13 rollback/reapply changed legacy cancellation history")


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
        run(*migrate, "up")
        check_tables(psql, EXPECTED_TABLES)
        run(*psql, "-q", "-f", str(TESTS / "schema.sql"), stdout=subprocess.DEVNULL)
        check_policy_cancellation_migration(psql, migrate)
        check_concurrency(psql)
        run(*psql, "-q", "-c", "DROP SCHEMA tutti_test CASCADE", stderr=subprocess.DEVNULL)
        run(*migrate, "down", "-all")
        check_tables(psql, set())
        run(*migrate, "up")
        check_tables(psql, EXPECTED_TABLES)
        print("PASS: all 32 tables, legacy issue upgrade/resolution, migrations 12/13 rollback, cancellation-origin constraints and retention guard, integrity checks, concurrent exclusion, complete rollback and reapply.")
    except Exception:
        run("docker", "logs", "--tail", "20", container)
        raise
    finally:
        run("docker", "rm", "-fv", container, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
