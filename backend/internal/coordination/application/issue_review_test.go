package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

func (f *issueStore) Get(_ context.Context, coordinator, id string) (domain.Issue, error) {
	f.calls = append(f.calls, "get:"+coordinator+":"+id)
	return f.report, f.readErr
}
func (f *issueStore) List(_ context.Context, coordinator string, status domain.IssueStatus, limit, offset int) ([]domain.Issue, error) {
	f.calls = append(f.calls, fmt.Sprintf("list:%s:%s:%d:%d", coordinator, status, limit, offset))
	return []domain.Issue{f.report}, f.readErr
}
func (f *issueStore) CoordinatorFaculty(_ context.Context, actor string) (string, error) {
	f.calls = append(f.calls, "coordinator:"+actor)
	return f.coordinator, f.coordinatorErr
}
func (f *issueStore) LockIssue(_ context.Context, id, faculty string) (domain.Issue, error) {
	f.calls = append(f.calls, "lock:"+id+":"+faculty)
	return f.report, f.lockErr
}
func (f *issueStore) ResolveIssue(_ context.Context, r domain.Issue, actor, note string) (domain.Issue, error) {
	f.calls = append(f.calls, "resolve")
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	r.Status, r.ResolverID, r.Resolution, r.ResolvedAt = domain.IssueResolved, actor, note, &now
	f.resolved = r
	return r, f.resolveErr
}
func (f *issueStore) RecordIssueResolution(_ context.Context, r domain.Issue) error {
	f.calls = append(f.calls, "resolution-history")
	f.history = r
	return f.resolutionHistoryErr
}
func issueCoordinator() identity.Principal {
	return identity.Principal{ID: issueActor, CoordinatorFaculty: issueHome}
}
func issueReviewStore() *issueStore {
	return &issueStore{coordinator: issueFaculty, report: domain.Issue{ID: issueSession, FacultyID: issueFaculty, Status: domain.IssueOpen,
		Description: "Original evidence", ReporterID: issueHome, CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}}
}

func TestIssueCoordinatorRoles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actor   identity.Principal
		allowed bool
	}{
		{"missing", identity.Principal{}, false}, {"student", issueStudent(), false},
		{"tutor", identity.Principal{ID: issueActor, Student: true, Tutor: true, Verification: identity.Approved}, false},
		{"coordinator", issueCoordinator(), true},
		{"multi-role pending student", identity.Principal{ID: issueActor, Student: true, Verification: identity.Pending, CoordinatorFaculty: issueHome}, true},
		{"suspended", identity.Principal{ID: issueActor, CoordinatorFaculty: issueHome, Suspended: true}, false},
		{"closed", identity.Principal{ID: issueActor, CoordinatorFaculty: issueHome, Closed: true}, false},
		{"invalid ID", identity.Principal{ID: "bad", CoordinatorFaculty: issueHome}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, operation := range []string{"get", "list", "resolve"} {
				store := issueReviewStore()
				service := NewIssues(store)
				var err error
				switch operation {
				case "get":
					_, err = service.Get(context.Background(), tc.actor, issueSession)
				case "list":
					_, err = service.List(context.Background(), tc.actor, domain.IssueOpen, 25, 0)
				case "resolve":
					_, err = service.Resolve(context.Background(), tc.actor, issueSession, "Reviewed")
				}
				if tc.allowed && err != nil {
					t.Fatal(operation, err)
				}
				if !tc.allowed && (!errors.Is(err, domain.ErrForbidden) || len(store.calls) != 0) {
					t.Fatal(operation, err, store.calls)
				}
			}
		})
	}
}

func TestIssueResolutionUsesLockedAuthorityAndPreservesEvidence(t *testing.T) {
	store := issueReviewStore()
	got, err := NewIssues(store).Resolve(context.Background(), issueCoordinator(), strings.ToUpper(issueSession), "  Resolved — 学生\n")
	wantCalls := []string{"coordinator:" + issueActor, "lock:" + issueSession + ":" + issueFaculty, "resolve", "resolution-history"}
	if err != nil || !store.committed || got.Status != domain.IssueResolved || got.ResolverID != issueActor || got.Resolution != "Resolved — 学生" || got.ResolvedAt == nil || !reflect.DeepEqual(got, store.history) || !reflect.DeepEqual(store.calls, wantCalls) {
		t.Fatal(got, err, store.calls)
	}
	if got.Description != store.report.Description || got.ReporterID != store.report.ReporterID || got.FacultyID != store.report.FacultyID || got.CreatedAt != store.report.CreatedAt {
		t.Fatal("original evidence changed")
	}
}

