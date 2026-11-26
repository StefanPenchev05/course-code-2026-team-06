package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/interfaces/cli"
	cataloghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/interfaces/http"
)

func TestTutorSemesterAPI(t *testing.T) {
	// The existing disposable DB helper lets this test reuse booking role fixtures
	// without affecting any other integration fixture or the development database.
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t)
	now := f.start
	repo := catalogpg.New(f.db)
	var logs bytes.Buffer
	h := cataloghttp.New(catalog.New(repo), slog.New(slog.NewTextHandler(&logs, nil)))
	calendar := catalog.NewCalendar(catalogpg.NewCalendar(f.db, catalogpg.CalendarDependencies{}), func() time.Time { return now })
	h.RegisterSemesters(f.a.handler.(*http.ServeMux), calendar, f.auth.RequireAuthentication)
	for _, v := range []struct {
		id         int
		start, end time.Time
	}{
		{94001, now.Add(-24 * time.Hour), now.Add(24 * time.Hour)},
		{94002, now.Add(-48 * time.Hour), now},
		{94003, now.Add(-72 * time.Hour), now.Add(-24 * time.Hour)},
		{94004, now.Add(48 * time.Hour), now.Add(72 * time.Hour)},
		{94005, now.Add(48 * time.Hour), now.Add(72 * time.Hour)},
	} {
		f.exec(t, "INSERT INTO semesters(id,name,starts_at,ends_at) VALUES($1,'Calendar fixture',$2,$3)", fixture(v.id), v.start, v.end)
	}
	f.a.call(t, 401, "GET", "/semesters", nil, nil)
	for _, actor := range []int{bookingStudent, bookingPending, bookingCoordinator} {
		f.a.call(t, 403, "GET", "/semesters", nil, f.cookie[actor])
	}
	before := calendarSnapshot(t, f.ctx, f.db)
	for _, actor := range []int{bookingTutor, bookingOtherTutor} {
		w := f.a.call(t, 200, "GET", "/semesters", nil, f.cookie[actor])
		var response struct {
			Data []domain.Semester `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range response.Data {
			ids = append(ids, s.ID)
		}
		if !reflect.DeepEqual(ids, []string{fixture(94001), fixture(406), fixture(94004), fixture(94005)}) {
			t.Fatal("ended semesters or unstable ordering", ids)
		}
		body := fields(t, w)
		if len(body) != 1 || body["data"] == nil {
			t.Fatal("HTTP exposed fields outside the data envelope", body)
		}
		for _, raw := range body["data"].([]any) {
			row := raw.(map[string]any)
			if len(row) != 4 || row["id"] == nil || row["name"] == nil || !strings.HasSuffix(row["starts_at"].(string), "Z") || !strings.HasSuffix(row["ends_at"].(string), "Z") {
				t.Fatal("unexpected semester fields", row)
			}
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("authenticated response cached")
		}
	}
	if !reflect.DeepEqual(before, calendarSnapshot(t, f.ctx, f.db)) {
		t.Fatal("semester reads changed persisted data")
	}
	f.a.call(t, 400, "GET", "/semesters?now=2030-01-01", nil, f.cookie[bookingTutor])
	f.a.call(t, 405, "POST", "/semesters", map[string]string{"name": "untrusted"}, f.cookie[bookingTutor])
	f.a.call(t, 404, "GET", "/public/semesters", nil, nil)
	// A tutor profile does not grant access without current student verification.
	// Provision only this synthetic pending actor; never rewrite a final review.
	f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2)", fixture(bookingPending), fixture(401))
	f.exec(t, "INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Unverified tutor fixture',now())", fixture(bookingPending))
	f.a.call(t, 403, "GET", "/semesters", nil, f.cookie[bookingPending])
	f.exec(t, "UPDATE tutor_profiles SET is_active=false WHERE student_id=$1", fixture(bookingTutor))
	f.a.call(t, 403, "GET", "/semesters", nil, f.cookie[bookingTutor])
	f.exec(t, "UPDATE tutor_profiles SET is_active=true WHERE student_id=$1", fixture(bookingTutor))
	f.exec(t, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(bookingTutor))
	f.a.call(t, 403, "GET", "/semesters", nil, f.cookie[bookingTutor])
	f.exec(t, "UPDATE users SET closed_at=NULL,is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Semester fixture' WHERE id=$1", fixture(bookingTutor), fixture(bookingCoordinator))
	f.a.call(t, 403, "GET", "/semesters", nil, f.cookie[bookingTutor])
	f.exec(t, "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1", fixture(bookingTutor))
	f.a.call(t, 204, "POST", "/auth/logout", nil, f.cookie[bookingOtherTutor])
	f.a.call(t, 401, "GET", "/semesters", nil, f.cookie[bookingOtherTutor])
	now = now.AddDate(10, 0, 0)
	w := f.a.call(t, 200, "GET", "/semesters", nil, f.cookie[bookingTutor])
	if strings.TrimSpace(w.Body.String()) != `{"data":[]}` {
		t.Fatal("empty calendar is not an array", w.Body.String())
	}
	// The same service must still return the full operator receipt to the CLI,
	// even when every semester is past and the tutor HTTP collection is empty.
	var out, stderr bytes.Buffer
	if code := cli.Run(f.ctx, []string{"list"}, &out, &stderr, func(context.Context) (*catalog.CalendarService, func(), error) {
		return calendar, func() {}, nil
	}); code != 0 {
		t.Fatalf("CLI list failed: %d %s", code, stderr.String())
	}
	var receipt catalog.CalendarReceipt
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || len(receipt.Semesters) != 6 || !receipt.ReadOnly || receipt.Operation != "list" || receipt.Outcome != "inspected" {
		t.Fatalf("CLI lost full calendar or operator metadata: %+v %v", receipt, err)
	}
	// Fail the actual calendar query in this helper-created disposable database.
	// Restore the table even if a response assertion fails. Identity still uses its
	// real PostgreSQL/Redis session, so this exercises Catalog's error mapping.
	f.exec(t, "ALTER TABLE semesters RENAME TO unavailable_semesters")
	defer f.exec(t, "ALTER TABLE unavailable_semesters RENAME TO semesters")
	w = f.a.call(t, 500, "GET", "/semesters", nil, f.cookie[bookingTutor])
	if strings.TrimSpace(w.Body.String()) != `{"error":"internal server error"}` {
		t.Fatal("calendar failure leaked persistence details", w.Body.String())
	}
	if !strings.Contains(logs.String(), "catalog request failed") || strings.Contains(logs.String(), "unavailable_semesters") {
		t.Fatal("calendar failure was hidden or leaked SQL details", logs.String())
	}
}
