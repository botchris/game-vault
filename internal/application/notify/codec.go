package notify

import (
	"encoding/json"
	"fmt"
	"time"

	"gamevault/internal/domain/event"
)

// eventDoc is an event as the queue stores it: data holds the payload of the kind.
type eventDoc struct {
	ID   string          `json:"id"`
	Kind event.Kind      `json:"kind"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// EncodeEvent returns e as JSON {"id","kind","at","data"}. Empty lists and maps are written as []
// and {}, never null, so the webhook's receivers can iterate them. A payload that does not match the
// kind is refused.
func EncodeEvent(e event.Event) (string, error) {
	data, err := normalize(e.Kind, e.Data)
	if err != nil {
		return "", err
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encoding the %s event: %w", e.Kind, err)
	}

	out, err := json.Marshal(eventDoc{
		ID:   e.ID,
		Kind: e.Kind,
		At:   e.At,
		Data: raw,
	})
	if err != nil {
		return "", fmt.Errorf("encoding the %s event: %w", e.Kind, err)
	}

	return string(out), nil
}

// DecodeEvent reads an event written by EncodeEvent; an unknown kind is an error.
func DecodeEvent(raw string) (event.Event, error) {
	var doc eventDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return event.Event{}, fmt.Errorf("decoding an event: %w", err)
	}

	data, err := decodeData(doc.Kind, doc.Data)
	if err != nil {
		return event.Event{}, err
	}

	return event.Event{
		ID:   doc.ID,
		Kind: doc.Kind,
		At:   doc.At,
		Data: data,
	}, nil
}

// decodeData decodes raw into the payload type of kind.
func decodeData(kind event.Kind, raw json.RawMessage) (any, error) {
	switch kind {
	case event.KindCollectionAdded:
		return decodeAs[event.CollectionAdded](kind, raw)
	case event.KindReleaseAnnounced, event.KindReleaseOut:
		return decodeAs[event.Release](kind, raw)
	case event.KindHistoricalLow:
		return decodeAs[event.HistoricalLow](kind, raw)
	case event.KindIssueOpened, event.KindIssueResolved:
		return decodeAs[event.Issue](kind, raw)
	case event.KindKeysExpiring:
		return decodeAs[event.KeysExpiring](kind, raw)
	case event.KindFailedLogins:
		return decodeAs[event.FailedLogins](kind, raw)
	default:
		return nil, fmt.Errorf("unknown event kind %q", kind)
	}
}

func decodeAs[T any](kind event.Kind, raw json.RawMessage) (any, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decoding the %s event: %w", kind, err)
	}

	return v, nil
}

// normalize checks that data is the payload of kind and returns it with nil lists and maps made
// empty.
func normalize(kind event.Kind, data any) (any, error) {
	ok := false

	switch kind {
	case event.KindCollectionAdded:
		var v event.CollectionAdded
		if v, ok = data.(event.CollectionAdded); ok {
			v.Games = gameRefs(v.Games)
			data = v
		}
	case event.KindReleaseAnnounced, event.KindReleaseOut:
		var v event.Release
		if v, ok = data.(event.Release); ok {
			v.Platforms = nonNil(v.Platforms)
			data = v
		}
	case event.KindHistoricalLow:
		_, ok = data.(event.HistoricalLow)
	case event.KindIssueOpened, event.KindIssueResolved:
		var v event.Issue
		if v, ok = data.(event.Issue); ok {
			if v.Params == nil {
				v.Params = map[string]string{}
			}

			data = v
		}
	case event.KindKeysExpiring:
		var v event.KeysExpiring
		if v, ok = data.(event.KeysExpiring); ok {
			keys := make([]event.ExpiringKey, 0, len(v.Keys))
			for _, k := range v.Keys {
				k.Game.Systems = nonNil(k.Game.Systems)
				keys = append(keys, k)
			}

			v.Keys = keys
			data = v
		}
	case event.KindFailedLogins:
		_, ok = data.(event.FailedLogins)
	default:
		return nil, fmt.Errorf("unknown event kind %q", kind)
	}

	if !ok {
		return nil, fmt.Errorf("the %s event carries a %T, not its payload", kind, data)
	}

	return data, nil
}

func gameRefs(in []event.GameRef) []event.GameRef {
	out := make([]event.GameRef, 0, len(in))
	for _, g := range in {
		g.Systems = nonNil(g.Systems)
		out = append(out, g)
	}

	return out
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}

	return in
}
