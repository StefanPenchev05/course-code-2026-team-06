package bootstrap

import (
	"fmt"
	"os"
	"strconv"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
)

func oidcSettings(origin string) (*application.OIDCConfig, error) {
	enabled, err := strconv.ParseBool(env("OIDC_ENABLED", "false"))
	if err != nil {
		return nil, fmt.Errorf("OIDC_ENABLED must be a boolean")
	}
	if !enabled {
		return nil, nil
	}
	ttl, err := duration("OIDC_TRANSACTION_TTL", "5m")
	if err != nil {
		return nil, err
	}
	cfg := &application.OIDCConfig{PublicOrigin: origin, Issuer: os.Getenv("OIDC_ISSUER_URL"), ClientID: os.Getenv("OIDC_CLIENT_ID"), RedirectURL: os.Getenv("OIDC_REDIRECT_URL"), TransactionTTL: ttl}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
