package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
)

var ErrOIDCTransaction = errors.New("invalid or expired authentication transaction")
var ErrOIDCUnavailable = errors.New("OIDC authentication unavailable")

// OIDCTransaction is pre-authentication correlation, never an application session.
// Do not log or expose it in an HTTP response.
type OIDCTransaction struct {
	BindingHash string    `json:"binding_hash"`
	Verifier    string    `json:"verifier"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func OIDCHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func ValidOIDCToken(value string) bool {
	if len(value) != 43 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32
}
