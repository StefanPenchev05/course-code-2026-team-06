package domain

import "fmt"

// ErrOIDCIdentityUnlinked identifies an unknown exact (issuer, subject) pair.
// It contains no external identifier, account ID or personal information.
var ErrOIDCIdentityUnlinked = fmt.Errorf("%w: OIDC identity is not linked", ErrNotFound)
