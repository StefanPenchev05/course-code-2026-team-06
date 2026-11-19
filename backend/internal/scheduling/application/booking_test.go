package application

import (
	"context"
	"errors"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type fakeBookingTx struct {
	BookingTransaction
	offerTutor string
	offerErr   error
	member     bool
	people     map[string]identity.Principal
	session    domain.Session
	events     []Event
	locks      []string
}

func (f *fakeBookingTx) Catalog() catalog.AcademicCatalog     { return f }
func (f *fakeBookingTx) Offerings() tutoring.BookingOfferings { return f }
func (f *fakeBookingTx) ResolveTutor(context.Context, string) (string, error) {
	return f.offerTutor, nil
}
func (f *fakeBookingTx) LockApprovedOffering(context.Context, string, string) (tutoring.BookingOffer, error) {
	return tutoring.BookingOffer{ID: testRule, TutorID: f.offerTutor, SubjectID: testSemester}, f.offerErr
}
func (f *fakeBookingTx) SubjectBelongsToProgram(context.Context, string, string) (bool, error) {
	return f.member, nil
}
func (f *fakeBookingTx) GetProgramFaculty(context.Context, string) (string, error) {
	return testRule, nil
}
func (f *fakeBookingTx) Student(_ context.Context, id string) (identity.Principal, error) {
	f.locks = append(f.locks, id)
	return f.people[id], nil
}
func (f *fakeBookingTx) ParticipantSession(_ context.Context, id, actor string, tutorOnly, _ bool) (domain.Session, error) {
	if f.session.ID != id || (f.session.TutorID != actor && (tutorOnly || f.session.StudentID != actor)) {
		return domain.Session{}, domain.ErrNotFound
	}
	return f.session, nil
}
func (f *fakeBookingTx) DueRequests(_ context.Context, now time.Time, limit int) ([]domain.Session, error) {
	if limit != ExpirationBatchSize {
		return nil, errors.New("unexpected expiration batch size")
	}
	if f.session.Status == domain.Requested && !now.Before(f.session.ExpiresAt) {
		return []domain.Session{f.session}, nil
	}
	return nil, nil
}
func (f *fakeBookingTx) UpdateSession(_ context.Context, s domain.Session, previous domain.Status) error {
	if f.session.Status != previous {
		return domain.ErrBookingState
	}
	f.session = s
	return nil
}
func (f *fakeBookingTx) RecordEvent(_ context.Context, e Event) error {
	f.events = append(f.events, e)
	return nil
}

type fakeBookings struct {
	BookingRepository
	tx *fakeBookingTx
}

func (f fakeBookings) TransactBooking(_ context.Context, work func(BookingTransaction) error) error {
	return work(f.tx)
}

func TestBookingEligibilityUsesLockedAccounts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	learner, tutor := testActor(), testActor()
	learner.ID, learner.Tutor = testRule, false
	learner.ProgramID, tutor.ProgramID = testSemester, testSemester
	tx := &fakeBookingTx{offerTutor: learner.ID, people: map[string]identity.Principal{learner.ID: learner, tutor.ID: tutor}}
	s := NewBooking(fakeBookings{tx: tx}, func() time.Time { return now })
	req := domain.Request{OfferingID: testRule, SlotID: testSemester, StartsAt: now.Add(5 * time.Hour), Minutes: 60,
		Mode: domain.Online, Purpose: "HOMEWORK", Description: "Practice"}
	if _, err := s.Request(ctx, learner, req); !errors.Is(err, domain.ErrBookingInvalid) || len(tx.locks) != 0 {
		t.Fatalf("self-booking was not rejected: %v", err)
	}
	tx.offerTutor = tutor.ID
	tx.offerErr = tutordomain.ErrNotFound
	if _, err := s.Request(ctx, learner, req); !errors.Is(err, domain.ErrBookingInvalid) {
		t.Fatal(err)
	}
	if len(tx.locks) != 2 || tx.locks[0] != tutor.ID || tx.locks[1] != learner.ID {
		t.Fatalf("incorrect lock order: %v", tx.locks)
	}
	tx.offerErr = nil
	if _, err := s.Request(ctx, learner, req); !errors.Is(err, domain.ErrBookingInvalid) {
		t.Fatal(err)
	}

	tx.session = domain.Session{ID: testRule, StudentID: learner.ID, TutorID: tutor.ID, Status: domain.Requested, ExpiresAt: now.Add(time.Hour)}
	if _, err := s.Accept(ctx, learner, testRule, domain.Meeting{Link: "https://example.edu"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	unrelated := tutor
	unrelated.ID = testSemester
	if _, err := s.Decline(ctx, unrelated, testRule, "Unavailable"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Cancel(ctx, unrelated, testRule, "Unavailable"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestExpireDueRequestsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tx := &fakeBookingTx{session: domain.Session{ID: testRule, StudentID: testTutor, TutorID: testSemester, Status: domain.Requested, ExpiresAt: now}}
	s := NewBooking(fakeBookings{tx: tx}, func() time.Time { return now })
	if n, err := s.ExpireDueRequests(ctx, now.Add(-time.Microsecond)); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := s.ExpireDueRequests(ctx, now); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := s.ExpireDueRequests(ctx, now); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
	if len(tx.events) != 1 || tx.events[0].Actor != "" || tx.events[0].To != domain.Expired || len(tx.events[0].Recipients) != 2 {
		t.Fatal(tx.events)
	}
}
