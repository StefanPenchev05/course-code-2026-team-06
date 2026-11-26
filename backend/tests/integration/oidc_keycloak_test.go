package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	"golang.org/x/oauth2"
)

func TestOIDCKeycloakBrowser(t *testing.T) {
	if os.Getenv("TUTTI_TEST_KEYCLOAK") != "1" {
		t.Skip("opt-in disposable test_oidc_keycloak.py runner")
	}
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t, 3*time.Minute)
	ctx := f.ctx
	const issuer = "http://localhost:8180/realms/tutti-demo"
	admin := oauth2.Config{ClientID: "admin-cli", Endpoint: oauth2.Endpoint{TokenURL: "http://localhost:8180/realms/master/protocol/openid-connect/token"}}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	token, err := admin.PasswordCredentialsToken(context.WithValue(ctx, oauth2.HTTPClient, httpClient), os.Getenv("KEYCLOAK_ADMIN_USERNAME"), os.Getenv("KEYCLOAK_ADMIN_PASSWORD"))
	if err != nil {
		t.Fatal("disposable Keycloak admin authentication failed")
	}
	adminRequest := func(method, path string, body any, want int) *http.Response {
		t.Helper()
		var data io.Reader
		if body != nil {
			wire, err := json.Marshal(body)
			if err != nil {
				t.Fatal("admin request encoding failed")
			}
			data = bytes.NewReader(wire)
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://localhost:8180/admin/realms/tutti-demo"+path, data)
		if err != nil {
			t.Fatal("admin request invalid")
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		response, err := httpClient.Do(req)
		if err != nil {
			t.Fatal("admin request unavailable")
		}
		if response.StatusCode != want {
			response.Body.Close()
			t.Fatal("unexpected admin response status", response.StatusCode)
		}
		return response
	}
	address := freeRealtimeAddress(t)
	origin := "http://localhost:" + strings.Split(address, ":")[1]
	response := adminRequest("GET", "/clients?clientId=tutti-backend", nil, 200)
	var clients []map[string]any
	err = json.NewDecoder(response.Body).Decode(&clients)
	response.Body.Close()
	if err != nil || len(clients) != 1 {
		t.Fatal("missing demo client")
	}
	client := clients[0]
	client["redirectUris"] = []string{origin + "/api/v1/auth/oidc/callback"}
	client["webOrigins"] = []string{origin}
	client["secret"] = os.Getenv("TUTTI_OIDC_CLIENT_SECRET")
	response = adminRequest("PUT", "/clients/"+client["id"].(string), client, 204)
	response.Body.Close()
	provisioner, err := application.NewTrustedOIDCProvisioner(identitypg.New(f.db), issuer)
	if err != nil {
		t.Fatal(err)
	}
	password := oauth2.GenerateVerifier()
	type browserUser struct {
		Scenario   string   `json:"scenario"`
		Username   string   `json:"username"`
		ID         string   `json:"id"`
		Status     int      `json:"status"`
		Verified   bool     `json:"verified"`
		Roles      []string `json:"roles"`
		Semesters  int      `json:"semesters"`
		OldSession string   `json:"oldSession,omitempty"`
Faculty string `json:"faculty,omitempty"`
	}
	users := []browserUser{
		{Scenario: "approved tutor rotation and logout", ID: fixture(bookingTutor), Status: 200, Verified: true, Roles: []string{"student", "tutor"}, Semesters: 200, OldSession: f.cookie[bookingTutor].Value},
		{Scenario: "pending student remains restricted", ID: fixture(bookingPending), Status: 200, Roles: []string{"student"}, Semesters: 403},
		{Scenario: "coordinator retains database faculty authority", ID: fixture(bookingCoordinator), Status: 200, Roles: []string{"coordinator"}, Semesters: 403},
		{Scenario: "unknown identity rejected", Status: 401},
		{Scenario: "suspended tutor rejected", ID: fixture(bookingOtherTutor), Status: 403},
	}
	for i := range users {
		user := &users[i]
		user.Username = fmt.Sprintf("synthetic-%s", oauth2.GenerateVerifier()[:12])
		response := adminRequest("POST", "/users", map[string]any{"username": user.Username, "enabled": true, "email": user.Username + "@tutti.example", "emailVerified": true, "firstName": "Synthetic", "lastName": "OIDC", "credentials": []map[string]any{{"type": "password", "value": password, "temporary": false}}}, 201)
		location := response.Header.Get("Location")
		response.Body.Close()
		parts := strings.Split(location, "/")
		subject := parts[len(parts)-1]
		if subject == "" {
			t.Fatal("missing synthetic identity subject")
		}
		if user.ID != "" {
			if err := provisioner.LinkOIDCIdentity(ctx, user.ID, domain.VerifiedIdentity{Issuer: issuer, Subject: subject}); err != nil {
				t.Fatal("trusted synthetic binding failed")
			}
		}
	}
	f.exec(t,"INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2)",fixture(bookingPending),fixture(401))
	f.exec(t, "UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='OIDC fixture' WHERE id=$1", fixture(bookingOtherTutor), fixture(bookingCoordinator))
	for key, value := range map[string]string{"DATABASE_URL": os.Getenv("TUTTI_TEST_DATABASE_URL"), "REDIS_URL": os.Getenv("TUTTI_TEST_REDIS_URL"), "HTTP_ADDR": address, "PUBLIC_ORIGIN": origin, "UNIVERSITY_EMAIL_DOMAINS": "uni.example", "OIDC_ENABLED": "true", "OIDC_ISSUER_URL": issuer, "OIDC_CLIENT_ID": "tutti-backend", "OIDC_CLIENT_SECRET": os.Getenv("TUTTI_OIDC_CLIENT_SECRET"), "OIDC_REDIRECT_URL": origin + "/api/v1/auth/oidc/callback", "OIDC_POST_LOGIN_PATH": "/api/v1/auth/me", "OIDC_HTTP_TIMEOUT": "3s", "OIDC_TRANSACTION_TTL": "5m", "SESSION_TTL": "1h", "ARGON2_MEMORY_KIB": "19456", "ARGON2_ITERATIONS": "2", "ARGON2_THREADS": "1", "REALTIME_NAMESPACE": "oidc-browser-" + oauth2.GenerateVerifier()} {
		t.Setenv(key, value)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- bootstrap.Run(runCtx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("API shutdown failed")
			}
		case <-time.After(15 * time.Second):
			t.Error("API shutdown timed out")
		}
	})
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := httpClient.Get(origin + "/api/v1/auth/me")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 401 {
				ready = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond) // Poll the actual readiness condition, not a fixed startup delay.
	}
	if !ready {
		t.Fatal("OIDC-enabled production bootstrap did not become ready")
	}
	fixtureJSON, err := json.Marshal(map[string]any{"origin": origin, "password": password, "users": users})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "node", "keycloak_browser.mjs")
	command.Env = append(os.Environ(), "TUTTI_KEYCLOAK_BROWSER_FIXTURE="+string(fixtureJSON))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Keycloak browser smoke failed (sanitized output): %s", output)
	}
	t.Log(strings.TrimSpace(string(output)))
	f.count(t, 4, "SELECT count(*) FROM oidc_identities")
}
