// Package notifyerr holds the errors the notification connectors (Telegram, ntfy, webhook) return,
// so the dispatcher reads them the same way whichever service sent them: a permanent failure pauses
// the connection, a rate limit delays the next try.
package notifyerr

import (
	"time"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/errkind"
)

// PermanentError is a failure retrying cannot fix. It is an Invalid error too, so a failed Test
// reaches the API as InvalidArgument and not as an internal error. A connector embeds it in a type
// of its own when it must tell one permanent failure from the others (an image refused, say).
type PermanentError string

// Error implements error.
func (e PermanentError) Error() string { return string(e) }

// Is makes errors.Is(e, notify.ErrPermanent) true.
func (PermanentError) Is(target error) bool { return target == notify.ErrPermanent }

// Kind returns errkind.Invalid.
func (PermanentError) Kind() errkind.Kind { return errkind.Invalid }

// Permanent returns a permanent failure with message msg, which says what to do.
func Permanent(msg string) error { return PermanentError(msg) }

// rateLimitError is a service asking to slow down.
type rateLimitError struct {
	msg   string
	after time.Duration
}

// Error implements error.
func (e *rateLimitError) Error() string { return e.msg }

// RetryAfter returns how long the service asked to wait, 0 when it did not say.
func (e *rateLimitError) RetryAfter() time.Duration { return e.after }

// RateLimited returns a rate limit with message msg; the dispatcher waits at least after (when not
// zero) before the next try.
func RateLimited(msg string, after time.Duration) error {
	return &rateLimitError{
		msg:   msg,
		after: after,
	}
}
