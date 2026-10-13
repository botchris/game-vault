package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"gamevault/internal/domain/provider"
)

// ClassifyStatus turns an unexpected HTTP status into an error that says what kind of failure it
// is: a 429 matches provider.ErrRateLimited and a 5xx provider.ErrUnavailable, so callers (the
// cover and metadata chains, the provider status, a source's health) treat them as passing troubles
// that say nothing about a game or a session. Any other status is a plain "HTTP n" error: only the
// caller knows what a 401, 403 or 404 means for its API.
func ClassifyStatus(status int) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("HTTP %d: %w", status, provider.ErrRateLimited)
	case status >= http.StatusInternalServerError:
		return fmt.Errorf("not answering (HTTP %d): %w", status, provider.ErrUnavailable)
	}

	return fmt.Errorf("HTTP %d", status)
}

// Rejected reports whether an OAuth token endpoint's status means it refused the code or the
// session itself (400 invalid_grant, 401, 403), rather than a passing trouble such as a 429 or an
// outage, after which signing in again would not help.
func Rejected(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden
}

// ClassifyError classifies the error of one request (from Client.Do or a plain http.Client) the way
// ClassifyStatus does: a *StatusError with a 429 or a 5xx, no answer at all (a transport error) and
// a web page instead of JSON (ErrNotJSON, a bot check or a maintenance notice) match
// provider.ErrRateLimited, the last two as provider.ErrUnavailable. A transport error never names
// the query of its URL, which may hold an API key. Errors already classified, other statuses, and
// errors after the caller gave up (ctx done) are returned as they are.
func ClassifyError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// The query goes first, so no path returns an error that names it.
	err = withoutQuery(err)
	if ctx.Err() != nil || errors.Is(err, provider.ErrRateLimited) || errors.Is(err, provider.ErrBadSettings) {
		return err
	}

	var se *StatusError
	if errors.As(err, &se) {
		switch {
		case se.Status == http.StatusTooManyRequests:
			return fmt.Errorf("%w: %w", err, provider.ErrRateLimited)
		case se.Status >= http.StatusInternalServerError:
			return fmt.Errorf("%w: %w", err, provider.ErrUnavailable)
		}

		return err
	}

	var ue *url.Error
	if errors.As(err, &ue) || errors.Is(err, ErrNotJSON) {
		return fmt.Errorf("not answering, try again later (%w): %w", err, provider.ErrUnavailable)
	}

	return err
}
