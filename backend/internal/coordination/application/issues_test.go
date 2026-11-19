package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

const issueActor = "00000000-0000-0000-0000-000000000001"
const issueSession = "00000000-0000-0000-0000-00000000000a"
const issueHome = "00000000-0000-0000-0000-000000000002"
const issueFaculty = "00000000-0000-0000-0000-000000000003"

func issueStudent() identity.Principal {
	return identity.Principal{ID: issueActor, Student: true, Verification: identity.Approved, ProgramID: "current-program"}
}

type issueStore struct {
	current                                                              identity.Principal
	session                                                              scheduling.Session
	faculty                                                              string
	studentErr, sessionErr, facultyErr, insertErr, historyErr, commitErr error
	calls                                                                []string
	inserted, history                                                    domain.Issue
	committed                                                            bool
	report, resolved                                                     domain.Issue
	coordinator                                                          string
	coordinatorErr, lockErr, resolveErr, resolutionHistoryErr, readErr   error
}

func (f *issueStore) Transact(ctx context.Context, work func(IssueTx) error) error {
	if err := work(f); err != nil {
		return err
	}
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = true
	return nil
}
func (f *issueStore) Student(context.Context, string) (identity.Principal, error) {
	f.calls = append(f.calls, "student")
	return f.current, f.studentErr
}
func (f *issueStore) HomeFaculty(_ context.Context, program string) (string, error) {
	f.calls = append(f.calls, "faculty:"+program)
	return f.faculty, f.facultyErr
}
func (f *issueStore) ParticipantSession(_ context.Context, id, actor string) (scheduling.Session, error) {
	f.calls = append(f.calls, "session:"+id+":"+actor)
	return f.session, f.sessionErr
}
func (f *issueStore) InsertIssue(_ context.Context, issue domain.Issue) (domain.Issue, error) {
	f.calls = append(f.calls, "insert")
	issue.ID, issue.CreatedAt = "created-issue", time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	f.inserted = issue
	return issue, f.insertErr
}
func (f *issueStore) RecordIssueCreation(_ context.Context, issue domain.Issue) error {
	f.calls = append(f.calls, "history")
	f.history = issue
	return f.historyErr
}
func newIssueStore() *issueStore {
	return &issueStore{current: issueStudent(), faculty: issueHome,
		session: scheduling.Session{ID: issueSession, StudentID: issueActor, TutorID: issueHome, FacultyID: issueFaculty}}
}

func TestIssueRolesAndStalePermissions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*identity.Principal)
		allowed bool
	}{
		{"student", func(*identity.Principal) {}, true},
		{"tutor and student", func(p *identity.Principal) { p.Tutor = true }, true},
		{"student and coordinator", func(p *identity.Principal) { p.CoordinatorFaculty = issueHome }, true},
		{"unauthenticated", func(p *identity.Principal) { p.ID = "" }, false},
		{"malformed actor", func(p *identity.Principal) { p.ID = "actor" }, false},
		{"coordinator only", func(p *identity.Principal) { p.Student = false; p.CoordinatorFaculty = issueHome }, false},
		{"pending", func(p *identity.Principal) { p.Verification = identity.Pending }, false},
		{"rejected", func(p *identity.Principal) { p.Verification = identity.Rejected }, false},
		{"suspended", func(p *identity.Principal) { p.Suspended = true }, false},
		{"closed", func(p *identity.Principal) { p.Closed = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, store := issueStudent(), newIssueStore()
			tc.change(&p)
			store.current = p
			_, err := NewIssues(store).Submit(context.Background(), p, domain.IssueInput{Description: "Issue"})
			if tc.allowed {
				if err != nil || !store.committed {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, domain.ErrForbidden) || len(store.calls) != 0 {
				t.Fatal(err, store.calls)
			}
			// A previously eligible principal must not override current state.
			_, err = NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue"})
			if !errors.Is(err, domain.ErrForbidden) || store.inserted.ID != "" {
				t.Fatal("stale permission accepted", err)
			}
		})
	}
}

func TestIssueRoutingAndAudit(t *testing.T) {
	for _, linked := range []bool{false, true} {
		store := newIssueStore()
		input := domain.IssueInput{Description: "  A problem.\n"}
		wantFaculty := issueHome
		wantCalls := []string{"student", "faculty:current-program", "insert", "history"}
		if linked {
			id := strings.ToUpper(issueSession)
			input.SessionID = &id
			wantFaculty = issueFaculty
			wantCalls = []string{"student", "session:" + issueSession + ":" + issueActor, "insert", "history"}
		}
		stale := issueStudent()
		stale.ProgramID = "stale-program"
		got, err := NewIssues(store).Submit(context.Background(), stale, input)
		if err != nil || got.ReporterID != issueActor || got.FacultyID != wantFaculty || got.Description != "A problem." || got.Status != domain.IssueOpen || got.CreatedAt.IsZero() || !reflect.DeepEqual(got, store.history) || !reflect.DeepEqual(store.calls, wantCalls) {
			t.Fatalf("got=%+v calls=%v err=%v", got, store.calls, err)
		}
	}
	for _, status := range []scheduling.Status{scheduling.Requested, scheduling.Accepted, scheduling.Completed, scheduling.NoShow, scheduling.Cancelled, scheduling.Declined, scheduling.Expired} {
		store := newIssueStore()
		store.session.Status = status
		store.session.StudentID, store.session.TutorID = issueHome, issueActor
		id := issueSession
		if _, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue", SessionID: &id}); err != nil {
			t.Fatal("tutor participation or session status wrongly restricted", status, err)
		}
	}
}

func TestIssueInvalidInputsAndSessionOwnership(t *testing.T) {
	for _, id := range []string{"", "not-a-uuid", " "} {
		store := newIssueStore()
		if _, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue", SessionID: &id}); !errors.Is(err, domain.ErrInvalid) || len(store.calls) != 0 {
			t.Fatal(err)
		}
	}
	for _, description := range []string{"", " \t", strings.Repeat("x", 2001)} {
		store := newIssueStore()
		if _, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: description}); !errors.Is(err, domain.ErrInvalid) || len(store.calls) != 0 {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*issueStore){
		func(f *issueStore) { f.session.StudentID = "someone-else" },
		func(f *issueStore) { f.session.TutorID = issueActor },
		func(f *issueStore) { f.session.ID = issueHome },
		func(f *issueStore) { f.sessionErr = domain.ErrIssueNotFound },
	} {
		store := newIssueStore()
		change(store)
		id := issueSession
		got, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue", SessionID: &id})
		if !errors.Is(err, domain.ErrIssueNotFound) || got.ID != "" || store.inserted.ID != "" {
			t.Fatal(got, err)
		}
	}
	store := newIssueStore()
	store.faculty = ""
	if _, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("missing faculty accepted", err)
	}
}

func TestIssueTransactionFailure(t *testing.T) {
	failure := errors.New("storage unavailable")
	for _, change := range []func(*issueStore){
		func(f *issueStore) { f.studentErr = failure }, func(f *issueStore) { f.facultyErr = failure },
		func(f *issueStore) { f.insertErr = failure }, func(f *issueStore) { f.historyErr = failure },
		func(f *issueStore) { f.commitErr = failure },
	} {
		store := newIssueStore()
		change(store)
		got, err := NewIssues(store).Submit(context.Background(), issueStudent(), domain.IssueInput{Description: "Issue"})
		if !errors.Is(err, failure) || got.ID != "" || store.committed {
			t.Fatal("failed transaction reported success", got, err)
		}
	}
}
