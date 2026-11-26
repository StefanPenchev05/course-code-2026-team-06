package application

import (
	"context"

	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
)

// RoutingMetadata never leaves the server; particularly, account targets are
// deliberately absent from Event, Claim and the Redis fanout message.
type RoutingMetadata interface {
	Route(context.Context, string) (target, kind string, err error)
}
type DomainRouting struct {
	Metadata     RoutingMetadata
	Tutoring     tutoring.RealtimeQueries
	Coordination coordination.RealtimeQueries
	Identity     identity.RealtimeQueries
}

func (r *Routing) caseAudience(ctx context.Context, e Event) (string, string, error) {
	target, kind, err := r.domains.Metadata.Route(ctx, e.ID)
	if err != nil {
		return "", "", err
	}
	if target != e.EntityID {
		return "", "", ErrInvalidEvent
	}
	if kind == "verification" || kind == "suspension" {
		return r.domains.Identity.AccountReviewAudience(ctx, kind, target)
	}
	return r.domains.Coordination.CaseAudience(ctx, kind, target)
}

func (r *Routing) domainRecipients(ctx context.Context, e Event) ([]string, error) {
	if r.domains == nil {
		return nil, ErrUnknownType
	}
	switch e.Type {
	case TutorApplicationChanged:
		owner, coordinator, _, err := r.domains.Tutoring.ApplicationAudience(ctx, e.EntityID)
		return nonempty(owner, coordinator), err
	case CoordinationChanged:
		coordinator, _, err := r.caseAudience(ctx, e)
		return nonempty(coordinator), err
	case ActivityChanged:
		coordinator, _, err := r.domains.Tutoring.ActivityAudience(ctx, e.EntityID)
		if err != nil {
			return nil, err
		}
		users, err := r.watches.Users(ctx, e.EntityID)
		return nonempty(append(users, e.EntityID, coordinator)...), err
	case AccountAccessChanged:
		target, _, err := r.domains.Metadata.Route(ctx, e.ID)
		return nonempty(target), err
	default:
		return nil, ErrUnknownType
	}
}

func (r *Routing) authorizeDomain(ctx context.Context, p domain.Principal, e Event) (bool, error) {
	if r.domains == nil {
		return false, ErrUnknownType
	}
	switch e.Type {
	case TutorApplicationChanged:
		owner, coordinator, faculty, err := r.domains.Tutoring.ApplicationAudience(ctx, e.EntityID)
		return (p.Student && p.ID == owner) || (p.ID == coordinator && p.CanAccessFaculty(faculty)), err
	case CoordinationChanged:
		coordinator, faculty, err := r.caseAudience(ctx, e)
		return p.ID == coordinator && p.CanAccessFaculty(faculty), err
	case ActivityChanged:
		coordinator, faculty, err := r.domains.Tutoring.ActivityAudience(ctx, e.EntityID)
		if err != nil {
			return false, err
		}
		if p.ID == coordinator && p.CanAccessFaculty(faculty) {
			return true, nil
		}
		// Existing profile watches express interest in both availability and
		// public rating changes, with the same current visibility check.
		e.Type = AvailabilityChanged
		return r.Authorize(ctx, p, e)
	case AccountAccessChanged:
		target, _, err := r.domains.Metadata.Route(ctx, e.ID)
		return p.ID == target, err
	default:
		return false, ErrUnknownType
	}
}

func nonempty(ids ...string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}
