package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	outboxpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/postgres"
	fanout "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/redis"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutorpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func domainRouting(f *bookingFixture) app.DomainRouting {
	return app.DomainRouting{Metadata: outboxpg.New(f.db), Tutoring: tutorpg.New(f.db, tutorpg.Dependencies{}),
		Coordination: coordpg.NewRealtime(f.db), Identity: identitypg.New(f.db)}
}

func (f *publicationFixture) caseWant(t *testing.T, kind string, recipients ...int) publicationWant {
	t.Helper()
	var id string
	if err := f.db.QueryRow(f.ctx, `SELECT entity_id::text FROM realtime_events WHERE dispatched_at IS NULL AND resource_kind=$1`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return publicationWant{app.CoordinationChanged, id, recipients}
}
func activityPublication(tutor int) publicationWant {
	return publicationWant{app.ActivityChanged, fixture(tutor), []int{tutor, bookingCoordinator, bookingOtherStudent}}
}
func (f *publicationFixture) registerIssues() {
	repo := coordpg.NewIssues(f.db, coordpg.IssueDependencies{
		LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty,
		HomeFaculty: func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
			return catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program)
		},
		LockParticipantSession: func(ctx context.Context, tx pgx.Tx, id, actor string) (scheduling.Session, error) {
			return schedulingpg.AttendanceSessions(tx).LockParticipantSession(ctx, id, actor)
		},
	})
	coordhttp.NewIssues(coordination.NewIssues(repo), slog.Default()).RegisterRoutes(f.a.handler.(*http.ServeMux), f.auth)
}

