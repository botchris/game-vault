package rpc

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
)

// loggingInterceptor logs every RPC at debug level, failed ones at warn level, and internal
// failures at error level.
func loggingInterceptor(log *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			res, err := next(ctx, req)

			attrs := []any{"procedure", req.Spec().Procedure, "duration", time.Since(start).Round(time.Millisecond)}

			switch code := connect.CodeOf(err); {
			case err == nil:
				log.Debug("rpc", attrs...)
			case code == connect.CodeInternal || code == connect.CodeUnknown:
				// An error without a kind (errkind) is a failure of Game Vault itself, not of the
				// request: it deserves more attention than a user mistake.
				log.Error("rpc failed", append(attrs, "code", code.String(), "error", err)...)
			default:
				log.Warn("rpc failed", append(attrs, "code", code.String(), "error", err)...)
			}

			return res, err
		}
	}
}
