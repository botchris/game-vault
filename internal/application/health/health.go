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

// Issue is one problem that needs the user's attention.
type Issue struct {
	// Key identifies the issue across checks ("provider:thegamesdb"), so clients can keep track of it.
	Key string

	Level Level
	Kind  Kind

	// Source names what has the problem: the provider or the source; empty for the backups and the
	// disk, which clients name in their language by Kind.
	Source string

	// Reason is a code clients translate ("backup_stale"), with Params filled in ("days": "4"), so
	// the UI speaks the user's language; Message stays as the English detail.
	Reason string
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
