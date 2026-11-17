// Package application implements identity workflows using persistence ports.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type Repository interface {
	Register(context.Context, domain.StudentDetails, string) (string, error)
	Credentials(context.Context, string) (id, hash string, err error)
	Principal(context.Context, string) (domain.Principal, error)
	ListPending(context.Context, string, int, int) ([]domain.Verification, error)
	Verification(context.Context, string, string) (domain.Verification, error)
	Review(context.Context, string, string, domain.Status, string) error
	Resubmit(context.Context, string, domain.StudentDetails) (string, error)
	SaveActivation(context.Context, string, []byte, time.Duration) (string, error)
	ConsumeActivation(context.Context, []byte, string) error
}

type Sessions interface {
	Create(ctx context.Context, key, userID string, ttl time.Duration) error
	Lookup(ctx context.Context, key string) (string, error)
	Revoke(ctx context.Context, key string) error
}

type Passwords interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (bool, error)
}

type ActivationMailer interface {
	SendActivation(ctx context.Context, email, token string, ttl time.Duration) error
}

type Config struct {
	Domains       []string
	SessionTTL    time.Duration
	ActivationTTL time.Duration
}

type Service struct {
	repo      Repository
	sessions  Sessions
	passwords Passwords
	config    Config
	domains   map[string]bool
	dummyHash string
}

func New(ctx context.Context, repo Repository, sessions Sessions, passwords Passwords, config Config) (*Service, error) {
	if config.SessionTTL < time.Second || config.ActivationTTL < time.Second {
		return nil, fmt.Errorf("%w: positive identity expiration required", domain.ErrInvalid)
	}
	domains := make(map[string]bool)
	for _, value := range config.Domains {
		value = strings.ToLower(strings.TrimSpace(value))
		if !validEmail("check@" + value) {
			return nil, fmt.Errorf("%w: invalid configured university domain", domain.ErrInvalid)
		}
		domains[value] = true
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("%w: configure university email domains", domain.ErrInvalid)
	}
	dummyHash, err := passwords.Hash(ctx, randomToken())
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, sessions: sessions, passwords: passwords, config: config, domains: domains, dummyHash: dummyHash}, nil
}

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validEmail(value string) bool {
	addr, err := mail.ParseAddress(value)
	_, host, found := strings.Cut(value, "@")
	return err == nil && addr.Address == value && found && strings.Contains(host, ".") && len(value) <= 254
}

func normalizedEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validatePassword(password string) error {
	if len(password) < 12 || len(password) > 256 {
		return fmt.Errorf("%w: password must be 12 to 256 bytes", domain.ErrInvalid)
	}
	return nil
}

func (s *Service) validateStudent(details domain.StudentDetails) (domain.StudentDetails, error) {
	details.Name = strings.TrimSpace(details.Name)
	details.Email = normalizedEmail(details.Email)
	details.StudentNumber = strings.TrimSpace(details.StudentNumber)
	_, host, _ := strings.Cut(details.Email, "@")
	if !validEmail(details.Email) || !s.domains[host] {
		return details, fmt.Errorf("%w: use an allowed university email address", domain.ErrInvalid)
	}
	if details.Name == "" || len(details.Name) > 200 || details.StudentNumber == "" ||
		len(details.StudentNumber) > 64 || !uuid.MatchString(details.ProgramID) ||
		strings.ContainsRune(details.Name+details.StudentNumber, 0) {
		return details, fmt.Errorf("%w: provide a name, student ID and valid program ID", domain.ErrInvalid)
	}
	return details, nil
}

func (s *Service) Register(ctx context.Context, details domain.StudentDetails, password string) (string, error) {
	clean, err := s.validateStudent(details)
	if err != nil {
		return "", err
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}
	hash, err := s.passwords.Hash(ctx, password)
	if err != nil {
		return "", err
	}
	return s.repo.Register(ctx, clean, hash)
}

