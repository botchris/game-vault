package source

import (
	"gamevault/internal/domain/schema"
	"testing"
	"time"
)

func TestStateFields(t *testing.T) {
	d := TypeDescriptor{
		Type: "x",
		Name: "X",
		Fields: []schema.Field{
			{
				Key:  "code",
				Kind: schema.FieldSecret,
			},
			{
				Key:  "session",
				Kind: schema.FieldState,
			},
		},
	}
	now := time.Now()

	src, err := New(d, Config{Settings: schema.Settings{"code": "abc", "session": "from-client"}}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := src.Settings()["session"]; ok {
		t.Fatal("clients must not set state fields")
	}

	src.ReplaceSettings(d, schema.Settings{"session": "s1"}, now)

	if got := d.Masked(src.Settings()); len(got) != 0 {
		t.Fatalf("state must never be shown: %v", got)
	}
	// Reconfiguring keeps the state; the provider rotates it later.
	if err := src.Reconfigure(d, Config{Settings: schema.Settings{"session": "evil"}}, now); err != nil {
		t.Fatal(err)
	}

	if src.Settings()["session"] != "s1" {
		t.Fatalf("reconfigure changed state: %v", src.Settings())
	}

	if src.UpdateState(d, schema.Settings{"session": "s1"}, now) {
		t.Fatal("unchanged state reported as changed")
	}

	if !src.UpdateState(d, schema.Settings{"session": "s2", "code": "ignored"}, now) || src.Settings()["session"] != "s2" || src.Settings()["code"] != "" {
		t.Fatalf("update state: %v", src.Settings())
	}
}
