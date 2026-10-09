// Package schema describes user-configurable settings (credentials, account ids...) shared by
// every pluggable integration: sources and metadata providers.
package schema

import (
	"fmt"
	"strings"
)

// Settings are configuration values keyed by Field.Key.
type Settings map[string]string

// SecretPlaceholder is shown instead of secret values. Sending it back keeps the stored value.
const SecretPlaceholder = "********"

// FieldKind tells clients how to render and protect a setting.
type FieldKind string

// Values of FieldKind.
const (
	FieldText   FieldKind = "text"
	FieldSecret FieldKind = "secret"

	// FieldState is kept by the integration itself (e.g. a refresh token that rotates on every use).
	// It is never shown to clients nor accepted from them.
	FieldState FieldKind = "state"
)

// Field describes one setting.
type Field struct {
	Key      string
	LabelKey string // translation key for the UI
	HelpKey  string // translation key for the UI
	HelpURL  string
	Kind     FieldKind
	Required bool
}

// Fields is the settings schema of an integration.
type Fields []Field

// ValidationError reports missing or invalid settings.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// Validate checks that every required setting is present. owner names the integration in errors.
func (fs Fields) Validate(s Settings, owner string) error {
	for _, f := range fs {
		if f.Kind != FieldState && f.Required && strings.TrimSpace(s[f.Key]) == "" {
			return &ValidationError{fmt.Sprintf("setting %q is required for %s", f.Key, owner)}
		}
	}

	return nil
}

// Merge applies incoming settings over stored ones. Unknown keys are dropped, secrets sent
// back as SecretPlaceholder keep their stored value and state fields always keep it.
func (fs Fields) Merge(stored, incoming Settings) Settings {
	out := Settings{}

	for _, f := range fs {
		if f.Kind == FieldState {
			if v := stored[f.Key]; v != "" {
				out[f.Key] = v
			}

			continue
		}

		v := strings.TrimSpace(incoming[f.Key])
		if f.Kind == FieldSecret && v == SecretPlaceholder {
			v = stored[f.Key]
		}

		if v != "" {
			out[f.Key] = v
		}
	}

	return out
}

// Masked returns the settings with secrets replaced by SecretPlaceholder, safe to show to clients.
func (fs Fields) Masked(s Settings) Settings {
	out := Settings{}

	for _, f := range fs {
		if f.Kind == FieldState {
			continue
		}

		if v, ok := s[f.Key]; ok {
			if f.Kind == FieldSecret {
				v = SecretPlaceholder
			}

			out[f.Key] = v
		}
	}

	return out
}

// Public returns the fields clients may see and edit (state fields excluded).
func (fs Fields) Public() Fields {
	var out Fields

	for _, f := range fs {
		if f.Kind != FieldState {
			out = append(out, f)
		}
	}

	return out
}

// State returns the values of the state fields in s.
func (fs Fields) State(s Settings) Settings {
	out := Settings{}

	for _, f := range fs {
		if f.Kind == FieldState && s[f.Key] != "" {
			out[f.Key] = s[f.Key]
		}
	}

	return out
}
