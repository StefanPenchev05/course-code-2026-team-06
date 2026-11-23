package application

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type OIDCExchange interface {
	ExchangeAndVerify(ctx context.Context, code, verifier, nonce string) (domain.VerifiedIdentity, error)
}

type OIDCCallback struct {
	provider     OIDCExchange
	transactions OIDCTransactions
	issuer       string
}

func NewOIDCCallback(provider OIDCExchange, transactions OIDCTransactions, issuer string) *OIDCCallback {
	return &OIDCCallback{provider: provider, transactions: transactions, issuer: issuer}
}

type OIDCResponse struct{ State, Code, Error, Issuer string }
type OIDCCallbackResult struct {
	Identity domain.VerifiedIdentity
	// Consumed permits cookie clearing only after a matching transaction was consumed.
	Consumed bool
}

func (s *OIDCCallback) Complete(ctx context.Context, response OIDCResponse, binding string) (OIDCCallbackResult, error) {
	var result OIDCCallbackResult
	if (response.Issuer != "" && response.Issuer != s.issuer) || !domain.ValidOIDCToken(response.State) || !domain.ValidOIDCToken(binding) || (response.Code == "") == (response.Error == "") {
		return result, domain.ErrOIDCTransaction
	}
	tx, err := s.transactions.Consume(ctx, response.State, binding)
	if errors.Is(err, domain.ErrOIDCTransaction) {
		return result, domain.ErrOIDCTransaction
	}
	if err != nil {
		return result, domain.ErrOIDCUnavailable
	}
	result.Consumed = true
	if response.Error != "" {
		return result, domain.ErrOIDCAuthentication
	}
	if !domain.ValidOIDCToken(tx.Verifier) || !domain.ValidOIDCToken(tx.Nonce) || !time.Now().Before(tx.ExpiresAt) {
		return result, domain.ErrOIDCAuthentication
	}
	identity, err := s.provider.ExchangeAndVerify(ctx, response.Code, tx.Verifier, tx.Nonce)
	if err != nil {
		if errors.Is(err, domain.ErrOIDCUnavailable) {
			return result, domain.ErrOIDCUnavailable
		}
		return result, domain.ErrOIDCAuthentication
	}
	if identity.Issuer != s.issuer || identity.Subject == "" {
		return result, domain.ErrOIDCAuthentication
	}
	result.Identity = identity
	return result, nil
}
