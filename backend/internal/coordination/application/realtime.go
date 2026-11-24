package application

import "context"

// CaseAudience follows the stored authority of the particular case/audit record.
type RealtimeQueries interface {
	CaseAudience(context.Context, string, string) (coordinator, faculty string, err error)
}