func (s *Service) Login(ctx context.Context, email, password string) (domain.Principal, string, error) {
	email = normalizedEmail(email)
	if !validEmail(email) || len(password) > 256 {
		return domain.Principal{}, "", domain.ErrUnauthenticated
	}
	id, hash, err := s.repo.Credentials(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Principal{}, "", err
	}
	if hash == "" {
		hash = s.dummyHash
		id = ""
	}
	ok, err := s.passwords.Verify(ctx, password, hash)
	if err != nil {
		return domain.Principal{}, "", err
	}
	if !ok || id == "" {
		return domain.Principal{}, "", domain.ErrUnauthenticated
	}
	principal, err := s.repo.Principal(ctx, id)
	if err != nil {
		return domain.Principal{}, "", err
	}
	if !principal.Active() || (!principal.Student && !principal.Coordinator()) {
		return domain.Principal{}, "", domain.ErrForbidden
	}
	token := randomToken()
	if err := s.sessions.Create(ctx, sessionKey(token), id, s.config.SessionTTL); err != nil {
		return domain.Principal{}, "", err
	}
	return principal, token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	if !validToken(token) {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	id, err := s.sessions.Lookup(ctx, sessionKey(token))
	if err != nil {
		return domain.Principal{}, err
	}
	p, err := s.repo.Principal(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Principal{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.Principal{}, err
	}
	if !p.Active() || (!p.Student && !p.Coordinator()) {
		return domain.Principal{}, domain.ErrForbidden
	}
	return p, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return domain.ErrUnauthenticated
	}
	return s.sessions.Revoke(ctx, sessionKey(token))
}

func (s *Service) ListPending(ctx context.Context, actor domain.Principal, limit, offset int) ([]domain.Verification, error) {
	if !actor.Coordinator() {
		return nil, domain.ErrForbidden
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, domain.ErrInvalid
	}
	return s.repo.ListPending(ctx, actor.ID, limit, offset)
}

func (s *Service) Verification(ctx context.Context, actor domain.Principal, id string) (domain.Verification, error) {
	if !actor.Coordinator() {
		return domain.Verification{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Verification{}, domain.ErrInvalid
	}
	return s.repo.Verification(ctx, actor.ID, id)
}

func (s *Service) Review(ctx context.Context, actor domain.Principal, id string, status domain.Status, reason string) error {
	if !actor.Coordinator() {
		return domain.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if !uuid.MatchString(id) || (status != domain.Approved && status != domain.Rejected) {
		return domain.ErrInvalid
	}
	if status == domain.Rejected && (reason == "" || len(reason) > 2000 || strings.ContainsRune(reason, 0)) {
		return fmt.Errorf("%w: a rejection reason of 1 to 2000 bytes is required", domain.ErrInvalid)
	}
	return s.repo.Review(ctx, actor.ID, id, status, reason)
}

func (s *Service) Resubmit(ctx context.Context, actor domain.Principal, details domain.StudentDetails) (string, error) {
	if !actor.Active() || !actor.Student {
		return "", domain.ErrForbidden
	}
	if actor.Verification != domain.Rejected {
		return "", domain.ErrConflict
	}
	clean, err := s.validateStudent(details)
	if err != nil {
		return "", err
	}
	return s.repo.Resubmit(ctx, actor.ID, clean)
}

// IssueActivation is for trusted provisioning code, never a public route.
func (s *Service) IssueActivation(ctx context.Context, id string, mailer ActivationMailer) error {
	if !uuid.MatchString(id) || mailer == nil {
		return domain.ErrInvalid
	}
	token := randomToken()
	digest := sha256.Sum256([]byte(token))
	email, err := s.repo.SaveActivation(ctx, id, digest[:], s.config.ActivationTTL)
	if err != nil {
		return err
	}
	return mailer.SendActivation(ctx, email, token, s.config.ActivationTTL)
}

func (s *Service) Activate(ctx context.Context, token, password string) error {
	if !validToken(token) {
		return domain.ErrUnauthenticated
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.passwords.Hash(ctx, password)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	return s.repo.ConsumeActivation(ctx, digest[:], hash)
}

func (s *Service) SessionTTL() time.Duration { return s.config.SessionTTL }

func randomToken() string {
	buf := make([]byte, 32)
	rand.Read(buf) // Go 1.24 terminates the process if the system RNG fails.
	return base64.RawURLEncoding.EncodeToString(buf)
}

func validToken(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(raw) == 32
}

func sessionKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
