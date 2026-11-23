package application

import "context"

// AccountStates reads current state for a bounded set of already-authorized IDs.
// Callers must resolve scope and invoke this port in the same read snapshot.
// It does not grant access to account history, reasons or profile information.
type AccountStates interface {
	SuspensionStates(ctx context.Context, users []string) (map[string]bool, error)
}