func TestRealtimeDomainApplications(t *testing.T) {
	f := newPublicationFixture(t)
	const private = "Private qualification evidence"
	apply := func(t *testing.T, actor int) string {
		return fields(t, f.a.call(t, 201, "POST", "/tutor-applications", map[string]any{"subject_id": fixture(405), "grade": 18.75, "description": private}, f.cookie[actor]))["application_id"].(string)
	}
	var approved, rejected string
	t.Run("submission and stored faculty audience", func(t *testing.T) {
		f.exec(t, `CREATE FUNCTION fail_application_capture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='tutor_application.changed' THEN RAISE EXCEPTION 'test capture failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER fail_application_capture BEFORE INSERT ON realtime_events FOR EACH ROW EXECUTE FUNCTION fail_application_capture()`)
		f.a.call(t, 500, "POST", "/tutor-applications", map[string]any{"subject_id": fixture(405), "grade": 18.75, "description": private}, f.cookie[bookingStudent])
		f.expect(t)
		f.count(t, 0, "SELECT count(*) FROM tutor_applications WHERE student_id=$1", fixture(bookingStudent))
		f.exec(t, "DROP TRIGGER fail_application_capture ON realtime_events; DROP FUNCTION fail_application_capture()")
		approved = apply(t, bookingStudent)
		f.expect(t, publicationWant{app.TutorApplicationChanged, approved, []int{bookingStudent, bookingHomeCoordinator}})
		f.a.call(t, 404, "GET", "/coordinator/tutor-applications/"+approved, nil, f.cookie[bookingCoordinator])
	})
	t.Run("failed and concurrent approvals capture only winning decision", func(t *testing.T) {
		path := "/coordinator/tutor-applications/" + approved + "/approve"
		f.a.call(t, 404, "POST", path, nil, f.cookie[bookingCoordinator])
		f.expect(t)
		start := make(chan struct{})
		codes := make(chan int, 2)
		for range 2 {
			go func() {
				<-start
				codes <- f.a.request("POST", path, origin, nil, f.cookie[bookingHomeCoordinator]).Code
			}()
		}
		close(start)
		if a, b := <-codes, <-codes; !(a == 204 && b == 409 || a == 409 && b == 204) {
			t.Fatalf("approval race %d %d", a, b)
		}
		f.disconnected(t, bookingStudent) // tutor role changed: gateway demands reconnect.
		f.connect(t, 0, bookingStudent)
		f.expect(t, publicationWant{app.TutorApplicationChanged, approved, []int{bookingStudent, bookingHomeCoordinator}}, publicationWant{app.AccountAccessChanged, "", []int{bookingStudent}})
		f.count(t, 2, "SELECT count(*) FROM application_status_history WHERE application_id=$1", approved)
		f.count(t, 1, "SELECT count(*) FROM notifications WHERE application_id=$1 AND type='APPLICATION_APPROVED'", approved)
	})
	t.Run("rejection retry and duplicate delivery keep stable identity", func(t *testing.T) {
		rejected = apply(t, bookingOtherStudent)
		f.expect(t, publicationWant{app.TutorApplicationChanged, rejected, []int{bookingOtherStudent, bookingCoordinator}})
		f.a.call(t, 204, "POST", "/coordinator/tutor-applications/"+rejected+"/reject", map[string]string{"reason": private}, f.cookie[bookingCoordinator])
		before := f.pending(t)
		options := *f.redis.Options()
		broken := redis.NewClient(&options)
		_ = broken.Close()
		if n, err := app.NewDispatcher(f.store, fanout.New(broken)).RunBatch(f.ctx); err == nil || n != 0 {
			t.Fatal(n, err)
		}
		f.count(t, 1, "SELECT count(*) FROM tutor_applications WHERE id=$1 AND status='REJECTED'", rejected)
		f.exec(t, "UPDATE realtime_events SET next_attempt_at=statement_timestamp() WHERE dispatched_at IS NULL")
		after := f.expect(t, publicationWant{app.TutorApplicationChanged, rejected, []int{bookingOtherStudent, bookingCoordinator}})
		for id, event := range before {
			if after[id] != event {
				t.Fatal("changed retry payload")
			}
		}
		// Republish the same IDs before a fresh event. FIFO Redis processing plus
		// the next event's probes make absence of duplicate socket frames observable.
		for id := range after {
			if err := f.bus.Publish(f.ctx, id); err != nil {
				t.Fatal(err)
			}
		}
		f.exec(t, "UPDATE tutor_offerings SET is_active=false,deactivated_at=now(),deactivation_reason='Eligibility changed' WHERE id=$1", fixture(bookingOffer))
		f.expect(t, publicationWant{app.TutorApplicationChanged, fixture(421), []int{bookingTutor, bookingCoordinator}})
		f.count(t, 1, "SELECT count(*) FROM notifications WHERE application_id=$1 AND type='APPLICATION_REJECTED'", rejected)
	})
	t.Run("program eligibility reconciliation captures and rolls back atomically", func(t *testing.T) {
		f.exec(t, "INSERT INTO programs VALUES($1,$2,'No matching subjects','BACHELOR')", fixture(88001), fixture(401))
		deps := tutorpg.Dependencies{Catalog: func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) }, LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty}
		// The caller-owned transaction is the existing integration point for #74.
		for _, commit := range []bool{false, true} {
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err = tx.Exec(f.ctx, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(bookingStudent), fixture(88001)); err != nil {
				t.Fatal(err)
			}
			if err = tutoring.RevalidateTutorEligibilityInTransaction(f.ctx, tutorpg.EligibilityInTransaction(tx, deps), fixture(bookingStudent)); err != nil {
				t.Fatal(err)
			}
			f.expect(t) // no uncommitted event visible to either API
			if !commit {
				if err = tx.Rollback(f.ctx); err != nil {
					t.Fatal(err)
				}
				f.expect(t)
				continue
			}
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			f.disconnected(t, bookingStudent)
			f.connect(t, 0, bookingStudent)
			f.expect(t, publicationWant{app.AccountAccessChanged, "", []int{bookingStudent}}, publicationWant{app.TutorApplicationChanged, approved, []int{bookingStudent, bookingHomeCoordinator}})
		}
		f.count(t, 1, "SELECT count(*) FROM tutor_offering_deactivations WHERE qualification_id IN(SELECT id FROM tutor_qualifications WHERE application_id=$1)", approved)
		coordinator, faculty, err := tutorpg.New(f.db, tutorpg.Dependencies{}).ActivityAudience(f.ctx, fixture(bookingStudent))
		if err != nil || coordinator != fixture(bookingCoordinator) || faculty != fixture(401) {
			t.Fatal("activity did not follow current home faculty", coordinator, faculty, err)
		}
	})
	t.Run("explicit reapproval invalidates both evidence pointers", func(t *testing.T) {
		id := apply(t, bookingTutor)
		current := publicationWant{app.TutorApplicationChanged, id, []int{bookingTutor, bookingCoordinator}}
		f.expect(t, current)
		f.a.call(t, 204, "POST", "/coordinator/tutor-applications/"+id+"/approve", nil, f.cookie[bookingCoordinator])
		f.expect(t, current, publicationWant{app.TutorApplicationChanged, fixture(421), []int{bookingTutor, bookingCoordinator}})
		f.count(t, 2, "SELECT count(*) FROM tutor_qualifications WHERE student_id=$1", fixture(bookingTutor))
	})
}

