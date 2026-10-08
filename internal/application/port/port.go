// Package port declares the cross-cutting ports the application layer needs from infrastructure.
package port

import (
	"context"
	"time"
)

// TxManager runs a function inside a single storage transaction. Repositories called with
// the context passed to fn take part in that transaction.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Clock returns the current time. Injected so use cases are deterministic in tests.
type Clock func() time.Time
