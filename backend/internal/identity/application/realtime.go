package application

import "context"

type RealtimeQueries interface {
	AccountReviewAudience(context.Context, string, string) (coordinator, faculty string, err error)
}