func TestIssueResolutionValidationAndFailures(t *testing.T) {
	for _, note := range []string{"", " \t\n\u3000", strings.Repeat("é", 1001), "invalid\xff", "nul\x00"} {
		store := issueReviewStore()
		if _, err := NewIssues(store).Resolve(context.Background(), issueCoordinator(), issueSession, note); !errors.Is(err, domain.ErrInvalid) || len(store.calls) != 0 {
			t.Fatal("invalid note", err)
		}
	}
	for _, id := range []string{"", "malformed"} {
		store := issueReviewStore()
		s := NewIssues(store)
		if _, err := s.Resolve(context.Background(), issueCoordinator(), id, "Note"); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
		if _, err := s.Get(context.Background(), issueCoordinator(), id); !errors.Is(err, domain.ErrInvalid) || len(store.calls) != 0 {
			t.Fatal(err)
		}
	}
	failure := errors.New("fixture failure")
	for _, tc := range []struct {
		name   string
		change func(*issueStore)
		want   error
	}{
		{"authority removed", func(f *issueStore) { f.coordinatorErr = domain.ErrForbidden }, domain.ErrForbidden},
		{"authority missing", func(f *issueStore) { f.coordinator = "" }, domain.ErrForbidden},
		{"wrong faculty", func(f *issueStore) { f.report.FacultyID = issueHome }, domain.ErrIssueNotFound},
		{"wrong report", func(f *issueStore) { f.report.ID = issueHome }, domain.ErrIssueNotFound},
		{"missing report", func(f *issueStore) { f.lockErr = domain.ErrIssueNotFound }, domain.ErrIssueNotFound},
		{"resolved", func(f *issueStore) { f.report.Status = domain.IssueResolved }, domain.ErrIssueConflict},
		{"unknown state", func(f *issueStore) { f.report.Status = "OTHER" }, domain.ErrIssueConflict},
		{"failed update", func(f *issueStore) { f.resolveErr = failure }, failure},
		{"failed history", func(f *issueStore) { f.resolutionHistoryErr = failure }, failure},
		{"failed commit", func(f *issueStore) { f.commitErr = failure }, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := issueReviewStore()
			tc.change(store)
			got, err := NewIssues(store).Resolve(context.Background(), issueCoordinator(), issueSession, "Note")
			if !errors.Is(err, tc.want) || got.ID != "" || store.committed {
				t.Fatal(got, err)
			}
			if tc.name == "failed update" && store.history.ID != "" {
				t.Fatal("audit written after failed update")
			}
		})
	}
}

func TestIssueInboxFiltersAndReadIdentity(t *testing.T) {
	for _, tc := range []struct {
		status        domain.IssueStatus
		limit, offset int
		valid         bool
	}{
		{domain.IssueOpen, 1, 0, true}, {domain.IssueResolved, 100, 100000, true},
		{"", 25, 0, false}, {"CLOSED", 25, 0, false}, {domain.IssueOpen, 0, 0, false},
		{domain.IssueOpen, 101, 0, false}, {domain.IssueOpen, 25, -1, false}, {domain.IssueOpen, 25, 100001, false},
	} {
		store := issueReviewStore()
		_, err := NewIssues(store).List(context.Background(), issueCoordinator(), tc.status, tc.limit, tc.offset)
		if !tc.valid {
			if !errors.Is(err, domain.ErrInvalid) || len(store.calls) != 0 {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(store.calls, []string{fmt.Sprintf("list:%s:%s:%d:%d", issueActor, tc.status, tc.limit+1, tc.offset)}) {
			t.Fatal(err, store.calls)
		}
	}
	store := issueReviewStore()
	_, err := NewIssues(store).Get(context.Background(), issueCoordinator(), strings.ToUpper(issueSession))
	if err != nil || !reflect.DeepEqual(store.calls, []string{"get:" + issueActor + ":" + issueSession}) {
		t.Fatal(err, store.calls)
	}
	store.readErr = errors.New("unavailable")
	if _, err = NewIssues(store).Get(context.Background(), issueCoordinator(), issueSession); !errors.Is(err, store.readErr) {
		t.Fatal(err)
	}
	if _, err = NewIssues(store).List(context.Background(), issueCoordinator(), domain.IssueOpen, 25, 0); !errors.Is(err, store.readErr) {
		t.Fatal(err)
	}
}