func TestRealtimeDomainCoordinationAndActivity(t *testing.T) {
	f := newPublicationFixture(t)
	f.registerIssues()
	t.Run("issues home faculty creation resolution and capture failure", func(t *testing.T) {
		body := map[string]string{"description": "Private report evidence"}
		f.exec(t, `CREATE FUNCTION fail_domain_capture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='coordination.changed' THEN RAISE EXCEPTION 'test capture failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER fail_domain_capture BEFORE INSERT ON realtime_events FOR EACH ROW EXECUTE FUNCTION fail_domain_capture()`)
		f.a.call(t, 500, "POST", "/issues", body, f.cookie[bookingStudent])
		f.expect(t)
		f.count(t, 0, "SELECT count(*) FROM issue_reports")
		f.count(t, 0, "SELECT count(*) FROM issue_status_history")
		f.exec(t, "DROP TRIGGER fail_domain_capture ON realtime_events; DROP FUNCTION fail_domain_capture()")
		id := f.session(t, 201, "POST", "/issues", body, bookingStudent)["id"].(string)
		want := publicationWant{app.CoordinationChanged, id, []int{bookingHomeCoordinator}}
		f.expect(t, want)
		path := "/coordinator/issues/" + id + "/resolve"
		note := map[string]string{"resolution_note": "Private resolution"}
		f.a.call(t, 404, "POST", path, note, f.cookie[bookingCoordinator])
		f.expect(t)
		f.a.call(t, 200, "POST", path, note, f.cookie[bookingHomeCoordinator])
		f.expect(t, want)
		f.a.call(t, 409, "POST", path, note, f.cookie[bookingHomeCoordinator])
		f.expect(t)
		f.count(t, 2, "SELECT count(*) FROM issue_status_history WHERE issue_report_id=$1", id)
	})
	var sessions []string
	for _, at := range []time.Duration{10 * time.Hour, 12 * time.Hour} {
		id := f.request(t, 201, bookingStudent, at, bookingOffer, bookingSlot)
		sessions = append(sessions, id)
		wants := []publicationWant{bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availabilityPublication(bookingTutor)}
		f.expect(t, wants...)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
		f.expect(t, wants...)
	}
	f.setTime(14 * time.Hour)
	t.Run("completion feedback and completed to no show keep HTTP aggregates authoritative", func(t *testing.T) {
		id := sessions[0]
		booking := bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator)
		availability := availabilityPublication(bookingTutor)
		activity := activityPublication(bookingTutor)
		f.session(t, 200, "POST", "/tutors/me/sessions/"+id+"/complete", nil, bookingTutor)
		f.expect(t, booking, availability, activity)
		f.exec(t, `CREATE FUNCTION fail_activity_capture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='activity.changed' THEN RAISE EXCEPTION 'test capture failure'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER fail_activity_capture BEFORE INSERT ON realtime_events FOR EACH ROW EXECUTE FUNCTION fail_activity_capture()`)
		f.a.call(t, 500, "POST", "/sessions/"+id+"/feedback", map[string]int{"rating": 5}, f.cookie[bookingStudent])
		f.expect(t)
		f.count(t, 0, "SELECT count(*) FROM feedback WHERE session_id=$1", id)
		f.exec(t, "DROP TRIGGER fail_activity_capture ON realtime_events; DROP FUNCTION fail_activity_capture()")
		f.session(t, 201, "POST", "/sessions/"+id+"/feedback", map[string]any{"rating": 5, "comment": "Private feedback must not leave HTTP"}, bookingStudent)
		f.expect(t, activity)
		stats := f.session(t, 200, "GET", "/tutors/me/activity", nil, bookingTutor)
		if stats["completed_sessions"] != float64(1) || stats["rating_count"] != float64(1) || stats["average_rating"] != float64(5) {
			t.Fatal(stats)
		}
		f.a.call(t, 409, "POST", "/sessions/"+id+"/feedback", map[string]int{"rating": 1}, f.cookie[bookingStudent])
		f.expect(t)
		for _, id := range sessions {
			// Tutor reports the cross-faculty learner: session case -> 401,
			// disciplinary history/flag -> learner's home faculty 402.
			report := f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", map[string]string{"description": "Private attendance allegation"}, bookingTutor)["id"].(string)
			caseEvent := publicationWant{app.CoordinationChanged, report, []int{bookingCoordinator}}
			f.expect(t, caseEvent)
			path := "/coordinator/attendance-reports/" + report + "/confirm"
			note := map[string]string{"resolution_note": "Private adjudication"}
			f.a.call(t, 404, "POST", path, note, f.cookie[bookingHomeCoordinator])
			f.expect(t)
			f.a.call(t, 200, "POST", path, note, f.cookie[bookingCoordinator])
			wants := []publicationWant{caseEvent, f.caseWant(t, "violation", bookingHomeCoordinator), bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability}
			if id == sessions[0] {
				wants = append(wants, activity)
			} else {
				wants = append(wants, f.caseWant(t, "flag", bookingHomeCoordinator))
			}
			f.expect(t, wants...)
			f.a.call(t, 409, "POST", path, note, f.cookie[bookingCoordinator])
			f.expect(t)
		}
		stats = f.session(t, 200, "GET", "/tutors/me/activity", nil, bookingTutor)
		if stats["completed_sessions"] != float64(0) || stats["rating_count"] != float64(0) || stats["average_rating"] != nil {
			t.Fatal(stats)
		}
		f.count(t, 1, "SELECT count(*) FROM feedback WHERE session_id=$1", id)
		f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1", fixture(bookingStudent))
		f.count(t, 1, "SELECT count(*) FROM notifications WHERE type='USER_FLAGGED'")
		f.count(t, 2, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status IN('COMPLETED','NO_SHOW')", id)
	})
	t.Run("suspension and reinstatement private target", func(t *testing.T) {
		for _, action := range []string{"suspend", "reinstate"} {
			res := f.session(t, 200, "POST", "/coordinator/users/"+fixture(bookingStudent)+"/"+action, suspensionReason, bookingHomeCoordinator)
			recipients := []int{bookingStudent}
			if action == "suspend" {
				f.disconnected(t, bookingStudent)
				recipients = nil
			} else {
				f.connect(t, 0, bookingStudent)
			}
			f.expect(t, publicationWant{app.AccountAccessChanged, "", recipients}, publicationWant{app.CoordinationChanged, res["decision_id"].(string), []int{bookingHomeCoordinator}})
		}
		f.count(t, 2, "SELECT count(*) FROM user_suspension_history WHERE user_id=$1", fixture(bookingStudent))
		f.count(t, 2, "SELECT count(*) FROM notifications WHERE type IN('USER_SUSPENDED','USER_REINSTATED')")
	})
	t.Run("attendance rejection changes only the case", func(t *testing.T) {
		id := f.request(t, 201, bookingStudent, 20*time.Hour, bookingOffer, bookingSlot)
		wants := []publicationWant{bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availabilityPublication(bookingTutor)}
		f.expect(t, wants...)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
		f.expect(t, wants...)
		f.setTime(22 * time.Hour)
		report := f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", map[string]string{"description": "Unconfirmed evidence"}, bookingStudent)["id"].(string)
		caseEvent := publicationWant{app.CoordinationChanged, report, []int{bookingCoordinator}}
		f.expect(t, caseEvent)
		f.a.call(t, 200, "POST", "/coordinator/attendance-reports/"+report+"/reject", map[string]string{"resolution_note": "Allegation rejected"}, f.cookie[bookingCoordinator])
		f.expect(t, caseEvent)
		f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='ACCEPTED'", id)
	})
	t.Run("late participant cancellation adds home faculty incident", func(t *testing.T) {
		id := f.request(t, 201, bookingStudent, 25*time.Hour, bookingOffer, bookingSlot)
		booking := bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator)
		availability := availabilityPublication(bookingTutor)
		f.expect(t, booking, availability)
		f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, bookingStudent)
		f.expect(t, booking, availability, f.caseWant(t, "violation", bookingHomeCoordinator))
		f.count(t, 1, "SELECT count(*) FROM user_violations WHERE session_id=$1 AND kind='LATE_CANCELLATION'", id)
		f.count(t, 0, "SELECT count(*) FROM users WHERE id=$1 AND is_suspended", fixture(bookingStudent))
	})
}

