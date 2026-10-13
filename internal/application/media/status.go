package media

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"gamevault/internal/application/health"
	"gamevault/internal/domain/provider"
)

// StatusKind is the kind of temporary, provider-level problem a ProviderStatus reports.
type StatusKind string

// Values of StatusKind.
const (
	StatusQuota       StatusKind = "quota"        // the request allowance is used up until it renews
	StatusRateLimited StatusKind = "rate_limited" // the service asked to slow down, or is busy
	StatusUnavailable StatusKind = "unavailable"  // the service is not answering
	StatusSettings    StatusKind = "settings"     // the service rejects the provider's settings
)

// How long a problem is shown when the error does not say when it ends (provider.Resumer).
const (
	statusRateLimitedFor = 5 * time.Minute
	statusOtherFor       = time.Hour
)

// ProviderStatus is a provider's current problem: one that concerns the provider, not a game, so the
// System and Providers pages show it instead of every game sheet.
type ProviderStatus struct {
	// Provider names the service: its settings group, or its id when it has none.
	Provider provider.ID

	// Name is the provider's display name.
	Name string

	// Providers lists the registered providers of the service (covers, details…).
	Providers []provider.ID

	Kind StatusKind

	// Message is the provider's own explanation, in English.
	Message string

	// Since is when the problem was first seen; Until is when it is expected to end, after which it
	// is no longer reported.
	Since time.Time
	Until time.Time
}

// statuses keeps the providers' current problems in memory, by service.
type statuses struct {
	mu      sync.Mutex
	entries map[provider.ID]ProviderStatus
}

// statusKey names the service a provider belongs to.
func statusKey(d provider.Descriptor) provider.ID {
	if d.SettingsGroup != "" {
		return provider.ID(d.SettingsGroup)
	}

	return d.ID
}

// ProviderLevel reports whether err is a problem of the provider rather than of a game: a rate limit
// (busy, quota, outage) or rejected settings. Those are reported in the provider's status, not on
// game sheets, in the cover picker or as a copy's price warning.
func ProviderLevel(err error) bool {
	return errors.Is(err, provider.ErrRateLimited) || errors.Is(err, provider.ErrBadSettings)
}

// ReportResult records what a provider's answer to a caller outside this service (the valuation
// service asking its price providers) says about the provider, as noteResult does for the
// service's own calls.
func (s *Service) ReportResult(id provider.ID, err error) { s.noteResult(id, err) }

// noteResult records what a provider's answer says about the provider: a provider-level problem sets
// its status, a successful answer clears it, and any other error (a failure for one game) leaves it.
// Providers answer success only when they did reach the service (one without its settings returns
// provider.ErrBadSettings, not an empty answer), so a sibling that sent nothing never clears a
// status. Answers are recorded as they arrive, not in request order: a slow request sent with the
// old settings that ends after new ones were saved can set "settings" again, until the next answer.
func (s *Service) noteResult(id provider.ID, err error) {
	impl, ok := s.impls[id]
	if !ok {
		return
	}

	d := impl.Descriptor()
	key := statusKey(d)

	s.status.mu.Lock()
	defer s.status.mu.Unlock()

	if err == nil {
		delete(s.status.entries, key)
		return
	}

	if !ProviderLevel(err) {
		return
	}

	now := s.now()
	st := ProviderStatus{
		Provider: key,
		Name:     d.Name,
		Message:  err.Error(),
		Since:    now,
	}

	switch {
	case errors.Is(err, provider.ErrQuotaExhausted):
		st.Kind, st.Until = StatusQuota, now.Add(statusOtherFor)
	case errors.Is(err, provider.ErrBadSettings):
		st.Kind, st.Until = StatusSettings, now.Add(statusOtherFor)
	case errors.Is(err, provider.ErrUnavailable):
		st.Kind, st.Until = StatusUnavailable, now.Add(statusRateLimitedFor)
	default:
		st.Kind, st.Until = StatusRateLimited, now.Add(statusRateLimitedFor)
	}

	var r provider.Resumer
	if errors.As(err, &r) && r.ResumesAt().After(now) {
		st.Until = r.ResumesAt()
	}

	if prev, ok := s.status.entries[key]; ok && prev.Kind == st.Kind && now.Before(prev.Until) {
		st.Since = prev.Since // the same problem going on
	}

	if s.status.entries == nil {
		s.status.entries = map[provider.ID]ProviderStatus{}
	}

	s.status.entries[key] = st
}

// forget drops a service's status.
func (st *statuses) forget(key provider.ID) {
	st.mu.Lock()
	defer st.mu.Unlock()

	delete(st.entries, key)
}

// ProviderStatuses returns the providers' current problems, by name.
func (s *Service) ProviderStatuses() []ProviderStatus {
	now := s.now()

	s.status.mu.Lock()
	defer s.status.mu.Unlock()

	out := make([]ProviderStatus, 0, len(s.status.entries))

	for key, st := range s.status.entries {
		if !now.Before(st.Until) {
			delete(s.status.entries, key)
			continue
		}

		st.Providers = nil

		for _, id := range s.order {
			if statusKey(s.impls[id].Descriptor()) == key {
				st.Providers = append(st.Providers, id)
			}
		}

		out = append(out, st)
	}

	// Services may share a name (Steam's covers and details): the id keeps the order stable.
	slices.SortFunc(out, func(a, b ProviderStatus) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Provider, b.Provider))
	})

	return out
}

// Issues implements health.Check: the providers' current problems. Rejected settings are an error
// (the provider stays useless until they are fixed); a used-up allowance, a rate limit or an outage a
// warning (it ends on its own).
func (s *Service) Issues(context.Context) []health.Issue {
	statuses := s.ProviderStatuses()
	out := make([]health.Issue, 0, len(statuses))

	for _, st := range statuses {
		level := health.LevelWarning
		if st.Kind == StatusSettings {
			level = health.LevelError
		}

		issue := health.Issue{
			Key:     "provider:" + string(st.Provider),
			Level:   level,
			Kind:    health.KindProvider,
			Source:  st.Name,
			Reason:  "provider_" + string(st.Kind),
			Message: st.Message,
			Since:   st.Since,
			Target:  health.TargetProviders,
		}
		if st.Kind == StatusQuota {
			issue.Params = map[string]string{"until": st.Until.UTC().Format(time.RFC3339)}
		}

		out = append(out, issue)
	}

	return out
}
