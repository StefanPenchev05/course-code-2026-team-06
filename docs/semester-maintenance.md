# Trusted semester calendar maintenance — Issue #50

The operator-only `semester-maintenance` CLI supplies Catalog creation, Scheduling
impact analysis and rollover review for FR3, FR12 and UC08. It registers **no HTTP
endpoints**. An authorized operator needs an approved database maintenance connection;
student/coordinator sessions confer no access. Redis, SMTP and a running worker are
not required.

## Verified inputs and exact instants

Use the University of Luxembourg's [official academic calendar](https://www.uni.lu/en/about/academic-calendar/)
and its current linked publication, or another explicitly authorized institutional
source. Verify the revision and distinguish semester dates, teaching periods and
examinations. This tool contains no production dates, scraper or institutional API.
Test dates are synthetic. The official site's content may require an interactive
browser; supplying its URL is not evidence that its dates were verified.

Scheduling uses exact instants and **`[starts_at, ends_at)`** intervals. A positive
slot/session can start at the start and finish at the end, but cannot start at the end.
Inputs must be RFC3339 timestamps with explicit offsets and an IANA zone. Each offset
must match that zone at that instant. Nonexistent local times fail; an explicitly
offset-qualified repeated autumn time denotes an unambiguous instant. Scheduling's
existing stricter rejection of ambiguous recurring local endpoints remains intact.
Precision is at most microseconds and supported years are 1–9999 in input and UTC.
Equal/reversed bounds, infinity and date-only inputs fail.

If an institutional publication specifies whole dates, record its reviewed meaning
in the approval. For a **confirmed inclusive** end date, use midnight on the **following
local date**, with that date's offset; do not add a fixed 24 hours across DST. Resolve
unclear inclusivity, examination coverage or faculty classifications with the calendar
owner before applying. This implementation does not invent institutional policy.

## Manifest, identity and provenance

Supply one UTF-8 JSON object, at most 16 KiB:

| Field | Required content |
| --- | --- |
| `id` | Nonzero UUID generated once and retained for retries. |
| `name` | Verified semester name, at most 200 characters. |
| `starts_at`, `ends_at` | Verified RFC3339 instants with explicit offsets. |
| `time_zone` | IANA zone matching both offsets, usually `Europe/Luxembourg` for Luxembourg local dates. |
| `source` | Authoritative HTTPS publication URL without credentials, query or fragment. |
| `source_revision` | Publication date/version if available; optional otherwise. |
| `operator` | Responsible operator. |
| `approval` | Approval/change-record reference, reviewed date interpretation and any overlap decision. |

Unknown/duplicate keys, repeated flags, trailing JSON and control characters fail.
Names are trimmed with internal whitespace collapsed; retry matching ignores case.
An equivalent name and exact bounds return `already_exists` with the existing UUID,
even if a second UUID was supplied. The same ID/name with different bounds, or a
different name with identical bounds, conflicts. Multiple equivalent legacy rows
also conflict for manual review. The tool never merges or deletes them. These are
conservative operator guards, not new database-wide naming constraints.

Archive the reviewed manifest, preview and execution receipt in the restricted
operations/change record. Receipts include supplied provenance, canonical semester,
outcome and UTC `action_at` (action start). **Provenance is documented in these files,
not stored in new database columns.** A retry includes its newly supplied manifest,
not proof of the original source/operator. Keep the original successful receipt;
existing rows have unknown provenance unless separately documented. Do not include
credentials, personal academic records or access tokens in manifests or approvals.

## Windows PowerShell workflow

From the repository root, build outside the checkout. These commands do not read or
replace `.env`. Use Go and an explicitly approved PostgreSQL connection.

