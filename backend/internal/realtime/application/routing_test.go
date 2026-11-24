package application

import (
	"context"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
)

const rtStudent = "00000000-0000-0000-0000-000000000001"
const rtTutor = "00000000-0000-0000-0000-000000000002"
const rtCoord = "00000000-0000-0000-0000-000000000003"
const rtOther = "00000000-0000-0000-0000-000000000004"

type routingQueries struct{ audience scheduling.BookingAudience }

func (q *routingQueries) BookingAudience(context.Context, string) (scheduling.BookingAudience, error) {
	return q.audience, nil
}

type visibilityFake bool

func (v visibilityFake) CanViewAvailability(context.Context, domain.Principal, string) (bool, error) {
	return bool(v), nil
}

type watchesFake struct{ active bool }

func (w *watchesFake) Register(context.Context, string, string) (time.Time, error) {
	w.active = true
	return time.Now().Add(time.Minute), nil
}
func (w *watchesFake) Users(context.Context, string) ([]string, error) {
	if w.active {
		return []string{rtStudent}, nil
	}
	return nil, nil
}
func (w *watchesFake) Contains(context.Context, string, string) (bool, error) { return w.active, nil }

type publisherFake struct{ ids []string }

func (p *publisherFake) Publish(_ context.Context, e Event, _ []string, _ Authorize) error {
	p.ids = append(p.ids, e.ID)
	return nil
}

func TestRoutingCurrentAuthorityAndDedupe(t *testing.T) {
	ctx := context.Background()
	q := &routingQueries{audience: scheduling.BookingAudience{StudentID: rtStudent, TutorID: rtTutor, FacultyID: rtTutor, CoordinatorID: rtCoord}}
	w := &watchesFake{}
	r := NewRouting(q, visibilityFake(true), w)
	e, _ := NewEvent(BookingChanged, rtOther, time.Now())
	for _, tc := range []struct {
		p    domain.Principal
		want bool
	}{
		{domain.Principal{ID: rtStudent, Student: true, Verification: domain.Approved}, true},
		{domain.Principal{ID: rtStudent, Student: true, Verification: domain.Pending}, false},
		{domain.Principal{ID: rtStudent, Student: true, Verification: domain.Approved, Suspended: true}, false},
		{domain.Principal{ID: rtOther, Student: true, Verification: domain.Approved}, false},
		{domain.Principal{ID: rtCoord, CoordinatorFaculty: rtTutor}, true},
		{domain.Principal{ID: rtCoord, CoordinatorFaculty: rtOther}, false},
	} {
		ok, err := r.Authorize(ctx, tc.p, e)
		if err != nil || ok != tc.want {
			t.Fatalf("routing: %v %v want %v", ok, err, tc.want)
		}
	}
	student := domain.Principal{ID: rtStudent, Student: true, Verification: domain.Approved}
	availability, _ := NewEvent(AvailabilityChanged, rtTutor, time.Now())
	if ok, _ := r.Authorize(ctx, student, availability); ok {
		t.Fatal("unregistered watcher authorized")
	}
	if _, err := r.Watch(ctx, student, rtTutor); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.Authorize(ctx, student, availability); !ok {
		t.Fatal("visible watcher denied")
	}
	r.visibility = visibilityFake(false)
	if ok, _ := r.Authorize(ctx, student, availability); ok {
		t.Fatal("stale visibility authorized")
	}
	if _, err := r.Watch(ctx, student, rtTutor); err == nil {
		t.Fatal("invisible tutor watch registered")
	}
	r.visibility = visibilityFake(true)
	store := &fakeOutbox{claim: Claim{Event: e}}
	publisher := &publisherFake{}
	receiver := NewReceiver(store, r, publisher)
	if err := receiver.Receive(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Receive(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if len(publisher.ids) != 1 {
		t.Fatal("duplicate event enqueued twice")
	}
}
