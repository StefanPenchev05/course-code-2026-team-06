package redis

import (
	"errors"
	"regexp"

	"github.com/redis/go-redis/v9"
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func ValidateNamespace(name string) error {
	if !namespacePattern.MatchString(name) {
		return errors.New("REALTIME_NAMESPACE must be 1-32 lowercase letters, digits, underscores or hyphens, starting with a letter")
	}
	return nil
}

// Explicit configuration is mandatory in bootstrap. Legacy constructors remain
// only for existing in-process tests; production never falls back to their keys.
func NewScoped(client *redis.Client, name string) (*Fanout, error) {
	if err := ValidateNamespace(name); err != nil {
		return nil, err
	}
	return &Fanout{client: client, channel: "tutti:" + name + ":realtime:v1"}, nil
}
func NewScopedWatches(client *redis.Client, name string) (*Watches, error) {
	if err := ValidateNamespace(name); err != nil {
		return nil, err
	}
	return &Watches{client: client, prefix: "tutti:{realtime:" + name + "}:watches:"}, nil
}
