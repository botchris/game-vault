package rpc

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
)

// loggingInterceptor logs every RPC at debug level, and failed ones at warn level.
func loggingInterceptor(log *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			res, err := next(ctx, req)
			attrs := []any{"procedure", req.Spec().Procedure, "duration", time.Since(start).Round(time.Millisecond)}
			if err != nil {
				log.Warn("rpc failed", append(attrs, "code", connect.CodeOf(err).String(), "error", err)...)
			} else {
				log.Debug("rpc", attrs...)
			}
			return res, err
		}
	}
}
