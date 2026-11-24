package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type domainQueries struct {
	owner, coordinator, faculty, target, kind string
	err                                       error
}

func (q *domainQueries) ApplicationAudience(context.Context, string) (string, string, string, error) {
	return q.owner, q.coordinator, q.faculty, q.err
}
func (q *domainQueries) ActivityAudience(context.Context, string) (string, string, error) {
	return q.coordinator, q.faculty, q.err
}
func (q *domainQueries) CaseAudience(context.Context, string, string) (string, string, error) {
	return q.coordinator, q.faculty, q.err
}
func (q *domainQueries) AccountReviewAudience(context.Context, string, string) (string, string, error) {
	return q.coordinator, q.faculty, q.err
}
func (q *domainQueries) Route(context.Context, string) (string, string, error) {
	return q.target, q.kind, q.err
}

func TestDomainRoutingCurrentReadPolicies(t *testing.T) {
	ctx := context.Background()
	q := &domainQueries{owner: rtStudent, coordinator: rtCoord, faculty: rtTutor, target: rtOther, kind: "issue"}
	w := &watchesFake{active: true}
	r := NewRouting(nil, visibilityFake(true), w, DomainRouting{q, q, q, q})
	student := domain.Principal{ID: rtStudent, Student: true, Verification: domain.Approved}
	coordinator := domain.Principal{ID: rtCoord, CoordinatorFaculty: rtTutor}
	for _, tc := range []struct {
		name   string
		kind   Type
		entity string
		p      domain.Principal
		want   bool
	}{
		{"applicant", TutorApplicationChanged, rtOther, student, true},
		{"restricted applicant keeps own history", TutorApplicationChanged, rtOther, domain.Principal{ID: rtStudent, Student: true, Verification: domain.Rejected}, true},
		{"application coordinator", TutorApplicationChanged, rtOther, coordinator, true},
		{"foreign faculty", TutorApplicationChanged, rtOther, domain.Principal{ID: rtCoord, CoordinatorFaculty: rtOther}, false},
		{"case reporter cannot read coordinator case", CoordinationChanged, rtOther, student, false},
		{"case coordinator", CoordinationChanged, rtOther, coordinator, true},
		{"public rating watcher", ActivityChanged, rtTutor, student, true},
		{"activity coordinator", ActivityChanged, rtTutor, coordinator, true},
		{"private account target", AccountAccessChanged, "", domain.Principal{ID: rtOther, Student: true}, true},
		{"coordinator cannot receive another account change", AccountAccessChanged, "", coordinator, false},
		{"suspended target", AccountAccessChanged, "", domain.Principal{ID: rtOther, Student: true, Suspended: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewEvent(tc.kind, tc.entity, time.Now())
			ok, err := r.Authorize(ctx, tc.p, e)
			if err != nil || ok != tc.want {
				t.Fatal(ok, err)
			}
		})
	}
	e, _ := NewEvent(CoordinationChanged, rtOther, time.Now())
	q.faculty = rtOther // candidate selection is not delivery authorization
	if ok, err := r.Authorize(ctx, coordinator, e); err != nil || ok {
		t.Fatal("stale coordinator authorized", ok, err)
	}
	q.target = rtTutor
	if _, err := r.Authorize(ctx, coordinator, e); !errors.Is(err, ErrInvalidEvent) {
		t.Fatal("mismatched case target", err)
	}
	e, _ = NewEvent(ActivityChanged, rtTutor, time.Now())
	w.active = false
	if ok, err := r.Authorize(ctx, student, e); err != nil || ok {
		t.Fatal("expired public interest authorized", ok, err)
	}
	w.active = true
	r.visibility = visibilityFake(false)
	if ok, err := r.Authorize(ctx, student, e); err != nil || ok {
		t.Fatal("hidden profile authorized", ok, err)
	}
	q.err = errors.New("storage unavailable")
	if _, err := r.Recipients(ctx, e); err == nil {
		t.Fatal("lookup failure suppressed")
	}
}
