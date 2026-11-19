package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	feedbackdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application/monitoring"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

var ErrMonitoringQuery = errors.New("invalid monitoring identifier, filter, pagination or time range")

type MonitoringSnapshot struct {
	Faculty    string
	Tutors     tutoring.MonitoringQueries
	Activity   feedback.MonitoringQueries
	Scheduling scheduling.Queries
}

type MonitoringRepository interface {
	Read(context.Context, string, func(MonitoringSnapshot) error) error
}

type TutorSummary struct {
	tutoring.MonitoredTutor
	feedbackdomain.Statistics
}
type TutorDetail struct {
	TutorSummary
	Offerings []tutoringdomain.Offering `json:"offerings"`
}
type FacultyStatistics struct {
	FacultyID string `json:"faculty_id"`
	tutoring.TutorCounts
	feedbackdomain.Statistics
}

type MonitoringService struct{ repo MonitoringRepository }

func NewMonitoring(repo MonitoringRepository) *MonitoringService { return &MonitoringService{repo} }

func monitoringActor(p identity.Principal) error {
	if !p.Coordinator() || !attendanceID.MatchString(p.ID) {
		return domain.ErrForbidden
	}
	return nil
}
func monitoringPage(limit, offset int) bool {
	return limit >= 1 && limit <= 100 && offset >= 0 && offset <= 100000
}

func (s *MonitoringService) Tutors(ctx context.Context, actor identity.Principal, f tutoring.MonitoringFilter) ([]TutorSummary, error) {
	if err := monitoringActor(actor); err != nil {
		return nil, err
	}
	if !monitoringPage(f.Limit, f.Offset) || (f.ProgramID != "" && !attendanceID.MatchString(f.ProgramID)) ||
		(f.SubjectID != "" && !attendanceID.MatchString(f.SubjectID)) {
		return nil, ErrMonitoringQuery
	}
	f.Limit++
	out := []TutorSummary{}
	err := s.repo.Read(ctx, actor.ID, func(q MonitoringSnapshot) error {
		tutors, err := q.Tutors.Tutors(ctx, q.Faculty, f)
		if err != nil {
			return err
		}
		ids := make([]string, len(tutors))
		for i, tutor := range tutors {
			ids[i] = tutor.ID
		}
		stats, err := q.Activity.TutorStatistics(ctx, q.Faculty, ids)
		if err != nil {
			return err
		}
		for _, tutor := range tutors {
			out = append(out, TutorSummary{tutor, stats[tutor.ID]})
		}
		return nil
	})
	return out, err
}

func (s *MonitoringService) Tutor(ctx context.Context, actor identity.Principal, id string, page tutoringdomain.Page) (TutorDetail, error) {
	var out TutorDetail
	if err := monitoringActor(actor); err != nil {
		return out, err
	}
	if !attendanceID.MatchString(id) || !monitoringPage(page.Limit, page.Offset) {
		return out, ErrMonitoringQuery
	}
	id = strings.ToLower(id)
	page.Limit++
	err := s.repo.Read(ctx, actor.ID, func(q MonitoringSnapshot) error {
		tutor, err := q.Tutors.Tutor(ctx, q.Faculty, id)
		if err != nil {
			return err
		}
		stats, err := q.Activity.TutorStatistics(ctx, q.Faculty, []string{id})
		if err != nil {
			return err
		}
		offerings, err := q.Tutors.Offerings(ctx, q.Faculty, id, page)
		if err != nil {
			return err
		}
		if offerings == nil {
			offerings = []tutoringdomain.Offering{}
		}
		out = TutorDetail{TutorSummary{tutor, stats[id]}, offerings}
		return nil
	})
	return out, err
}

func (s *MonitoringService) Schedule(ctx context.Context, actor identity.Principal, id string, f scheduling.Filter) ([]scheduling.ScheduleEntry, error) {
	if err := monitoringActor(actor); err != nil {
		return nil, err
	}
	if !attendanceID.MatchString(id) || !monitoringPage(f.Limit, f.Offset) || f.From.IsZero() ||
		f.To.IsZero() || !f.To.After(f.From) || f.To.Sub(f.From) > 31*24*time.Hour {
		return nil, ErrMonitoringQuery
	}
	f.Limit++
	id = strings.ToLower(id)
	out := []scheduling.ScheduleEntry{}
	err := s.repo.Read(ctx, actor.ID, func(q MonitoringSnapshot) error {
		if _, err := q.Tutors.Tutor(ctx, q.Faculty, id); err != nil {
			return err
		}
		items, err := q.Scheduling.Schedule(ctx, q.Faculty, id, f)
		if items != nil {
			out = items
		}
		return err
	})
	return out, err
}

func (s *MonitoringService) Statistics(ctx context.Context, actor identity.Principal) (FacultyStatistics, error) {
	var out FacultyStatistics
	if err := monitoringActor(actor); err != nil {
		return out, err
	}
	err := s.repo.Read(ctx, actor.ID, func(q MonitoringSnapshot) error {
		counts, err := q.Tutors.Counts(ctx, q.Faculty)
		if err != nil {
			return err
		}
		stats, err := q.Activity.FacultyStatistics(ctx, q.Faculty)
		if err != nil {
			return err
		}
		out = FacultyStatistics{q.Faculty, counts, stats}
		return nil
	})
	return out, err
}
