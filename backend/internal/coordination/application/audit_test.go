package application

import (
	"context"
	"errors"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type auditStub struct {
	calls int
	limit int
}

func (r *auditStub) ListAudit(_ context.Context, _, _, _ string, limit, _ int) ([]domain.AuditEntry, error) {
	r.calls++
	r.limit = limit
	return nil, nil
}
func TestAuditAuthorizationAndBounds(t *testing.T) {
	r := &auditStub{}
	s := NewAudit(r)
	ctx := context.Background()
	const id = "00000000-0000-0000-0000-000000000001"
	for _, p := range []identity.Principal{{}, {ID: id, Student: true}, {ID: id, CoordinatorFaculty: id, Suspended: true}, {ID: id, CoordinatorFaculty: id, Closed: true}} {
		if _, err := s.List(ctx, p, id, "SUSPENSION", 25, 0); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal(err)
		}
	}
	actor := identity.Principal{ID: id, CoordinatorFaculty: id}
	for _, v := range []struct {
		user, kind    string
		limit, offset int
	}{{"bad", "FLAG", 1, 0}, {id, "", 1, 0}, {id, "ALL", 1, 0}, {id, "FLAG", 0, 0}, {id, "FLAG", 101, 0}, {id, "FLAG", 1, -1}, {id, "FLAG", 1, 100001}} {
		if _, err := s.List(ctx, actor, v.user, v.kind, v.limit, v.offset); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(v, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("unauthorized/invalid read reached repository")
	}
	for _, kind := range []string{"SUSPENSION", "VIOLATION", "FLAG"} {
		if _, err := s.List(ctx, actor, id, kind, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	if r.calls != 3 || r.limit != 101 {
		t.Fatal(r)
	}
}
