package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Cost struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
}

func (c Cost) valid() bool {
	return c.Memory >= 19*1024 && c.Memory <= 128*1024 &&
		c.Time >= 2 && c.Time <= 6 && c.Threads >= 1 && c.Threads <= 4
}

type Passwords struct {
	cost  Cost
	slots chan struct{}
}

func NewPasswords(cost Cost, concurrency int) (*Passwords, error) {
	if !cost.valid() || concurrency < 1 || concurrency > 8 {
		return nil, fmt.Errorf("invalid Argon2id cost or concurrency")
	}
	return &Passwords{cost: cost, slots: make(chan struct{}, concurrency)}, nil
}

func (p *Passwords) derive(ctx context.Context, password string, salt []byte, cost Cost) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.slots <- struct{}{}:
	}
	defer func() { <-p.slots }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return argon2.IDKey([]byte(password), salt, cost.Time, cost.Memory, cost.Threads, 32), nil
}

func (p *Passwords) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := p.derive(ctx, password, salt, p.cost)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		p.cost.Memory, p.cost.Time, p.cost.Threads, base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func (p *Passwords) Verify(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, nil
	}
	var cost Cost
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &cost.Memory, &cost.Time, &cost.Threads); err != nil ||
		n != 3 || !cost.valid() || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", cost.Memory, cost.Time, cost.Threads) {
		return false, nil
	}
	salt, saltErr := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	expected, keyErr := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if saltErr != nil || keyErr != nil || len(salt) != 16 || len(expected) != 32 {
		return false, nil
	}
	actual, err := p.derive(ctx, password, salt, cost)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