func TestRealtimeDomainAccess(t *testing.T) {
	f := newPublicationFixture(t)
	f.registerIssues()
	var verification string
	t.Run("verification submission rejection resubmission approval", func(t *testing.T) {
		// Existing pending fixture has no application; seed its initial submission.
		if err := f.db.QueryRow(f.ctx, `INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2) RETURNING id::text`, fixture(bookingPending), fixture(401)).Scan(&verification); err != nil {
			t.Fatal(err)
		}
		f.disconnected(t, bookingPending)
		f.connect(t, 0, bookingPending)
		access := publicationWant{app.AccountAccessChanged, "", []int{bookingPending}}
		f.expect(t, access, publicationWant{app.CoordinationChanged, verification, []int{bookingCoordinator}})
		f.a.call(t, 204, "POST", "/coordinator/verifications/"+verification+"/reject", map[string]string{"reason": "Private verification reason"}, f.cookie[bookingCoordinator])
		f.disconnected(t, bookingPending)
		f.connect(t, 0, bookingPending)
		f.expect(t, access, publicationWant{app.CoordinationChanged, verification, []int{bookingCoordinator}})
		f.a.call(t, 201, "POST", "/auth/resubmit-verification", map[string]string{"name": "Review Student", "email": "book-pending@uni.example", "student_number": "880001", "program_id": fixture(404)}, f.cookie[bookingPending])
		f.disconnected(t, bookingPending)
		f.connect(t, 0, bookingPending)
		verification = f.caseWant(t, "verification", bookingHomeCoordinator).entity
		f.expect(t, access, publicationWant{app.CoordinationChanged, verification, []int{bookingHomeCoordinator}})
		f.a.call(t, 404, "POST", "/coordinator/verifications/"+verification+"/approve", nil, f.cookie[bookingCoordinator])
		f.expect(t)
		f.a.call(t, 204, "POST", "/coordinator/verifications/"+verification+"/approve", nil, f.cookie[bookingHomeCoordinator])
		f.disconnected(t, bookingPending)
		f.connect(t, 0, bookingPending)
		f.expect(t, access, publicationWant{app.CoordinationChanged, verification, []int{bookingHomeCoordinator}})
		f.count(t, 2, "SELECT count(*) FROM student_verifications WHERE student_id=$1", fixture(bookingPending))
	})
	t.Run("queued cases recheck current authority and lost coordinator closes", func(t *testing.T) {
		// Coordinator 417 has not reviewed a record in this fixture other than
		// verification above; FK constraints intentionally preserve that assignment.
		// Use coordinator 416's account closure, an existing Identity access flag,
		// to revoke authority without rewriting history or cascading anything.
		id := f.session(t, 201, "POST", "/issues", map[string]string{"description": "Private case"}, bookingOtherStudent)["id"].(string)
		f.exec(t, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(bookingCoordinator))
		f.disconnected(t, bookingCoordinator)
		// Two transactions: dispatch together, but check every event/recipient.
		events := f.pending(t)
		if len(events) != 2 {
			t.Fatal(events)
		}
		if n, err := app.NewDispatcher(f.store, f.bus).RunBatch(f.ctx); err != nil || n != 2 {
			t.Fatal(n, err)
		}
		for _, processed := range f.processed {
			for range events {
				select {
				case <-processed:
				case <-time.After(3 * time.Second):
					t.Fatal("fanout timed out")
				}
			}
		}
		f.expectSockets(t, events, []publicationWant{{app.CoordinationChanged, id, nil}, {app.AccountAccessChanged, "", nil}})
		f.a.call(t, 403, "GET", "/coordinator/issues/"+id, nil, f.cookie[bookingCoordinator])
	})
}

