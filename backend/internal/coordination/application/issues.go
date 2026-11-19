package application

import (
	"context"
	"strings"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type IssueTx interface {
	Student(context.Context, string) (identity.Principal, error)
	HomeFaculty(ctx context.Context, program string) (string, error)
	ParticipantSession(ctx context.Context, id, actor string) (scheduling.Session, error)
	InsertIssue(context.Context, domain.Issue) (domain.Issue, error)
	RecordIssueCreation(context.Context, domain.Issue) error
}

type IssueRepository interface {
	Transact(context.Context, func(IssueTx) error) error
}

type IssueService struct{ repo IssueRepository }

func NewIssues(repo IssueRepository) *IssueService { return &IssueService{repo: repo} }

func (s *IssueService) Submit(ctx context.Context, actor identity.Principal, input domain.IssueInput) (domain.Issue, error) {
	if !actor.VerifiedStudent() || !attendanceID.MatchString(actor.ID) {
		return domain.Issue{}, domain.ErrForbidden
	}
	text, err := domain.Text(input.Description)
	if err != nil {
		return domain.Issue{}, err
	}
	var sessionID *string
	if input.SessionID != nil {
		if !attendanceID.MatchString(*input.SessionID) {
			return domain.Issue{}, domain.ErrInvalid
		}
		id := strings.ToLower(*input.SessionID)
		sessionID = &id
	}
	var result domain.Issue
	err = s.repo.Transact(ctx, func(tx IssueTx) error {
		// Match the existing account -> session lock order. Read the current
		// academic membership under Identity's lock, not from a stale cookie.
		current, err := tx.Student(ctx, actor.ID)
		if err != nil {
			return err
		}
		if !current.VerifiedStudent() || current.ID != actor.ID {
			return domain.ErrForbidden
		}
		var faculty string
		if sessionID != nil {
			session, err := tx.ParticipantSession(ctx, *sessionID, actor.ID)
			if err != nil {
				return err
			}
			if session.ID != *sessionID || session.StudentID == session.TutorID ||
				(actor.ID != session.StudentID && actor.ID != session.TutorID) {
				return domain.ErrIssueNotFound
			}
			// Session faculty and participants are immutable in Scheduling.
			// General issues have no attendance-specific status/time window.
			faculty = session.FacultyID
		} else {
			faculty, err = tx.HomeFaculty(ctx, current.ProgramID)
			if err != nil {
				return err
			}
		}
		if !attendanceID.MatchString(faculty) {
			return domain.ErrForbidden
		}
		result, err = tx.InsertIssue(ctx, domain.Issue{ReporterID: current.ID, FacultyID: faculty,
			SessionID: sessionID, Description: text, Status: domain.IssueOpen})
		if err != nil {
			return err
		}
		return tx.RecordIssueCreation(ctx, result)
	})
	if err != nil {
		return domain.Issue{}, err
	}
	return result, nil
}