```powershell
$operationDir = Join-Path $env:TEMP ('tutti-calendar-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $operationDir | Out-Null
$semesterExe = Join-Path $operationDir 'semester-maintenance.exe'
Push-Location backend
go build -trimpath -o $semesterExe ./cmd/semester-maintenance
if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
Pop-Location
& $semesterExe --help

# Secret input is not echoed or placed in shell history. Select the database explicitly.
# Compose normally uses localhost:5433; native Windows PostgreSQL may use 5432.
$env:DATABASE_URL = [System.Net.NetworkCredential]::new('', (Read-Host 'Approved DATABASE_URL' -AsSecureString)).Password
& $semesterExe list
if ($LASTEXITCODE -ne 0) { throw 'Calendar inspection failed' }

$manifestPath = Join-Path $operationDir 'semester.json'
$manifest = [ordered]@{
    id = [guid]::NewGuid().ToString() # generate once; preserve this file for retries
    name = Read-Host 'Verified semester name'
    starts_at = Read-Host 'Verified start RFC3339 with correct offset'
    ends_at = Read-Host 'Verified exclusive end RFC3339 with correct offset'
    time_zone = Read-Host 'Source IANA zone, e.g. Europe/Luxembourg'
    source = Read-Host 'Verified authoritative HTTPS publication URL'
    source_revision = Read-Host 'Publication revision/date, blank only if unavailable'
    operator = Read-Host 'Responsible operator'
    approval = Read-Host 'Approved change reference and date-bound interpretation'
}
[System.IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json), [System.Text.UTF8Encoding]::new($false))
& $semesterExe create --file $manifestPath --dry-run |
    Tee-Object -FilePath (Join-Path $operationDir 'preview.json')
if ($LASTEXITCODE -ne 0) { throw 'Preview failed; do not apply' }
# Review the manifest, overlaps and impact; obtain the required approval before applying.
& $semesterExe create --file $manifestPath --approve |
    Tee-Object -FilePath (Join-Path $operationDir 'receipt.json')
if ($LASTEXITCODE -ne 0) { throw 'Creation failed; inspect and retry the SAME manifest if appropriate' }
& $semesterExe list
```

For a reviewed overlap, add `--acknowledge-overlap` to **apply**. Preview reports
overlaps without that flag. No approved general no-overlap policy was found. An
overlap can change semester selection for **future bookings from existing one-off
slots** (latest containing start, then UUID); preview counts these selector changes.
Existing sessions keep their stored semester. Adjacent ranges do not overlap.
Acknowledgement cannot override an identity conflict.

JSON goes to stdout; redacted errors go to stderr. Exit codes: `0` command succeeded
(including reports requiring review), `1` operational failure, `2` invalid input or
unsupported command, `3` conflict/unknown semester/unacknowledged overlap. Check
`outcome` and `blocking_reasons`. Creation requires exactly one of `--dry-run` or
`--approve`. Help and invalid-input checks do not connect to PostgreSQL.

To build the CLI in the existing Docker image without starting any services:

```powershell
docker build --target runtime -t tutti-semester-ops ./backend
if ($LASTEXITCODE -ne 0) { throw 'Image build failed' }
docker run --rm tutti-semester-ops /semester-maintenance --help
```

The image includes `/semester-maintenance`, `/tutti-api` and `/tutti-worker`. Use the
native command above for local operator access, or approved container networking and
secret injection; container `localhost` is not the host. Rebuild after Go changes.
Compose ports, services and persistent volumes remain unchanged.

## Read-only impact inspection

```powershell
$existingSemester = Read-Host 'Existing semester UUID'
$proposedStart = Read-Host 'Proposed start RFC3339'
$proposedEnd = Read-Host 'Proposed exclusive end RFC3339'
$sourceZone = Read-Host 'Source IANA zone'
& $semesterExe inspect-impact --id $existingSemester --starts-at $proposedStart `
    --ends-at $proposedEnd --time-zone $sourceZone |
    Tee-Object -FilePath (Join-Path $operationDir 'impact.json')
