package application

import (
	"context"
	"errors"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type flaggedUsersStub struct {
	calls, limit, offset int
	actor                string
	err                  error
}

func (r *flaggedUsersStub) ListFlaggedUsers(_ context.Context, actor string, limit, offset int) ([]domain.FlaggedUser, error) {
	r.calls++
	r.actor, r.limit, r.offset = actor, limit, offset
	return []domain.FlaggedUser{}, r.err
}

func TestFlaggedUsersAuthorizationAndBounds(t *testing.T) {
	r := &flaggedUsersStub{}
	s := NewFlaggedUsers(r)
	ctx := context.Background()
	const id = "00000000-0000-0000-0000-000000000001"
	for _, p := range []identity.Principal{{}, {ID: id, Student: true}, {ID: id, Student: true, Tutor: true},
		{ID: id, CoordinatorFaculty: id, Suspended: true}, {ID: id, CoordinatorFaculty: id, Closed: true}} {
		if _, err := s.List(ctx, p, 25, 0); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal(p, err)
		}
	}
	actor := identity.Principal{ID: id, CoordinatorFaculty: id}
	for _, v := range [][2]int{{0, 0}, {101, 0}, {1, -1}, {1, 100001}} {
		if _, err := s.List(ctx, actor, v[0], v[1]); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(v, err)
		}
	}
	if r.calls != 0 {
		t.Fatal("invalid or forbidden read reached repository")
	}
	for _, p := range []identity.Principal{actor, {ID: id, CoordinatorFaculty: id, Student: true, Tutor: true}} {
		for _, v := range [][2]int{{1, 0}, {25, 10}, {100, 100000}} {
			got, err := s.List(ctx, p, v[0], v[1])
			if err != nil || got == nil || r.actor != id || r.limit != v[0]+1 || r.offset != v[1] {
				t.Fatal(got, err, r)
			}
		}
	}
	r.err = errors.New("read failed")
	if _, err := s.List(ctx, actor, 25, 0); !errors.Is(err, r.err) {
		t.Fatal(err)
	}
}
