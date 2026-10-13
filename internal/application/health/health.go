// Package health gathers what needs the user's attention, Sonarr-style: each part of Game Vault
// registers a Check that reports its current issues (a provider out of allowance, a source signed
// out, a failed backup, a disk filling up), and the System page shows them together.
package health

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// Level is how serious an issue is.
type Level string

// Values of Level.
const (
	LevelError   Level = "error"   // something stopped working until the user acts
	LevelWarning Level = "warning" // something works worse, or will stop working
)

// Kind is the part of Game Vault an issue is about.
type Kind string

// Values of Kind.
const (
	KindProvider Kind = "provider"
	KindSource   Kind = "source"
	KindBackups  Kind = "backups"
	KindDisk     Kind = "disk"
)

// Target is where in the app an issue can be fixed.
type Target string

// Values of Target.
const (
	TargetNone      Target = ""
	TargetProviders Target = "providers"
	TargetSources   Target = "sources"
	TargetBackups   Target = "system/backups"
)

// Reason is a code clients translate. It is part of the API: the web UI lists the same codes
// (web/src/lib/health.ts) and translates each under health.reasons, and
// TestHealthReasons_webAgrees (internal/application/health/webparity_test.go) keeps the three equal.
type Reason string

// Values of Reason.
const (
	// ReasonProviderQuota means a provider used up its request allowance; Params has "until".
	ReasonProviderQuota Reason = "provider_quota"

	// ReasonProviderRateLimited means a provider's service asked to slow down.
	ReasonProviderRateLimited Reason = "provider_rate_limited"

	// ReasonProviderUnavailable means a provider's service is not answering.
	ReasonProviderUnavailable Reason = "provider_unavailable"

	// ReasonProviderSettings means a provider's service rejects its settings.
	ReasonProviderSettings Reason = "provider_settings"

	// ReasonSourceSignedOut means a store no longer accepts a source's saved session.
	ReasonSourceSignedOut Reason = "source_signed_out"

	// ReasonSourceSyncFailed means a source's last scan failed.
	ReasonSourceSyncFailed Reason = "source_sync_failed"

	// ReasonSourceKeepAliveFailed means keeping a source's session alive failed.
	ReasonSourceKeepAliveFailed Reason = "source_keepalive_failed"

	// ReasonSourceListFailed means the sources could not be read to check them.
	ReasonSourceListFailed Reason = "source_list_failed"

	// ReasonBackupFailed means the last backup failed.
	ReasonBackupFailed Reason = "backup_failed"

	// ReasonBackupStale means no backup has succeeded for a while; Params has "days".
	ReasonBackupStale Reason = "backup_stale"

	// ReasonDiskLow means the disk holding the config directory is filling up; Params has "free",
	// "percent" and "path".
	ReasonDiskLow Reason = "disk_low"

	// ReasonDiskCritical means that disk is almost full; Params as for ReasonDiskLow.
	ReasonDiskCritical Reason = "disk_critical"
)

// reasons lists every Reason.
var reasons = []Reason{
	ReasonProviderQuota, ReasonProviderRateLimited, ReasonProviderUnavailable, ReasonProviderSettings,
	ReasonSourceSignedOut, ReasonSourceSyncFailed, ReasonSourceKeepAliveFailed, ReasonSourceListFailed,
	ReasonBackupFailed, ReasonBackupStale, ReasonDiskLow, ReasonDiskCritical,
}

// Reasons returns every reason an issue can have (a copy: callers cannot change the list).
func Reasons() []Reason { return slices.Clone(reasons) }

// Issue is one problem that needs the user's attention.
type Issue struct {
	// Key identifies the issue across checks ("provider:thegamesdb"), so clients can keep track of it.
	Key string

	Level Level
	Kind  Kind

	// Source names what has the problem: the provider or the source; empty for the backups and the
	// disk, which clients name in their language by Kind.
	Source string

	// Reason is a code clients translate (ReasonBackupStale), with Params filled in ("days": "4"),
	// so the UI speaks the user's language; Message stays as the English detail.
	Reason Reason
	Params map[string]string

	// Message says what happened and what to do, in English, like the app's other errors: the detail
	// under the translated reason (often the service's own words).
	Message string

	// Since is when the problem was first seen; zero when unknown.
	Since time.Time

	// Target is where the problem can be fixed; TargetID narrows it (a source's id).
	Target   Target
	TargetID string
}

// Check reports the current issues of one part of the app. It must be cheap (in-memory state, a
// local read or a stat), never a network call: the System page asks every few minutes.
type Check interface {
	// Issues returns the part's current issues; none when all is well.
	Issues(ctx context.Context) []Issue
}

// CheckFunc adapts a function to Check.
type CheckFunc func(ctx context.Context) []Issue

// Issues implements Check.
func (f CheckFunc) Issues(ctx context.Context) []Issue { return f(ctx) }

// Service aggregates the checks.
type Service struct {
	checks []Check
}

// NewService builds the service on the checks; nil checks are skipped.
func NewService(checks ...Check) *Service {
	return &Service{checks: slices.DeleteFunc(slices.Clone(checks), func(c Check) bool { return c == nil })}
}

// levelRank orders errors before warnings.
var levelRank = map[Level]int{
	LevelError:   0,
	LevelWarning: 1,
}

// Issues returns every check's issues: errors first, then by kind and by source.
func (s *Service) Issues(ctx context.Context) []Issue {
	out := make([]Issue, 0, len(s.checks))

	for _, c := range s.checks {
		out = append(out, c.Issues(ctx)...)
	}

	slices.SortStableFunc(out, func(a, b Issue) int {
		return cmp.Or(
			cmp.Compare(levelRank[a.Level], levelRank[b.Level]),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Source, b.Source),
			cmp.Compare(a.Key, b.Key),
		)
	})

	return out
}
