package application

import (
	"context"
	"regexp"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// AttendanceSessions is Scheduling's transaction-bound outcome port.
type AttendanceSessions interface {
	LockParticipantSession(ctx context.Context, id, actor string) (scheduling.Session, error)
	LockFacultySession(ctx context.Context, id, faculty string) (scheduling.Session, error)
	CorrectNoShow(context.Context, scheduling.Session, string, string, time.Time) error
}

type AttendanceTx interface {
	Student(context.Context, string) (identity.Principal, error)
	CoordinatorFaculty(context.Context, string) (string, error)
	HomeFaculty(context.Context, string) (string, error)
	Sessions() AttendanceSessions
	Report(ctx context.Context, coordinator, id string, lock bool) (domain.Report, error)
	InsertReport(ctx context.Context, report domain.Report) (domain.Report, error)
	Decide(ctx context.Context, report domain.Report) error
	RecordNoShow(ctx context.Context, report domain.Report, session scheduling.Session, home string) error
	NotifyReport(ctx context.Context, report domain.Report, faculty string) error
	NotifyDecision(ctx context.Context, report domain.Report) error
}

type AttendanceRepository interface {
	Transact(context.Context, func(AttendanceTx) error) error
	Get(ctx context.Context, coordinator, id string) (domain.Report, error)
	List(ctx context.Context, coordinator string, status domain.Status, limit, offset int) ([]domain.Report, error)
}

type AttendanceService struct {
	repo AttendanceRepository
	now  func() time.Time
}

func NewAttendance(repo AttendanceRepository, now func() time.Time) *AttendanceService {
	return &AttendanceService{repo: repo, now: now}
}

var attendanceID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *AttendanceService) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

func (s *AttendanceService) Submit(ctx context.Context, actor identity.Principal, id, description string) (domain.Report, error) {
	if !actor.VerifiedStudent() {
		return domain.Report{}, domain.ErrForbidden
	}
	text, err := domain.Text(description)
	if err != nil || !attendanceID.MatchString(id) {
		return domain.Report{}, domain.ErrInvalid
	}
	var result domain.Report
	err = s.repo.Transact(ctx, func(tx AttendanceTx) error {
		current, err := tx.Student(ctx, actor.ID)
		if err != nil {
			return err
		}
		if !current.VerifiedStudent() {
			return domain.ErrForbidden
		}
		session, err := tx.Sessions().LockParticipantSession(ctx, id, actor.ID)
		if err != nil {
			return err
		}
		now := s.clock()
		absent, err := domain.AbsentParticipant(session, actor.ID, now)
		if err != nil {
			return err
		}
		result, err = tx.InsertReport(ctx, domain.Report{SessionID: session.ID, ReporterID: actor.ID, AbsentID: absent,
			Status: domain.Pending, Description: text, ReportedAt: now})
		if err != nil {
			return err
		}
		return tx.NotifyReport(ctx, result, session.FacultyID)
	})
	if err != nil {
		return domain.Report{}, err
	}
	return result, nil
}

func (s *AttendanceService) Review(ctx context.Context, actor identity.Principal, id string, status domain.Status, note string) (domain.Report, error) {
	if !actor.Coordinator() {
		return domain.Report{}, domain.ErrForbidden
	}
	text, err := domain.Text(note)
	if err != nil || !attendanceID.MatchString(id) || (status != domain.Confirmed && status != domain.Rejected) {
		return domain.Report{}, domain.ErrInvalid
	}
	var result domain.Report
	err = s.repo.Transact(ctx, func(tx AttendanceTx) error {
		hint, err := tx.Report(ctx, actor.ID, id, false)
		if err != nil {
			return err
		}
		if hint.Status != domain.Pending {
			return domain.ErrConflict
		}
		// Account before session before report, matching completion and cancellation.
		var home string
		if status == domain.Confirmed {
			absent, err := tx.Student(ctx, hint.AbsentID)
			if err != nil {
				return err
			}
			home, err = tx.HomeFaculty(ctx, absent.ProgramID)
			if err != nil {
				return err
			}
		}
		faculty, err := tx.CoordinatorFaculty(ctx, actor.ID)
		if err != nil {
			return err
		}
		session, err := tx.Sessions().LockFacultySession(ctx, hint.SessionID, faculty)
		if err != nil {
			return err
		}
		report, err := tx.Report(ctx, actor.ID, id, true)
		if err != nil {
			return err
		}
		if err := report.ValidateDecision(status, session); err != nil {
			return err
		}
		now := s.clock()
		report.Status, report.ReviewedAt, report.ReviewerID, report.Resolution = status, &now, actor.ID, text
		if err := tx.Decide(ctx, report); err != nil {
			return err
		}
		if status == domain.Confirmed {
			if err := tx.Sessions().CorrectNoShow(ctx, session, actor.ID, text, now); err != nil {
				return err
			}
			if err := tx.RecordNoShow(ctx, report, session, home); err != nil {
				return err
			}
		}
		if err := tx.NotifyDecision(ctx, report); err != nil {
			return err
		}
		result = report
		return nil
	})
	if err != nil {
		return domain.Report{}, err
	}
	return result, nil
}

func (s *AttendanceService) Get(ctx context.Context, actor identity.Principal, id string) (domain.Report, error) {
	if !actor.Coordinator() {
		return domain.Report{}, domain.ErrForbidden
	}
	if !attendanceID.MatchString(id) {
		return domain.Report{}, domain.ErrInvalid
	}
	return s.repo.Get(ctx, actor.ID, id)
}

func (s *AttendanceService) List(ctx context.Context, actor identity.Principal, status domain.Status, limit, offset int) ([]domain.Report, error) {
	if !actor.Coordinator() {
		return nil, domain.ErrForbidden
	}
	if !status.Valid() || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, domain.ErrInvalid
	}
	return s.repo.List(ctx, actor.ID, status, limit+1, offset)
}
