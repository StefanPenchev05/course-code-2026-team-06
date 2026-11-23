package integration_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func (f *bookingFixture) testAttendance(t *testing.T) {
	const queue = "/coordinator/attendance-reports"
	complete := func(id string) string { return "/tutors/me/sessions/" + id + "/complete" }
	reports := func(id string) string { return "/sessions/" + id + "/no-show-reports" }
	evidence := map[string]string{"description": "The other participant did not attend."}
	note := map[string]string{"resolution_note": "Reviewed attendance evidence."}
	accept := func(actor int, at time.Duration) string {
		t.Helper()
		id := f.request(t, 201, actor, at, bookingOffer, bookingSlot)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
		return id
	}
	// Use a participant without a flag, and seed their first incident via the API.
	f.setTime(100 * time.Hour)
	cancelled := f.request(t, 201, bookingOtherStudent, 105*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", "/sessions/"+cancelled+"/cancel", testReason, bookingOtherStudent)
	f.count(t, 1, "SELECT count(*) FROM user_violations WHERE session_id=$1 AND kind='LATE_CANCELLATION'", cancelled)
	// Move the learner's home faculty while preserving their original verification.
	f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(bookingOtherStudent), fixture(404))
	id := accept(bookingOtherStudent, 106*time.Hour)
	competing := accept(bookingOtherStudent, 107*time.Hour)
	duplicateIncident := accept(bookingStudent, 108*time.Hour)
	f.session(t, 409, "POST", complete(cancelled), nil, bookingTutor)
	f.session(t, 409, "POST", reports(cancelled), evidence, bookingOtherStudent)
	f.session(t, 409, "POST", complete(id), nil, bookingTutor)
	f.session(t, 422, "POST", reports(id), evidence, bookingOtherStudent)

	f.setTime(107 * time.Hour)
	f.session(t, 404, "POST", complete(id), nil, bookingOtherTutor)
	f.session(t, 404, "POST", complete(id), nil, bookingOtherStudent)
	f.session(t, 404, "POST", reports(id), evidence, bookingOtherTutor)
	f.session(t, 400, "POST", reports(id), map[string]string{"description": "test", "absent_user_id": fixture(bookingOtherStudent)}, bookingOtherStudent)
	f.session(t, 422, "POST", reports(id), map[string]string{"description": " "}, bookingOtherStudent)

	// A failed history write must also roll back completion and block release.
	f.exec(t, `CREATE FUNCTION attendance_test_history_failure() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.to_status IN ('COMPLETED','NO_SHOW') THEN RAISE EXCEPTION 'fixture'; END IF; RETURN NEW; END; $$;
		CREATE TRIGGER attendance_test_history_failure BEFORE INSERT ON session_status_history
		FOR EACH ROW EXECUTE FUNCTION attendance_test_history_failure();`)
	f.session(t, 500, "POST", complete(id), nil, bookingTutor)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='ACCEPTED' AND completed_at IS NULL", id)
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	f.exec(t, "DROP TRIGGER attendance_test_history_failure ON session_status_history")
	defer f.exec(t, "DROP FUNCTION attendance_test_history_failure()")

	// Completion racing a report preserves both actions regardless of who wins.
	codes := make(chan int, 2)
	go func() { codes <- f.a.request("POST", complete(id), origin, nil, f.cookie[bookingTutor]).Code }()
	go func() {
		codes <- f.a.request("POST", reports(id), origin, evidence, f.cookie[bookingOtherStudent]).Code
	}()
	if a, b := <-codes, <-codes; !(a == 200 && b == 201 || a == 201 && b == 200) {
		t.Fatalf("completion/report race: %d, %d", a, b)
	}
	f.session(t, 409, "POST", complete(id), nil, bookingTutor)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND from_status='ACCEPTED' AND to_status='COMPLETED' AND changed_by_user_id=$2", id, fixture(bookingTutor))
	f.session(t, 409, "POST", reports(id), evidence, bookingOtherStudent)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
	var first string
	if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM attendance_reports WHERE session_id=$1", id).Scan(&first); err != nil {
		t.Fatal(err)
	}
	f.session(t, 404, "GET", queue+"/"+first, nil, bookingHomeCoordinator)
	f.session(t, 404, "POST", queue+"/"+first+"/confirm", note, bookingHomeCoordinator)
	f.session(t, 403, "POST", queue+"/"+first+"/confirm", note, bookingTutor)
	f.session(t, 422, "POST", queue+"/"+first+"/reject", map[string]string{"resolution_note": " "}, bookingCoordinator)
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='ATTENDANCE_REPORTED' AND recipient_user_id=$2", id, fixture(bookingCoordinator))
	f.session(t, 200, "GET", queue+"/"+first, nil, bookingCoordinator)
	all := fields(t, f.a.call(t, 200, "GET", queue+"?status=PENDING&limit=1", nil, f.cookie[bookingCoordinator]))["data"].([]any)
	if len(all) != 1 {
		t.Fatal(all)
	}
	wrong := fields(t, f.a.call(t, 200, "GET", queue, nil, f.cookie[bookingHomeCoordinator]))["data"].([]any)
	if len(wrong) != 0 {
		t.Fatal(wrong)
	}

	second := f.session(t, 201, "POST", reports(id), evidence, bookingTutor)["id"].(string)
	f.session(t, 200, "POST", queue+"/"+first+"/reject", note, bookingCoordinator)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='COMPLETED'", id)

	// Reinstall the failure hook for the coordinator's correction.
	f.exec(t, "CREATE TRIGGER attendance_test_history_failure BEFORE INSERT ON session_status_history FOR EACH ROW EXECUTE FUNCTION attendance_test_history_failure()")
	f.session(t, 500, "POST", queue+"/"+second+"/confirm", note, bookingCoordinator)
	f.count(t, 1, "SELECT count(*) FROM attendance_reports WHERE id=$1 AND status='PENDING'", second)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='COMPLETED'", id)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
	f.exec(t, "DROP TRIGGER attendance_test_history_failure ON session_status_history")
	f.session(t, 200, "POST", queue+"/"+second+"/confirm", note, bookingCoordinator)
	f.session(t, 409, "POST", queue+"/"+second+"/confirm", note, bookingCoordinator)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='NO_SHOW' AND completed_at IS NOT NULL", id)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND from_status='COMPLETED' AND to_status='NO_SHOW' AND changed_by_user_id=$2", id, fixture(bookingCoordinator))
	f.count(t, 1, `SELECT count(*) FROM user_violations WHERE session_id=$1 AND kind='NO_SHOW' AND user_id=$2
		AND semester_id=$3 AND attendance_report_id=$4 AND confirmed_by_coordinator_id=$5`,
		id, fixture(bookingOtherStudent), fixture(406), second, fixture(bookingCoordinator))
	f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1 AND semester_id=$2 AND faculty_id=$3", fixture(bookingOtherStudent), fixture(406), fixture(402))
	f.count(t, 1, `SELECT count(*) FROM notifications n JOIN user_semester_flags flag ON flag.id=n.flag_id
		WHERE flag.user_id=$1 AND n.recipient_user_id=$2 AND n.type='USER_FLAGGED'`, fixture(bookingOtherStudent), fixture(bookingHomeCoordinator))
	f.count(t, 1, "SELECT count(*) FROM users WHERE id=$1 AND NOT is_suspended", fixture(bookingOtherStudent))
	f.count(t, 4, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='ATTENDANCE_DECIDED'", id)

	f.setTime(109 * time.Hour)
	claim := f.session(t, 201, "POST", reports(competing), evidence, bookingOtherStudent)["id"].(string)
	opposite := f.session(t, 201, "POST", reports(competing), evidence, bookingTutor)["id"].(string)
	// Confirmation races both completion and rejection. If rejection wins,
	// the independent opposing allegation can still be confirmed.
	done := make(chan int, 1)
	go func() { done <- f.a.request("POST", complete(competing), origin, nil, f.cookie[bookingTutor]).Code }()
	go func() {
		codes <- f.a.request("POST", queue+"/"+claim+"/confirm", origin, note, f.cookie[bookingCoordinator]).Code
	}()
	go func() {
		codes <- f.a.request("POST", queue+"/"+claim+"/reject", origin, note, f.cookie[bookingCoordinator]).Code
	}()
	if a, b := <-codes, <-codes; !(a == 200 && b == 409 || a == 409 && b == 200) {
		t.Fatalf("decisions: %d, %d", a, b)
	}
	if code := <-done; code != 200 && code != 409 {
		t.Fatalf("completion: %d", code)
	}
	f.session(t, 200, "POST", queue+"/"+opposite+"/confirm", note, bookingCoordinator)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='NO_SHOW'", competing)
	f.count(t, 1, "SELECT count(*) FROM user_violations WHERE attendance_report_id=$1 AND user_id=$2", opposite, fixture(bookingOtherStudent))

	// A fabricated late-cancellation incident without authentic history is now
	// rejected by migration 15. A real confirmation remains idempotent.
	f.setTime(109*time.Hour + 168*time.Hour)
	last := f.session(t, 201, "POST", reports(duplicateIncident), evidence, bookingStudent)["id"].(string)
	f.setTime(109*time.Hour + 168*time.Hour + time.Microsecond)
	f.session(t, 422, "POST", reports(duplicateIncident), evidence, bookingTutor)
	_, err := f.db.Exec(f.ctx, `INSERT INTO user_violations (user_id,semester_id,session_id,kind,occurred_at)
		VALUES ($1,$2,$3,'LATE_CANCELLATION','2027-01-05T00:00Z')`, fixture(bookingTutor), fixture(406), duplicateIncident)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23514" {
		t.Fatal("fabricated incident accepted", err)
	}
	f.count(t, 1, "SELECT count(*) FROM attendance_reports WHERE id=$1 AND status='PENDING'", last)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='ACCEPTED'", duplicateIncident)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", duplicateIncident)
	f.session(t, 200, "POST", queue+"/"+last+"/confirm", note, bookingCoordinator)
	f.session(t, 409, "POST", queue+"/"+last+"/confirm", note, bookingCoordinator)
	f.count(t, 1, "SELECT count(*) FROM user_violations WHERE session_id=$1", duplicateIncident)
}
