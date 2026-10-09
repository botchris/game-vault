// Package settings holds application settings that users change at runtime from the UI.
package settings

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// LogLevels lists the levels accepted by Logging.Level, from most to least verbose.
var LogLevels = []string{"debug", "info", "warn", "error"}

// Logging controls the server log and the rotation of its files.
type Logging struct {
	Level string

	// MaxFileSizeMB is the size at which the current log file is rotated.
	MaxFileSizeMB int

	// MaxFiles is the total number of log files kept, the current one included.
	MaxFiles int
}

// DefaultLogging is used until the user saves their own settings: at most 5 × 10 MB of logs.
func DefaultLogging() Logging {
	return Logging{
		Level:         "info",
		MaxFileSizeMB: 10,
		MaxFiles:      5,
	}
}

// ValidationError reports settings outside the allowed ranges.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// Validate normalises the level and checks the limits.
func (l Logging) Validate() (Logging, error) {
	l.Level = strings.ToLower(strings.TrimSpace(l.Level))
	switch {
	case !slices.Contains(LogLevels, l.Level):
		return l, &ValidationError{fmt.Sprintf("log level must be one of %s", strings.Join(LogLevels, ", "))}
	case l.MaxFileSizeMB < 1 || l.MaxFileSizeMB > 100:
		return l, &ValidationError{"max file size must be between 1 and 100 MB"}
	case l.MaxFiles < 1 || l.MaxFiles > 50:
		return l, &ValidationError{"max files must be between 1 and 50"}
	}

	return l, nil
}

// Preferences are the user's choices that shape the UI and the imports.
type Preferences struct {
	// Currency is the default currency of new prices (ISO 4217); empty until the user picks one.
	Currency string
}

// Validate normalizes the currency and checks it is a three-letter code (or empty).
func (p Preferences) Validate() (Preferences, error) {
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Currency != "" && !isCurrencyCode(p.Currency) {
		return p, &ValidationError{"the default currency must be a three-letter code (EUR, USD…)"}
	}

	return p, nil
}

// isCurrencyCode reports whether s is three letters A–Z. It repeats game's check: settings does
// not import the catalog domain.
func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// Repository is the persistence port for settings.
type Repository interface {
	// Logging returns the saved logging settings, or DefaultLogging when none were saved.
	Logging(ctx context.Context) (Logging, error)

	// SaveLogging stores the logging settings.
	SaveLogging(ctx context.Context, l Logging) error

	// Preferences returns the saved preferences, empty when none were saved.
	Preferences(ctx context.Context) (Preferences, error)

	// SavePreferences stores the preferences.
	SavePreferences(ctx context.Context, p Preferences) error
}
