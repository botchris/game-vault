// Package errkind says what kind of failure an error is (bad input, nothing found, a service that
// is not answering…), so an inbound adapter can answer it with the right status without knowing
// every package's sentinel errors.
//
// A sentinel error gets its kind from New (or, for an error type of its own, from a Kind method);
// input that breaks a rule is a *ValidationError (Invalidf); Wrap gives a kind to an error that has
// none. Of reads the kind back through any wrapping. An error without a kind is a failure of Game
// Vault itself.
package errkind

import (
	"errors"
	"fmt"
)

// Kind is the category of a failure.
type Kind uint8

// Values of Kind.
const (
	// Unknown is an error without a kind: a failure of Game Vault itself (a database error, a bug).
	Unknown Kind = iota

	// Invalid is input that breaks a rule: the request must change.
	Invalid

	// NotFound is a request for something that does not exist (or no longer does).
	NotFound

	// AlreadyExists is a request to create something that exists already.
	AlreadyExists

	// Conflict is a change that collided with another one made meanwhile: trying again may work.
	Conflict

	// Unauthenticated means the caller has not signed in to Game Vault. Only Game Vault's own
	// sign-in uses it: the web page signs the user out when it sees it, so a store or a provider
	// that rejects its credentials is Precondition, never this.
	Unauthenticated

	// PermissionDenied means the caller is known but may not do this.
	PermissionDenied

	// Precondition means the request is valid but the state does not allow it yet: something else
	// must be done first (sign in to a store again, add a barcode, enable a provider…).
	Precondition

	// Exhausted means the caller made too many attempts and must wait.
	Exhausted

	// Unavailable means an external service Game Vault relies on is busy, rate-limited or not
	// answering: trying again later may work.
	Unavailable

	// kindCount is the number of kinds, Unknown included: new kinds go above it.
	kindCount
)

// All returns every kind but Unknown, so an adapter can check that it answers each one.
func All() []Kind {
	out := make([]Kind, 0, kindCount-1)
	for k := Unknown + 1; k < kindCount; k++ {
		out = append(out, k)
	}

	return out
}

// String returns the kind's name, for logs and test messages.
func (k Kind) String() string {
	switch k {
	case Invalid:
		return "invalid"
	case NotFound:
		return "not found"
	case AlreadyExists:
		return "already exists"
	case Conflict:
		return "conflict"
	case Unauthenticated:
		return "unauthenticated"
	case PermissionDenied:
		return "permission denied"
	case Precondition:
		return "precondition"
	case Exhausted:
		return "exhausted"
	case Unavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// kinded is implemented by every error that knows its kind.
type kinded interface {
	// Kind returns the error's kind.
	Kind() Kind
}

// Of returns the kind of err: that of the first error with a kind in the order errors.As visits
// them (depth first: an error before the errors it wraps, and the errors an error joins or wraps
// with several %w in order), so a wrapper can give a more precise kind than the error it wraps. It
// returns Unknown for a nil error and for an error without a kind.
func Of(err error) Kind {
	var k kinded
	if errors.As(err, &k) {
		return k.Kind()
	}

	return Unknown
}

// sentinel is the type of the errors New returns.
type sentinel struct {
	kind Kind
	msg  string
}

func (e *sentinel) Error() string { return e.msg }

// Kind returns the kind New was given.
func (e *sentinel) Kind() Kind { return e.kind }

// New returns a sentinel error of the given kind. Each call returns a distinct error, so callers
// match it with errors.Is as they would one made by errors.New.
func New(kind Kind, msg string) error {
	return &sentinel{
		kind: kind,
		msg:  msg,
	}
}

// wrapped is the type of the errors Wrap returns.
type wrapped struct {
	kind Kind
	err  error
}

func (e *wrapped) Error() string { return e.err.Error() }

func (e *wrapped) Unwrap() error { return e.err }

// Kind returns the kind Wrap was given.
func (e *wrapped) Kind() Kind { return e.kind }

// Wrap gives err a kind and keeps its message; errors.Is and errors.As still see err. It returns
// nil for a nil err. Wrap(Unknown, err) hides the kinds inside err: Of then reads Unknown.
func Wrap(kind Kind, err error) error {
	if err == nil {
		return nil
	}

	return &wrapped{
		kind: kind,
		err:  err,
	}
}

// ValidationError reports input that breaks a rule. Its message says what to fix. Every domain
// package returns this one type (the packages keep their own name for it as an alias), so a
// caller matches any rule broken anywhere with one errors.As.
type ValidationError struct {
	msg string
}

// Error returns the message, which says what to fix.
func (e *ValidationError) Error() string { return e.msg }

// Kind returns Invalid.
func (*ValidationError) Kind() Kind { return Invalid }

// Invalidf returns a *ValidationError with the formatted message.
func Invalidf(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}
