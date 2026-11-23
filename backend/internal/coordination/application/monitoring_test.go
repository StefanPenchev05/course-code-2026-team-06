package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	feedbackdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application/monitoring"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type monitoringRepoStub struct {
	reads    int
	snapshot MonitoringSnapshot
}

func (r *monitoringRepoStub) Read(_ context.Context, _ string, work func(MonitoringSnapshot) error) error {
	r.reads++
	return work(r.snapshot)
}

const monitoringID = "00000000-0000-0000-0000-000000000123"

func TestMonitoringRejectsInvalidAuthorityBeforeReading(t *testing.T) {
	for _, actor := range []identity.Principal{
		{}, {ID: monitoringID, Student: true}, {ID: monitoringID, Tutor: true},
		{ID: monitoringID, CoordinatorFaculty: monitoringID, Suspended: true},
		{ID: monitoringID, CoordinatorFaculty: monitoringID, Closed: true},
		{ID: "invalid", CoordinatorFaculty: monitoringID},
	} {
		repo := &monitoringRepoStub{}
		s := NewMonitoring(repo)
		_, e1 := s.Tutors(context.Background(), actor, tutoring.MonitoringFilter{})
		_, e2 := s.Tutor(context.Background(), actor, monitoringID, tutoringdomain.Page{Limit: 25})
		_, e3 := s.Schedule(context.Background(), actor, monitoringID, scheduling.Filter{})
		_, e4 := s.Statistics(context.Background(), actor)
		for _, err := range []error{e1, e2, e3, e4} {
			if !errors.Is(err, domain.ErrForbidden) {
				t.Fatal(actor, err)
			}
		}
		if repo.reads != 0 {
			t.Fatal("unauthorized query reached persistence")
		}
	}
}

func TestMonitoringBounds(t *testing.T) {
	actor := identity.Principal{ID: monitoringID, CoordinatorFaculty: monitoringID}
	repo := &monitoringRepoStub{}
	s := NewMonitoring(repo)
	for _, page := range []tutoringdomain.Page{{Limit: 0}, {Limit: 101}, {Limit: 1, Offset: -1}, {Limit: 1, Offset: 100001}} {
		if _, err := s.Tutors(context.Background(), actor, tutoring.MonitoringFilter{Page: page}); !errors.Is(err, ErrMonitoringQuery) {
			t.Fatal(err)
		}
		if _, err := s.Tutor(context.Background(), actor, monitoringID, page); !errors.Is(err, ErrMonitoringQuery) {
			t.Fatal(err)
		}
	}
	from := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, f := range []scheduling.Filter{{Limit: 25}, {From: from, To: from, Limit: 25}, {From: from, To: from.Add(-time.Hour), Limit: 25},
		{From: from, To: from.Add(31*24*time.Hour + time.Nanosecond), Limit: 25}, {From: from, To: from.Add(time.Hour), Limit: 101}} {
		if _, err := s.Schedule(context.Background(), actor, monitoringID, f); !errors.Is(err, ErrMonitoringQuery) {
			t.Fatal(err)
		}
	}
	if repo.reads != 0 {
		t.Fatal("invalid query reached persistence")
	}
}

type monitoringTutorsStub struct {
	tutoring.MonitoringQueries
	faculty string
	limit   int
}

func (r *monitoringTutorsStub) Tutors(_ context.Context, faculty string, f tutoring.MonitoringFilter) ([]tutoring.MonitoredTutor, error) {
	r.faculty, r.limit = faculty, f.Limit
	return []tutoring.MonitoredTutor{{ID: monitoringID}}, nil
}

type monitoringActivityStub struct {
	feedback.MonitoringQueries
	faculty string
	ids     []string
}

func (r *monitoringActivityStub) TutorStatistics(_ context.Context, faculty string, ids []string) (map[string]feedbackdomain.Statistics, error) {
	r.faculty, r.ids = faculty, ids
	return map[string]feedbackdomain.Statistics{monitoringID: {CompletedSessions: 2, HoursTaught: 2.5}}, nil
}

func TestMonitoringUsesResolvedFacultyAndBatchedStatistics(t *testing.T) {
	tutors, activity := &monitoringTutorsStub{}, &monitoringActivityStub{}
	repo := &monitoringRepoStub{snapshot: MonitoringSnapshot{Faculty: "trusted faculty", Tutors: tutors, Activity: activity}}
	s := NewMonitoring(repo)
	// A pending student role must not override independent coordinator authority.
	actor := identity.Principal{ID: monitoringID, CoordinatorFaculty: "stale faculty", Student: true}
	items, err := s.Tutors(context.Background(), actor, tutoring.MonitoringFilter{Page: tutoringdomain.Page{Limit: 1}})
	if err != nil || len(items) != 1 || items[0].CompletedSessions != 2 || items[0].HoursTaught != 2.5 {
		t.Fatal(items, err)
	}
	if repo.reads != 1 || tutors.limit != 2 || tutors.faculty != "trusted faculty" || activity.faculty != "trusted faculty" || len(activity.ids) != 1 {
		t.Fatal(repo, tutors, activity)
	}
}
