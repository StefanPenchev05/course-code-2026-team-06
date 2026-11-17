package application

import (
	"context"
	"errors"
	"testing"
	"time"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

const testTutor = "00000000-0000-0000-0000-000000000001"
const testRule = "00000000-0000-0000-0000-000000000002"
const testSemester = "00000000-0000-0000-0000-000000000003"

type access struct{}

func (access) CanManageAvailability(p identity.Principal) bool { return p.CanTutor() }
func (access) CanViewAvailability(context.Context, identity.Principal, string) (bool, error) {
	return true, nil
}

type fixtureRepo struct {
	Repository
	current identity.Principal
	tx      Transaction
	writes  int
}

func (r *fixtureRepo) InTransaction(_ context.Context, _ string, work func(Transaction, identity.Principal) error) error {
	r.writes++
	return work(r.tx, r.current)
}

type fixtureTx struct {
	Transaction
	owner     string
	semester  domain.Semester
	insertErr error
	saved     bool
	rule      domain.Rule
}

func (tx *fixtureTx) Rule(_ context.Context, tutor, _ string) (domain.Rule, error) {
	if tutor != tx.owner {
		return domain.Rule{}, domain.ErrNotFound
	}
	return tx.rule, nil
}

func (tx *fixtureTx) Semester(context.Context, string) (domain.Semester, error) {
	return tx.semester, nil
}
func (tx *fixtureTx) CreateRule(context.Context, string, domain.Rule) (string, error) {
	tx.saved = true
	return testRule, nil
}
func (tx *fixtureTx) CreateRuleSlots(context.Context, string, string, []domain.Window) error {
	return tx.insertErr
}

func testActor() identity.Principal {
	return identity.Principal{ID: testTutor, Student: true, Tutor: true, Verification: identity.Approved}
}

func sampleRule() domain.Rule {
	return domain.Rule{SemesterID: testSemester, Weekday: 1, Start: "14:00", End: "17:00", Zone: "UTC", Mode: domain.Online}
}

func TestWriteChecksBothSessionAndLockedIdentity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	r := &fixtureRepo{current: testActor()}
	s := New(r, access{}, func() time.Time { return now })
	p := testActor()
	p.Tutor = false
	if _, err := s.CreateRule(ctx, p, sampleRule()); !errors.Is(err, domain.ErrForbidden) || r.writes != 0 {
		t.Fatal(err)
	}
	r.current.Suspended = true
	if _, err := s.CreateRule(ctx, testActor(), sampleRule()); !errors.Is(err, domain.ErrForbidden) || r.writes != 1 {
		t.Fatal(err)
	}
}

func TestRuleScopeAndConflicts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	tx := &fixtureTx{owner: "another tutor", rule: sampleRule(),
		semester: domain.Semester{StartsAt: now, EndsAt: now.AddDate(0, 1, 0)}, insertErr: domain.ErrOverlap}
	r := &fixtureRepo{current: testActor(), tx: tx}
	s := New(r, access{}, func() time.Time { return now })
	end := "18:00"
	if _, err := s.UpdateRule(ctx, testActor(), testRule, RulePatch{End: &end}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.CreateRule(ctx, testActor(), sampleRule()); !errors.Is(err, domain.ErrOverlap) || !tx.saved {
		t.Fatal("slot insertion errors must reach the transaction boundary", err)
	}
	w := domain.Window{StartsAt: now.Add(time.Hour), EndsAt: now, Mode: domain.Online}
	if _, err := s.CreateSlot(ctx, testActor(), w); !errors.Is(err, domain.ErrInvalid) || r.writes != 2 {
		t.Fatal(err)
	}
	w = domain.Window{StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour), Mode: domain.Online}
	if _, err := s.CreateSlot(ctx, testActor(), w); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
}
