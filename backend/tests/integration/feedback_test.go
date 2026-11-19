package integration_test

import (
	"strings"
	"testing"
	"time"
)

func (f *bookingFixture) testFeedback(t *testing.T) {
	const privateComment = "Private feedback fixture"
	const activity = "/tutors/me/activity"
	const history = "/tutors/me/feedback"
	feedbackPath := func(id string) string { return "/sessions/" + id + "/feedback" }
	complete := func(id string) string { return "/tutors/me/sessions/" + id + "/complete" }
	f.setTime(145 * time.Hour)
	// The earlier booking concurrency fixture deactivated the second offering.
	f.exec(t, "UPDATE tutor_offerings SET is_active=true,deactivated_at=NULL,deactivation_reason=NULL WHERE id=$1", fixture(bookingOtherOffer))
	book := func(at time.Duration, offer, slot, tutor int) string {
		t.Helper()
		id := f.request(t, 201, bookingStudent, at, offer, slot)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, tutor)
		return id
	}
	first := book(150*time.Hour, bookingOffer, bookingSlot, bookingTutor)
	payload := f.payload(151*time.Hour, bookingOffer, bookingSlot)
	payload["duration_minutes"] = 90
	second := f.session(t, 201, "POST", "/sessions", payload, bookingStudent)["id"].(string)
	f.session(t, 200, "POST", tutorRequestsPath+second+"/accept", testMeeting, bookingTutor)
	other := book(153*time.Hour, bookingOtherOffer, bookingOtherSlot, bookingOtherTutor)
	racing := book(154*time.Hour, bookingOffer, bookingSlot, bookingTutor)
	future := book(180*time.Hour, bookingOffer, bookingSlot, bookingTutor)
	f.request(t, 201, bookingStudent, 181*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 422, "POST", feedbackPath(first), map[string]any{"rating": 5}, bookingStudent)
	f.setTime(155 * time.Hour)
	for _, v := range []struct {
		id    string
		tutor int
	}{{first, bookingTutor}, {second, bookingTutor}, {other, bookingOtherTutor}, {racing, bookingTutor}} {
		f.session(t, 200, "POST", complete(v.id), nil, v.tutor)
	}
	for _, v := range []struct {
		code, actor int
		body        any
	}{
		{401, 0, map[string]any{"rating": 5}},
		{403, bookingPending, map[string]any{"rating": 5}},
		{404, bookingTutor, map[string]any{"rating": 5}},
		{404, bookingOtherStudent, map[string]any{"rating": 5}},
		{422, bookingStudent, map[string]any{"rating": 0}},
		{422, bookingStudent, map[string]any{"rating": 6}},
		{400, bookingStudent, map[string]any{"rating": 3.5}},
		{400, bookingStudent, map[string]any{"rating": 5, "student_id": fixture(bookingStudent)}},
		{422, bookingStudent, map[string]any{"rating": 5, "comment": strings.Repeat("a", 2001)}},
	} {
		f.session(t, v.code, "POST", feedbackPath(first), v.body, v.actor)
	}
	f.session(t, 404, "GET", feedbackPath(first), nil, bookingStudent)

	codes := make(chan int, 2)
	start := make(chan struct{})
	rating := map[string]any{"rating": 5, "comment": privateComment}
	for range 2 {
		go func() {
			<-start
			codes <- f.a.request("POST", feedbackPath(first), origin, rating, f.cookie[bookingStudent]).Code
		}()
	}
	close(start)
	if a, b := <-codes, <-codes; !(a == 201 && b == 409 || a == 409 && b == 201) {
		t.Fatalf("submissions: %d, %d", a, b)
	}
	f.count(t, 1, "SELECT count(*) FROM feedback WHERE session_id=$1", first)
	for _, actor := range []int{bookingStudent, bookingTutor, bookingCoordinator} {
		got := f.session(t, 200, "GET", feedbackPath(first), nil, actor)
		if got["comment"] != privateComment || got["rating"] != float64(5) {
			t.Fatal(got)
		}
	}
	f.session(t, 404, "GET", feedbackPath(first), nil, bookingOtherStudent)
	f.session(t, 404, "GET", feedbackPath(first), nil, bookingHomeCoordinator)
	f.session(t, 201, "POST", feedbackPath(second), map[string]int{"rating": 2}, bookingStudent)
	f.session(t, 403, "GET", history, nil, bookingStudent)
	f.session(t, 403, "GET", activity, nil, bookingStudent)
	unrated := f.session(t, 200, "GET", "/tutors/"+fixture(bookingOtherTutor), nil, bookingStudent)
	if unrated["average_rating"] != nil || unrated["rating_count"] != float64(0) {
		t.Fatal(unrated)
	}
	f.session(t, 201, "POST", feedbackPath(other), map[string]int{"rating": 1}, bookingStudent)

	stats := func(wantCompleted, wantHours, wantAverage, wantCount float64) {
		t.Helper()
		got := f.session(t, 200, "GET", activity+"?limit=1", nil, bookingTutor)
		if got["completed_sessions"] != wantCompleted || got["hours_taught"] != wantHours ||
			got["average_rating"] != wantAverage || got["rating_count"] != wantCount {
			t.Fatal(got)
		}
		up := got["upcoming_sessions"].([]any)
		if len(up) != 1 || up[0].(map[string]any)["id"] != future || up[0].(map[string]any)["student_id"] != nil {
			t.Fatal(up)
		}
	}
	stats(3, 3.5, 3.5, 2)
	review := "/coordinator/tutors/" + fixture(bookingTutor) + "/feedback"
	f.a.call(t, 404, "GET", review, nil, f.cookie[bookingHomeCoordinator])
	f.a.call(t, 403, "GET", review, nil, f.cookie[bookingStudent])
	w := f.a.call(t, 200, "GET", review+"?limit=1", nil, f.cookie[bookingCoordinator])
	data := fields(t, w)
	if data["average_rating"] != 3.5 || data["rating_count"] != float64(2) ||
		len(data["data"].([]any)) != 1 || data["pagination"].(map[string]any)["has_more"] != true {
		t.Fatal(data)
	}
	f.a.call(t, 400, "GET", history+"?limit=101", nil, f.cookie[bookingTutor])

	search := func(wantRating, wantCount float64) {
		t.Helper()
		w := f.a.call(t, 200, "GET", "/tutors?subject_id="+fixture(405), nil, f.cookie[bookingStudent])
		if strings.Contains(w.Body.String(), privateComment) || strings.Contains(w.Body.String(), "\"comment\"") {
			t.Fatal("search exposed private feedback")
		}
		items := fields(t, w)["data"].([]any)
		if len(items) != 2 {
			t.Fatal(items)
		}
		for _, item := range items {
			m := item.(map[string]any)
			switch m["id"] {
			case fixture(bookingTutor):
				if m["average_rating"] != wantRating || m["rating_count"] != wantCount {
					t.Fatal(m)
				}
			case fixture(bookingOtherTutor):
				if m["average_rating"] != float64(1) || m["rating_count"] != float64(1) {
					t.Fatal(m)
				}
			default:
				t.Fatal(m)
			}
		}
	}
	search(3.5, 2)
	evidence := map[string]string{"description": "The tutor did not attend."}
	note := map[string]string{"resolution_note": "Reviewed evidence. Confirmed."}
	report := func(id string) string {
		return f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", evidence, bookingStudent)["id"].(string)
	}
	decision := func(id string) string { return "/coordinator/attendance-reports/" + id + "/confirm" }
	claim := report(first)
	stats(3, 3.5, 3.5, 2) // a pending report does not invalidate feedback
	f.session(t, 200, "POST", decision(claim), note, bookingCoordinator)
	stats(2, 2.5, 2, 1)
	search(2, 1)
	f.count(t, 1, "SELECT count(*) FROM feedback WHERE session_id=$1 AND rating=5 AND comment=$2", first, privateComment)
	f.session(t, 422, "POST", feedbackPath(first), rating, bookingStudent)
	f.session(t, 404, "GET", feedbackPath(first), nil, bookingStudent)
	got := f.session(t, 200, "GET", feedbackPath(first), nil, bookingCoordinator)
	if got["session_status"] != "NO_SHOW" || got["comment"] != privateComment {
		t.Fatal(got)
	}
	w = f.a.call(t, 200, "GET", history, nil, f.cookie[bookingTutor])
	if strings.Contains(w.Body.String(), privateComment) || len(fields(t, w)["data"].([]any)) != 1 {
		t.Fatal(w.Body.String())
	}

	claim = report(racing)
	go func() {
		codes <- f.a.request("POST", feedbackPath(racing), origin, rating, f.cookie[bookingStudent]).Code
	}()
	decided := f.a.request("POST", decision(claim), origin, note, f.cookie[bookingCoordinator])
	if decided.Code != 200 {
		t.Fatal(decided.Code, decided.Body.String())
	}
	code := <-codes
	if code != 201 && code != 422 {
		t.Fatalf("rating/correction race: %d", code)
	}
	want := 0
	if code == 201 {
		want = 1
	}
	f.count(t, want, "SELECT count(*) FROM feedback WHERE session_id=$1", racing)
	stats(1, 1.5, 2, 1)
	search(2, 1)
}
