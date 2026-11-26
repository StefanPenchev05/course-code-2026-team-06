package bootstrap

import "testing"

func TestOIDCSettings(t *testing.T) {
	t.Setenv("OIDC_CLIENT_SECRET", "synthetic-client-secret")
	t.Setenv("OIDC_HTTP_TIMEOUT", "3s")
	t.Setenv("OIDC_POST_LOGIN_PATH", "/sessions")
	t.Setenv("OIDC_ENABLED", "false")
	if cfg, err := oidcSettings("http://localhost:8080"); err != nil || cfg != nil {
		t.Fatal("disabled OIDC altered legacy startup")
	}
	t.Setenv("OIDC_ENABLED", "invalid")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("invalid enable flag")
	}
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "")
	t.Setenv("OIDC_CLIENT_ID", "")
	t.Setenv("OIDC_REDIRECT_URL", "")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("missing settings")
	}
	t.Setenv("OIDC_ISSUER_URL", "http://localhost:8180/realms/tutti-demo")
	t.Setenv("OIDC_CLIENT_ID", "tutti-backend")
	t.Setenv("OIDC_REDIRECT_URL", "http://localhost:8080/api/v1/auth/oidc/callback")
	t.Setenv("OIDC_TRANSACTION_TTL", "5m")
	if _, err := oidcSettings("http://localhost:8080"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OIDC_CLIENT_SECRET", "")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("missing confidential secret accepted")
	}
	t.Setenv("OIDC_CLIENT_SECRET", "synthetic-client-secret")
	t.Setenv("OIDC_TRANSACTION_TTL", "1h")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("invalid TTL")
	}
	t.Setenv("OIDC_TRANSACTION_TTL", "5m")
	t.Setenv("OIDC_HTTP_TIMEOUT", "11s")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("unbounded HTTP timeout")
	}
	t.Setenv("OIDC_HTTP_TIMEOUT", "3s")
	t.Setenv("OIDC_POST_LOGIN_PATH", "//evil.example")
	if _, err := oidcSettings("http://localhost:8080"); err == nil {
		t.Fatal("unsafe post-login redirect")
	}

}
