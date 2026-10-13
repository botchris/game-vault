package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"gamevault/internal/domain/errkind"
)

// kindCodes is the Connect code of each kind of error. Kinds are declared next to the errors
// (errkind), so this one table answers every package's errors.
var kindCodes = map[errkind.Kind]connect.Code{
	errkind.Invalid:          connect.CodeInvalidArgument,
	errkind.NotFound:         connect.CodeNotFound,
	errkind.AlreadyExists:    connect.CodeAlreadyExists,
	errkind.Conflict:         connect.CodeAborted,
	errkind.Unauthenticated:  connect.CodeUnauthenticated,
	errkind.PermissionDenied: connect.CodePermissionDenied,
	errkind.Precondition:     connect.CodeFailedPrecondition,
	errkind.Exhausted:        connect.CodeResourceExhausted,
	errkind.Unavailable:      connect.CodeUnavailable,
}

// toConnectError maps domain and application errors to Connect status codes by their kind. An error
// without a kind is a failure of Game Vault itself: it becomes Internal, which the logging
// interceptor reports as an error.
func toConnectError(err error) error {
	if err == nil {
		return nil
	}

	var ce *connect.Error

	switch {
	case errors.As(err, &ce):
		return err
	case errors.Is(err, context.Canceled):
		// The caller went away (a closed page): not a failure of Game Vault.
		return connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}

	if code, ok := kindCodes[errkind.Of(err)]; ok {
		return connect.NewError(code, err)
	}

	return connect.NewError(connect.CodeInternal, err)
}
