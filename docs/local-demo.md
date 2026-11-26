# Local demo accounts without SMTP

For a disposable local PostgreSQL database only, apply all migrations and then
run this from the repository root after exporting your local `DATABASE_URL`:

```sh
DEMO_PASSWORD='TuttiDemo2026!' go -C backend run ./cmd/seed-demo --local-demo
```

The command requires an explicit local confirmation and a loopback database
host. It inserts fictional data atomically and hashes the supplied password
with the existing Argon2id implementation. It never runs automatically.
Do not provision these public demo credentials on a deployed instance.

| Email | Initial access |
| --- | --- |
| `student@example.test` | Approved student |
| `tutor@example.test` | Approved student and tutor |
| `coordinator@example.test` | Coordinator for Tutti Demo Faculty |
| `pending@example.test` | Student awaiting that coordinator's approval |

All four use the supplied `DEMO_PASSWORD`. Log in using the existing password
login page. SMTP and Keycloak are not needed. Start the API with
`PUBLIC_ORIGIN=http://localhost:5173` and
`UNIVERSITY_EMAIL_DOMAINS=example.test` for local frontend use.

The approved student and tutor share **Tutti Demo Computing** and
**Demo Software Engineering**. The tutor has an approved offering and free
slots from 09:00 to 17:00 Europe/Luxembourg for the next seven days. A demo
semester is added only if no existing semester covers that interval.
Booking notice, duration, approval, faculty and ownership checks still apply.
No sessions or feedback are fabricated. Log in as the student to request a
session, then as the tutor to accept it. Use separate browser profiles or
log out between roles. The coordinator can review the pending demo student.

Rerunning the seed leaves existing accounts, passwords, approvals and slots
unchanged. It does not reset scenarios or refresh expired availability; use
the tutor's availability screen to add later slots. Partial or conflicting
demo data causes a rollback rather than overwriting records. The catalog
fixture is not required for this seed. No application authentication checks
are disabled and no email is sent.
