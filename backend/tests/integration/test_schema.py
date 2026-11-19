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
    sponsors sponsor_reports
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
        run(*migrate, "up")
        check_tables(psql, EXPECTED_TABLES)
        run(*psql, "-q", "-f", str(TESTS / "schema.sql"), stdout=subprocess.DEVNULL)
        check_concurrency(psql)
        run(*psql, "-q", "-c", "DROP SCHEMA tutti_test CASCADE", stderr=subprocess.DEVNULL)
        run(*migrate, "down", "-all")
        check_tables(psql, set())
        run(*migrate, "up")
        check_tables(psql, EXPECTED_TABLES)
        print("PASS: all 31 tables, integrity checks, concurrent exclusion, complete rollback and reapply.")
    except Exception:
        run("docker", "logs", "--tail", "20", container)
        raise
    finally:
        run("docker", "rm", "-fv", container, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