func TestRealtimeDomainMigration(t *testing.T) {
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t)
	f.exec(t, "DELETE FROM realtime_events")
	store := outboxpg.New(f.db)
	// Every new category participates in the same transactional capture boundary.
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(bookingTutor)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tutor_application.changed", "activity.changed", "account.access_changed"} {
		if _, err = tx.Exec(f.ctx, "SELECT enqueue_realtime_event($1,$2)", kind, fixture(bookingTutor)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(f.ctx, "SELECT enqueue_realtime_event('coordination.changed',$1,'issue')", fixture(88080)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Claim(f.ctx, time.Second); err != nil || found {
		t.Fatal("uncommitted capture visible", err)
	}
	if err = tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.count(t, 0, "SELECT count(*) FROM realtime_events")
	f.count(t, 0, "SELECT count(*) FROM users WHERE closed_at IS NOT NULL")
	f.exec(t, `SELECT enqueue_realtime_event('account.access_changed',$1);
	`, fixture(bookingTutor))
	c, ok, err := store.Claim(f.ctx, time.Second)
	if err != nil || !ok || c.EntityID != "" || c.Validate() != nil {
		t.Fatal("account target leaked through claim", c, err)
	}
	target, kind, err := store.Route(f.ctx, c.ID)
	if err != nil || target != fixture(bookingTutor) || kind != "" {
		t.Fatal(target, kind, err)
	}
	if _, err = f.db.Exec(f.ctx, "UPDATE realtime_events SET entity_id=$2 WHERE id=$1", c.ID, fixture(bookingStudent)); err == nil {
		t.Fatal("mutable private target")
	}
	if _, err = f.db.Exec(f.ctx, "UPDATE realtime_events SET resource_kind='issue' WHERE id=$1", c.ID); err == nil {
		t.Fatal("mutable routing kind")
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000018_realtime_domain_events.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := f.db.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(f.ctx, string(down)); err == nil {
		t.Fatal("rollback dropped pending domain work")
	}
	if _, err = conn.Exec(f.ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err = store.Dispatched(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	var before, after string
	const snapshot = `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM tutor_applications a),(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM student_verifications v))::text`
	if err = conn.QueryRow(f.ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(f.ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(f.ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("rollback changed business evidence", err)
	}
	if err = store.Check(f.ctx); err == nil {
		t.Fatal("startup accepted migration 17 without private routing schema")
	}
	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000018_realtime_domain_events.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(f.ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if err = store.Check(f.ctx); err != nil {
		t.Fatal(err)
	}
	// Identical UUIDs in different case tables must not coalesce together.
	f.exec(t, `SELECT enqueue_realtime_event('coordination.changed',$1,'issue'), enqueue_realtime_event('coordination.changed',$1,'flag')`, fixture(88080))
	f.count(t, 2, "SELECT count(*) FROM realtime_events")
}

func TestRealtimeDomainCoordinatorAssignmentLoss(t *testing.T) {
	f := newPublicationFixture(t)
	f.registerIssues()
	f.exec(t, "INSERT INTO faculties(id,name) VALUES($1,'Temporary authority')", fixture(88021))
	// Trusted provisioning fixture; no new administrative workflow is introduced.
	f.exec(t, "INSERT INTO coordinators(user_id,employee_number,faculty_id) VALUES($1,'RT88-TEMP',$2)", fixture(bookingOtherStudent), fixture(88021))
	f.disconnected(t, bookingOtherStudent)
	f.connect(t, 0, bookingOtherStudent)
	f.expect(t, publicationWant{app.AccountAccessChanged, "", []int{bookingOtherStudent}})
	// A queued private record for another faculty is captured in the same
	// transaction as revocation. The active student's old coordinator socket must
	// close, and a freshly authenticated student must not inherit the former scope.
	f.exec(t, `BEGIN;
		INSERT INTO issue_reports(id,reporter_user_id,faculty_id,description) VALUES('00000000-0000-0000-0000-000000088022','00000000-0000-0000-0000-000000000414','00000000-0000-0000-0000-000000000401','Private queued case');
		DELETE FROM coordinators WHERE employee_number='RT88-TEMP'; COMMIT;`)
	f.disconnected(t, bookingOtherStudent)
	f.connect(t, 0, bookingOtherStudent)
	f.expect(t, publicationWant{app.AccountAccessChanged, "", []int{bookingOtherStudent}}, publicationWant{app.CoordinationChanged, fixture(88022), []int{bookingCoordinator}})
	f.a.call(t, 403, "GET", "/coordinator/issues/"+fixture(88022), nil, f.cookie[bookingOtherStudent])
}
