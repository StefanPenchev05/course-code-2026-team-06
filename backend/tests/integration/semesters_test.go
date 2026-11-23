package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	caldomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/interfaces/cli"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	scheduledomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Synthetic 2060 dates isolate these fixtures from the other integration suites;
// they make no assertion about a published university calendar.
func calendarManifest(id int, name, start, end string) caldomain.CalendarInput {
	return caldomain.CalendarInput{ID: fixture(id), Name: name, StartsAt: start, EndsAt: end, TimeZone: "UTC", Source: "https://www.uni.lu/en/about/academic-calendar/", SourceRevision: "synthetic integration fixture", Operator: "integration test", Approval: "disposable fixture only"}
}

func TestSemesterMaintenance(t *testing.T) {
	dsn := os.Getenv("TUTTI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("run tests/integration/test_docker.ps1 -SemestersOnly for disposable PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deps := catalogpg.CalendarDependencies{Scheduling: schedulingpg.Calendar, Discipline: coordpg.Calendar}
	repo := catalogpg.NewCalendar(db, deps)
	var clock atomic.Int64
	clock.Store(time.Date(2059, 12, 31, 0, 0, 0, 0, time.UTC).UnixMicro())
	now := func() time.Time { return time.UnixMicro(clock.Load()).UTC() }
	service := catalog.NewCalendar(repo, now)
	old := calendarManifest(50001, "Synthetic old 2060", "2060-01-01T00:00:00Z", "2060-02-01T00:00:00Z")
	next := calendarManifest(50002, "Synthetic next 2060", "2060-02-01T00:00:00Z", "2060-03-01T00:00:00Z")
	execSQL := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int, sql string, args ...any) {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, sql, args...).Scan(&n); err != nil || n != want {
			t.Fatalf("count got %d want %d: %v", n, want, err)
		}
	}
	unchanged := func(before map[string]string) {
		t.Helper()
		if after := calendarSnapshot(t, ctx, db); !reflect.DeepEqual(before, after) {
			for table, v := range before {
				if after[table] != v {
					t.Errorf("read-only operation changed %s", table)
				}
			}
		}
	}

	t.Run("dry-run validation and server read-only enforcement", func(t *testing.T) {
		before := calendarSnapshot(t, ctx, db)
		r, err := service.Create(ctx, old, true, false)
		if err != nil || r.Outcome != "would_create" || !r.ReadOnly {
			t.Fatalf("%+v %v", r, err)
		}
		bad := old
		bad.EndsAt = bad.StartsAt
		if _, err := service.Create(ctx, bad, true, false); !errors.Is(err, caldomain.ErrCalendarInput) {
			t.Fatal(err)
		}
		if _, err := service.Create(ctx, bad, false, false); !errors.Is(err, caldomain.ErrCalendarInput) {
			t.Fatal(err)
		}
		entry, _ := old.Validate()
		err = repo.CalendarTransaction(ctx, true, func(tx catalog.CalendarTx) error { return tx.Insert(ctx, entry) })
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
			t.Fatalf("expected PostgreSQL read-only rejection, got %v", err)
		}
		unchanged(before)
	})
	t.Run("concurrent identical creates and conflicting retries", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan string, 8)
		failures := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				in := old
				if i%2 == 1 {
					in.ID = fixture(50100 + i)
					in.Name = " synthetic OLD   2060 "
				}
				r, err := service.Create(ctx, in, false, false)
				results <- r.Outcome
				failures <- err
			}(i)
		}
		close(start)
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		created := 0
		for result := range results {
			if result == "created" {
				created++
			} else if result != "already_exists" {
				t.Fatal(result)
			}
		}
		if created != 1 {
			t.Fatalf("created %d", created)
		}
		// The first serialized writer owns the stable ID. Read and retain it for all retries.
		if err := db.QueryRow(ctx, "SELECT id::text FROM semesters WHERE starts_at=$1 AND ends_at=$2", old.StartsAt, old.EndsAt).Scan(&old.ID); err != nil {
			t.Fatal(err)
		}
		r, err := service.Create(ctx, old, false, false)
		if err != nil || r.Outcome != "already_exists" || r.Semester.ID != old.ID {
			t.Fatalf("retry %+v %v", r, err)
		}
		count(1, "SELECT count(*) FROM semesters WHERE starts_at=$1 AND ends_at=$2", old.StartsAt, old.EndsAt)
		before := calendarSnapshot(t, ctx, db)
		bad := old
		bad.EndsAt = "2060-02-02T00:00:00Z"
		for _, dry := range []bool{true, false} {
			if _, err := service.Create(ctx, bad, dry, true); !errors.Is(err, caldomain.ErrCalendarConflict) {
				t.Fatalf("conflicting retry: %v", err)
			}
		}
		unchanged(before)
	})

	// Real Identity/Tutoring/Scheduling contracts: no permissive substitute for
	// eligibility, recurrence generation, booking, history or reservation triggers.
	execSQL("INSERT INTO faculties(id,name) VALUES($1,'Semester test faculty')", fixture(50010))
	execSQL("INSERT INTO programs VALUES($1,$2,'Semester test program','BACHELOR')", fixture(50011), fixture(50010))
	execSQL("INSERT INTO subjects VALUES($1,'SEM50','Semester test subject')", fixture(50012))
	execSQL("INSERT INTO program_subjects VALUES($1,$2)", fixture(50011), fixture(50012))
	for _, id := range []int{50020, 50021, 50022} {
		execSQL("INSERT INTO users(id,name,email) VALUES($1,'Calendar fixture',$2)", fixture(id), fmt.Sprintf("semester-%d@uni.example", id))
	}
	execSQL("INSERT INTO coordinators VALUES($1,'SEM-C',$2)", fixture(50022), fixture(50010))
	for _, id := range []int{50020, 50021} {
		execSQL("INSERT INTO students VALUES($1,$2,$3)", fixture(id), fmt.Sprintf("SEM-%d", id), fixture(50011))
		execSQL("INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$3)", fixture(id), fixture(50010), fixture(50022))
	}
	execSQL("INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Fixture',now())", fixture(50020))
	execSQL("INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,$3,'Fixture','APPROVED',now(),$4)", fixture(50030), fixture(50020), fixture(50010), fixture(50022))
	execSQL("INSERT INTO tutor_qualifications(id,application_id,student_id,subject_id,grade) VALUES($1,$2,$3,$4,18)", fixture(50031), fixture(50030), fixture(50020), fixture(50012))
	execSQL("INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Fixture')", fixture(50032), fixture(50020), fixture(50012), fixture(50031))
	tutor := identity.Principal{ID: fixture(50020), Student: true, Tutor: true, Verification: identity.Approved, ProgramID: fixture(50011)}
	student := identity.Principal{ID: fixture(50021), Student: true, Verification: identity.Approved, ProgramID: fixture(50011)}
	tutorAccess := tutoring.New(tutoringpg.New(db, tutoringpg.Dependencies{}), catalog.New(catalogpg.New(db)), nil)
	availability := scheduling.New(schedulingpg.New(db, identitypg.LockStudent), tutorAccess, now)
	book := scheduling.NewBooking(schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{LockStudent: identitypg.LockStudent, Catalog: func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) }, Offerings: tutoringpg.BookingOfferings, Discipline: coordpg.Discipline}), now)
	instant := func(day, hour int) time.Time { return time.Date(2060, 1, day, hour, 0, 0, 0, time.UTC) }
	weekday := int(instant(1, 0).Weekday())
	if weekday == 0 {
		weekday = 7
	}
	rule, err := availability.CreateRule(ctx, tutor, scheduledomain.Rule{SemesterID: old.ID, Weekday: weekday, Start: "10:00", End: "12:00", Zone: "UTC", Mode: scheduledomain.Online})
	if err != nil {
		t.Fatal(err)
	}
	slotFor := func(day int) string {
		var id string
		if err := db.QueryRow(ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 AND starts_at=$2", rule.ID, instant(day, 10)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	makeSlot := func(day, start, end int) scheduledomain.Slot {
		slot, err := availability.CreateSlot(ctx, tutor, scheduledomain.Window{StartsAt: instant(day, start), EndsAt: instant(day, end), Mode: scheduledomain.Online})
		if err != nil {
			t.Fatal(err)
		}
		return slot
	}
	request := func(slot string, at time.Time) scheduledomain.Session {
		s, err := book.Request(ctx, student, scheduledomain.Request{OfferingID: fixture(50032), SlotID: slot, StartsAt: at, Minutes: 60, Mode: scheduledomain.Online, Purpose: "HOMEWORK", Description: "Calendar integration"})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if err := availability.DeleteSlot(ctx, tutor, slotFor(29)); err != nil {
		t.Fatal(err)
	}
	completed := request(slotFor(1), instant(1, 10))
	requested := request(slotFor(8), instant(8, 10))
	accepted := request(slotFor(15), instant(15, 10))
	for _, s := range []scheduledomain.Session{completed, accepted} {
		if _, err := book.Accept(ctx, tutor, s.ID, scheduledomain.Meeting{Link: "https://meet.example/test"}); err != nil {
			t.Fatal(err)
		}
	}
	makeSlot(3, 10, 12)
	cancelSlot := makeSlot(1, 14, 18)
	cancelA := request(cancelSlot.ID, instant(1, 14))
	cancelB := request(cancelSlot.ID, instant(1, 16))
	clock.Store(instant(1, 12).Add(time.Minute).UnixMicro())
	if _, err := book.Complete(ctx, tutor, completed.ID); err != nil {
		t.Fatal(err)
	}
	for _, s := range []scheduledomain.Session{cancelA, cancelB} {
		if _, err := book.Cancel(ctx, student, s.ID, "Fixture late cancellation"); err != nil {
			t.Fatal(err)
		}
	}
	execSQL("INSERT INTO attendance_reports(session_id,reporter_user_id,absent_user_id,reported_at) VALUES($1,$2,$3,$4)", completed.ID, student.ID, tutor.ID, now())
	clock.Store(instant(2, 0).UnixMicro())

	t.Run("contraction expansion exact boundaries and preserved evidence", func(t *testing.T) {
		before := calendarSnapshot(t, ctx, db)
		r, err := service.InspectImpact(ctx, old.ID, "2060-01-09T00:00:00Z", "2060-01-14T00:00:00Z", "UTC")
		if err != nil {
			t.Fatal(err)
		}
		i := r.Impact
		if !i.Contracts || i.Expands || i.ModificationSupported || len(i.BlockingReasons) == 0 {
			t.Fatalf("unsafe change %+v", i)
		}
		for name, want := range map[string]int64{"linked_rules": 1, "linked_materialized_slots": 4, "linked_slots_outside_proposed_bounds": 4, "exceptions_retained": 1, "active_reservation_blocks": 4, "reservation_blocks_outside_proposed_bounds": 4, "oneoff_or_detached_candidates": 2, "oneoff_candidates_outside_proposed_bounds": 2, "rule_tutors_requiring_review": 1} {
			if i.Activity.Counts[name] != want {
				t.Errorf("%s=%d want %d", name, i.Activity.Counts[name], want)
			}
		}
		for status, want := range map[string]int64{"REQUESTED": 1, "ACCEPTED": 1, "COMPLETED": 1, "CANCELLED": 2} {
			if i.Activity.Sessions[status] != want || i.Activity.SessionsOutside[status] != want {
				t.Errorf("status %s: %+v", status, i.Activity)
			}
		}
		if i.Discipline["violations_retained"] != 2 || i.Discipline["flags_retained"] != 1 || i.Discipline["attendance_reports_retained"] != 1 {
			t.Fatalf("discipline %+v", i.Discipline)
		}
		if i.Activity.Counts["session_history_rows_retained"] != 10 {
			t.Fatalf("history %+v", i.Activity.Counts)
		}
		for _, bounds := range [][2]string{{"2060-01-01T00:00:00Z", "2060-02-01T00:00:00Z"}, {"2059-12-30T00:00:00Z", "2060-02-02T00:00:00Z"}, {"2060-01-01T10:00:00Z", "2060-01-22T12:00:00Z"}} {
			r, err := service.InspectImpact(ctx, old.ID, bounds[0], bounds[1], "UTC")
			if err != nil {
				t.Fatal(err)
			}
			if r.Impact.Activity.Counts["linked_slots_outside_proposed_bounds"] != 0 {
				t.Fatalf("boundary report %+v", r.Impact)
			}
			if bounds[0] == "2059-12-30T00:00:00Z" && !r.Impact.Expands {
				t.Fatal("expansion not marked")
			}
			if bounds[0] == old.StartsAt && bounds[1] == old.EndsAt && (r.Impact.Expands || r.Impact.Contracts || len(r.Impact.BlockingReasons) != 0) {
				t.Fatal("unchanged bounds reported a change")
			}
		}
		conflict := old
		conflict.EndsAt = "2060-01-14T00:00:00Z"
		if _, err := service.Create(ctx, conflict, false, true); !errors.Is(err, caldomain.ErrCalendarConflict) {
			t.Fatalf("unsafe replacement %v", err)
		}
		unchanged(before)
	})
	t.Run("rollover and new-semester scheduling boundaries", func(t *testing.T) {
		beforeCreate := calendarSnapshot(t, ctx, db)
		if _, err := service.Create(ctx, next, false, false); err != nil {
			t.Fatal(err)
		}
		afterCreate := calendarSnapshot(t, ctx, db)
		delete(beforeCreate, "semesters")
		delete(afterCreate, "semesters")
		if !reflect.DeepEqual(beforeCreate, afterCreate) {
			t.Fatal("calendar creation changed non-calendar data")
		}
		before := calendarSnapshot(t, ctx, db)
		r, err := service.Rollover(ctx, old.ID, next.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Rollover.Tutors["previous_without_next_publication"] != 1 || r.Rollover.Next.Activity.Counts["linked_rules"] != 0 || r.Rollover.Previous.Activity.Counts["future_active_sessions"] != 2 {
			t.Fatalf("rollover %+v", r.Rollover)
		}
		unchanged(before)
		nextStart := time.Date(2060, 2, 1, 0, 0, 0, 0, time.UTC)
		nextDay := int(nextStart.Weekday())
		if nextDay == 0 {
			nextDay = 7
		}
		nextRule, err := availability.CreateRule(ctx, tutor, scheduledomain.Rule{SemesterID: next.ID, Weekday: nextDay, Start: "10:00", End: "12:00", Zone: "UTC", Mode: scheduledomain.Online})
		if err != nil {
			t.Fatal(err)
		}
		count(0, "SELECT count(*) FROM availability_slots WHERE rule_id=$1 AND (starts_at<$2 OR ends_at>$3)", nextRule.ID, next.StartsAt, next.EndsAt)
		var nextSlot string
		if err := db.QueryRow(ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 ORDER BY starts_at LIMIT 1", nextRule.ID).Scan(&nextSlot); err != nil {
			t.Fatal(err)
		}
		newSession := request(nextSlot, nextStart.Add(10*time.Hour))
		if newSession.SemesterID != next.ID {
			t.Fatal("wrong new-session semester")
		}
		count(3, "SELECT count(*) FROM tutoring_sessions WHERE id=ANY($1::uuid[]) AND semester_id=$2", []string{requested.ID, accepted.ID, completed.ID}, old.ID)
		// Exact end is allowed, one microsecond beyond all configured bounds is not.
		end := time.Date(2060, 3, 1, 0, 0, 0, 0, time.UTC)
		w := scheduledomain.Window{StartsAt: end.Add(-time.Hour), EndsAt: end, Mode: scheduledomain.Online}
		if _, err := availability.CreateSlot(ctx, tutor, w); err != nil {
			t.Fatal(err)
		}
		w.StartsAt = end
		w.EndsAt = end.Add(time.Microsecond)
		if _, err := availability.CreateSlot(ctx, tutor, w); !errors.Is(err, scheduledomain.ErrInvalid) {
			t.Fatalf("out-of-semester slot %v", err)
		}
		r, err = service.Rollover(ctx, old.ID, next.ID)
		if err != nil || r.Rollover.Tutors["previous_without_next_publication"] != 0 || r.Rollover.Tutors["previous_requiring_manual_reconfirmation"] != 1 {
			t.Fatalf("reconfirmation %+v %v", r.Rollover, err)
		}
	})
	t.Run("overlap preview explains one-off attribution and requires acknowledgement", func(t *testing.T) {
		overlap := calendarManifest(50003, "Synthetic classification", "2060-01-02T00:00:00Z", "2060-01-05T00:00:00Z")
		before := calendarSnapshot(t, ctx, db)
		r, err := service.Create(ctx, overlap, true, false)
		if err != nil || len(r.Overlaps) != 1 || r.Impact.Activity.Counts["oneoff_future_booking_selector_changes"] != 1 {
			t.Fatalf("%+v %v", r, err)
		}
		if _, err := service.Create(ctx, overlap, false, false); !errors.Is(err, caldomain.ErrCalendarOverlap) {
			t.Fatal(err)
		}
		unchanged(before)
		if _, err := service.Create(ctx, overlap, false, true); err != nil {
			t.Fatal(err)
		}
		count(1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND semester_id=$2", requested.ID, old.ID)
	})
	t.Run("read-only snapshots during availability booking and rollover", func(t *testing.T) {
		for _, operation := range []string{"publication", "booking", "rollover"} {
			t.Run(operation, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				barrierDeps := deps
				barrierDeps.Scheduling = func(tx pgx.Tx) catalog.CalendarScheduling {
					return calendarBarrier{schedulingpg.Calendar(tx), func() {
						once.Do(func() {
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
							}
						})
					}}
				}
				reader := catalog.NewCalendar(catalogpg.NewCalendar(db, barrierDeps), now)
				baseline, err := service.InspectImpact(ctx, old.ID, old.StartsAt, old.EndsAt, "UTC")
				if err != nil {
					t.Fatal(err)
				}
				results := make(chan catalog.CalendarReceipt, 1)
				failures := make(chan error, 1)
				go func() {
					var r catalog.CalendarReceipt
					var err error
					if operation == "rollover" {
						r, err = reader.Rollover(ctx, old.ID, next.ID)
					} else {
						r, err = reader.InspectImpact(ctx, old.ID, "2060-01-09T00:00:00Z", "2060-01-14T00:00:00Z", "UTC")
					}
					results <- r
					failures <- err
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				// A report's repeatable-read snapshot is established by List before
				// this barrier. Real writes must commit without waiting for the report.
				if operation == "booking" {
					request(slotFor(22), instant(22, 10))
				} else if operation == "rollover" {
					makeSlot(25, 14, 15)
				} else {
					makeSlot(24, 14, 15)
				}
				close(release)
				r := <-results
				if err := <-failures; err != nil {
					t.Fatal(err)
				}
				activity := r.Impact
				if operation == "rollover" {
					activity = &r.Rollover.Previous
				}
				if !reflect.DeepEqual(activity.Activity.Sessions, baseline.Impact.Activity.Sessions) || activity.Activity.Counts["oneoff_or_detached_candidates"] != baseline.Impact.Activity.Counts["oneoff_or_detached_candidates"] {
					t.Fatalf("mixed snapshot %+v vs %+v", activity, baseline.Impact)
				}
				after, err := service.InspectImpact(ctx, old.ID, old.StartsAt, old.EndsAt, "UTC")
				if err != nil {
					t.Fatal(err)
				}
				if operation == "booking" && after.Impact.Activity.Sessions["REQUESTED"] != baseline.Impact.Activity.Sessions["REQUESTED"]+1 {
					t.Fatal("booking not visible in new snapshot")
				}
				if operation != "booking" && after.Impact.Activity.Counts["oneoff_or_detached_candidates"] != baseline.Impact.Activity.Counts["oneoff_or_detached_candidates"]+1 {
					t.Fatal("publication not visible in new snapshot")
				}
			})
		}
	})
	t.Run("CLI JSON previews conflicts and acknowledgement loss recovery", func(t *testing.T) {
		manifest := calendarManifest(50004, "Synthetic CLI", "2061-01-01T00:00:00Z", "2061-02-01T00:00:00Z")
		data, _ := json.Marshal(manifest)
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		open := func(context.Context) (*catalog.CalendarService, func(), error) { return service, func() {}, nil }
		var out, stderr bytes.Buffer
		before := calendarSnapshot(t, ctx, db)
		if code := cli.Run(ctx, []string{"create", "--file", path, "--dry-run"}, &out, &stderr, open); code != 0 {
			t.Fatalf("%d %s", code, stderr.String())
		}
		var receipt catalog.CalendarReceipt
		if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || receipt.Outcome != "would_create" {
			t.Fatalf("%v %s", err, out.String())
		}
		unchanged(before)
		// Simulate a committed transaction whose receipt cannot reach the client.
		stderr.Reset()
		if code := cli.Run(ctx, []string{"create", "--file", path, "--approve"}, calendarBrokenWriter{}, &stderr, open); code != 1 || !strings.Contains(stderr.String(), "may have committed") {
			t.Fatalf("%d %s", code, stderr.String())
		}
		out.Reset()
		stderr.Reset()
		if code := cli.Run(ctx, []string{"create", "--file", path, "--approve"}, &out, &stderr, open); code != 0 || !strings.Contains(out.String(), "already_exists") {
			t.Fatalf("%d %s %s", code, out.String(), stderr.String())
		}
		count(1, "SELECT count(*) FROM semesters WHERE id=$1", manifest.ID)
		out.Reset()
		stderr.Reset()
		if code := cli.Run(ctx, []string{"inspect-impact", "--id", fixture(59999), "--starts-at", manifest.StartsAt, "--ends-at", manifest.EndsAt, "--time-zone", "UTC"}, &out, &stderr, open); code != 3 {
			t.Fatalf("missing %d %s", code, stderr.String())
		}
	})
	t.Run("two executable processes share the same creation identity", func(t *testing.T) {
		binary := "/out/semester-maintenance" // existing Docker build target
		if _, err := os.Stat(binary); err != nil {
			binary = filepath.Join(t.TempDir(), "semester-maintenance.exe")
			cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/semester-maintenance")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("build executable: %v %s", err, output)
			}
		}
		manifest := calendarManifest(50005, "Synthetic process retry", "2062-01-01T00:00:00Z", "2062-02-01T00:00:00Z")
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		type result struct {
			output []byte
			err    error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				cmd := exec.CommandContext(ctx, binary, "create", "--file", path, "--approve")
				cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn)
				output, err := cmd.CombinedOutput()
				results <- result{output, err}
			}()
		}
		close(start)
		outcomes := map[string]int{}
		for i := 0; i < 2; i++ {
			r := <-results
			if r.err != nil {
				t.Fatalf("CLI process: %v %s", r.err, r.output)
			}
			var receipt catalog.CalendarReceipt
			if err := json.Unmarshal(r.output, &receipt); err != nil {
				t.Fatal(err)
			}
			outcomes[receipt.Outcome]++
		}
		if outcomes["created"] != 1 || outcomes["already_exists"] != 1 {
			t.Fatalf("process outcomes: %v", outcomes)
		}
		count(1, "SELECT count(*) FROM semesters WHERE id=$1", manifest.ID)
	})
}

type calendarBarrier struct {
	catalog.CalendarScheduling
	before func()
}

func (b calendarBarrier) Inspect(ctx context.Context, current, proposed caldomain.Semester, at time.Time) (catalog.CalendarActivity, error) {
	b.before()
	return b.CalendarScheduling.Inspect(ctx, current, proposed, at)
}

type calendarBrokenWriter struct{}

func (calendarBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("lost receipt") }

// Hash every public table, including schema_migrations and histories, rather
// than checking only row counts (which would miss in-place mutations).
func calendarSnapshot(t *testing.T, ctx context.Context, db *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := db.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, table := range tables {
		var hash string
		sql := "SELECT md5(coalesce(string_agg(row_to_json(t)::text,E'\\n' ORDER BY row_to_json(t)::text),'')) FROM " + pgx.Identifier{"public", table}.Sanitize() + " t"
		if err := db.QueryRow(ctx, sql).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		result[table] = hash
	}
	return result
}
