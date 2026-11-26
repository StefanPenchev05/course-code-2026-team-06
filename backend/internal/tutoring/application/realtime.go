package application

import "context"

// RealtimeQueries exposes current HTTP read audiences, never application evidence.
type RealtimeQueries interface {
	ApplicationAudience(context.Context, string) (owner, coordinator, faculty string, err error)
	ActivityAudience(context.Context, string) (coordinator, faculty string, err error)
}