if ($LASTEXITCODE -ne 0) { throw 'Impact inspection failed' }
```

This is always read-only; no `--dry-run` flag is necessary. Reports show current and
proposed bounds, expansion/contraction (both may be true for a shift), overlaps,
dependencies and blocking reasons. They expose aggregate counts, calendar IDs and
supplied provenance, not participant identities, emails, grades or feedback.

- `linked_rules`/`linked_materialized_slots` are records linked through a semester
  rule. `rules_affected_by_bound_change` is zero for unchanged bounds, otherwise all
  linked rules needing review. Outside slots need remediation; expansion can need
  new occurrences even with zero outside slots. No regeneration occurs.
- `exceptions_retained` counts all linked rule exceptions. The report does not
  equate an exception with a generated slot or guess an instant for its local date.
- One-off/detached `candidates` intersect current or proposed bounds. Containment and
  outside counts describe ranges, **not ownership**: these slots have no semester FK.
  Selector changes use the same whole-slot containment/order as booking. Candidates
  may appear in multiple overlapping-semester reports.
- Session totals/outside counts use stored semester and status; omitted status keys
  mean zero. REQUESTED/ACCEPTED are active; other statuses retain history. Reservation
  counts are participant **blocks**, normally two per active session. `future_*`
  means `ends_at > action_at`, including ongoing windows. History, attendance,
  violations and flags remain attributed to their original semester.
- Tutor counts are distinct within each category, so recurring and one-off counts
  cannot be added to infer distinct people. They define manual review scope, not
  current eligibility. Existing publication/booking services still recheck Identity,
  program membership and approved offerings.

**There is no update/delete command.** Changed bounds produce a blocking reason and
`modification_supported: false`, even without dependent rows. Creation cannot replace
the same identity. A correction requires a separately approved migration/remediation
plan, dependency and lock review, and tutor reconfirmation. Never rewrite/delete
bookings, reservation evidence, semester attribution or disciplinary history to make
a proposed correction pass.

## Rollover and tutor reconfirmation

1. Verify, preview and create the next published semester with approval.
2. Review explicit old and next IDs. The next start must be later than the old start;
   overlap is disclosed, not erased.

   ```powershell
   $previousSemester = Read-Host 'Previous semester UUID'
   $nextSemester = Read-Host 'Next semester UUID'
   & $semesterExe rollover-check --from $previousSemester --to $nextSemester |
       Tee-Object -FilePath (Join-Path $operationDir 'rollover.json')
   if ($LASTEXITCODE -ne 0) { throw 'Rollover review failed' }
   ```

3. Check remaining future slots/reservations, old history and next publication.
   Publisher counts use rules plus one-offs assigned by the current booking selector.
   `previous_without_next_publication` needs schedule setup. **All** prior publishers
   need manual reconfirmation; a next rule does not prove human confirmation. There
   is no reconfirmation field or approved reminder type.
4. Identify/contact tutors using authorized faculty workflows and approved channels,
   review continuing eligibility, and request separate next-semester publication and
   reconfirmation. Keep follow-up evidence in the operations record. The CLI neither
   exports tutor identities nor sends messages.
5. Re-run the review after publication. Verify boundaries in the disposable suite
   below. Do not make test bookings in a shared database to verify rollout.

Rollover copies no rules, removes no exceptions, revokes no approvals, cancels no
sessions, resets no flags and queues no notifications. Historical qualifications,
sessions and disciplinary attribution remain intact under existing access/retention
rules. Issue #41 reliability/uptime work is separate from calendar maintenance.

## Transactions, recovery and verification

List, previews, impact and rollover use **REPEATABLE READ READ ONLY** transactions.
They do not write then undo, allocate PostgreSQL IDs, change sequences or alter
migration metadata. Snapshots can become stale; preview is not an apply token.

Apply revalidates in READ COMMITTED after a short `SHARE ROW EXCLUSIVE` lock on
`semesters`, checks identity/overlap again, then inserts. The lock serializes calendar
writers while allowing Scheduling's `FOR SHARE` reads. Waiting is bounded by a
five-second lock timeout and 30-second command context. The tool does not lock
session/slot rows or offer date updates that could race with publication, booking,
cancellation or expiry. Direct privileged SQL can bypass the tool's identity guards;
use this tool for creation. No migration is needed: existing UUID/finite-bound
constraints and writer serialization protect this workflow without altering legacy data.

Read commands need SELECT on referenced Catalog/Scheduling/Coordination tables. Apply
also needs INSERT and permission to acquire the calendar table lock. Use an authorized
maintenance account and protect its connection; source/operator/approval fields are
review assertions, not authentication. No role grants are changed by this task.

After timeout/connectivity failure, inspect `list` and retry the **identical manifest**.
A failed transaction rolls back; lost acknowledgement or failed receipt output can
mean creation committed. An identical retry returns the existing row. Investigate
identity conflicts instead of inventing new IDs/names. Preserve original receipts.
When finished, use `Remove-Item Env:DATABASE_URL` and archive the operation directory
in the approved restricted record system.

From the repository root, with Docker Desktop running Linux containers:

```powershell
.\backend\tests\integration\test_docker.ps1 -SemestersOnly
.\backend\tests\integration\test_docker.ps1
Push-Location backend
go test ./...
go vet ./...
go build ./...
Pop-Location
```

The runner rebuilds images and creates unique disposable PostgreSQL 17 and Redis
containers. The full run covers formatting, vet, race-enabled Go tests, service
regressions, local SMTP worker checks, schema/concurrency tests and migration
rollback/reapplication. It does not use `.env` or Compose databases. Native integration
cases skip without the runner's environment. Hosted CI's unfiltered suites include
the new tests and packaging includes the CLI. Hosted CI/review remain release gates;
local checks do not prove those gates passed.
