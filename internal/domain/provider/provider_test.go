package provider

import (
	"testing"
	"time"

	"gamevault/internal/domain/schema"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestConfigureRequiresSettingsToEnable(t *testing.T) {
	d := Descriptor{ID: "thegamesdb", Kind: KindCover, Name: "TheGamesDB",
		Fields: schema.Fields{{Key: "api_key", Kind: schema.FieldSecret, Required: true}}}

	p := New(d, 0, now)
	if p.Enabled() {
		t.Fatal("providers needing credentials start disabled")
	}

	if err := p.Configure(d, true, nil, now); err == nil {
		t.Fatal("cannot enable without the api key")
	}

	if err := p.Configure(d, true, schema.Settings{"api_key": "k"}, now); err != nil || !p.Enabled() {
		t.Fatalf("enable with key: %v", err)
	}
	// Sending the placeholder back keeps the key.
	if err := p.Configure(d, true, schema.Settings{"api_key": schema.SecretPlaceholder}, now); err != nil || p.Settings()["api_key"] != "k" {
		t.Fatalf("placeholder must keep the secret: %v %v", err, p.Settings())
	}
}

func TestReorder(t *testing.T) {
	a := Rehydrate("a", KindCover, true, 0, nil, now)
	b := Rehydrate("b", KindCover, true, 1, nil, now)
	c := Rehydrate("c", KindCover, true, 2, nil, now)
	list := []*Provider{a, b, c}
	Reorder(list, []ID{"c", "a"}, now)

	if list[0] != c || list[1] != a || list[2] != b || c.Priority() != 0 || b.Priority() != 2 {
		t.Fatalf("unexpected order: %s %s %s", list[0].ID(), list[1].ID(), list[2].ID())
	}
}
