package source

import (
	"testing"
	"time"
)

func TestStateFields(t *testing.T) {
	d := TypeDescriptor{Type: "x", Name: "X", Fields: []Field{
		{Key: "code", Kind: FieldSecret},
		{Key: "session", Kind: FieldState},
	}}
	now := time.Now()

	src, err := New(d, Config{Settings: Settings{"code": "abc", "session": "from-client"}}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := src.Settings()["session"]; ok {
		t.Fatal("clients must not set state fields")
	}

	src.ReplaceSettings(d, Settings{"session": "s1"}, now)

	if got := d.Masked(src.Settings()); len(got) != 0 {
		t.Fatalf("state must never be shown: %v", got)
	}
	// Reconfiguring keeps the state; the provider rotates it later.
	if err := src.Reconfigure(d, Config{Settings: Settings{"session": "evil"}}, now); err != nil {
		t.Fatal(err)
	}

	if src.Settings()["session"] != "s1" {
		t.Fatalf("reconfigure changed state: %v", src.Settings())
	}

	if src.UpdateState(d, Settings{"session": "s1"}, now) {
		t.Fatal("unchanged state reported as changed")
	}

	if !src.UpdateState(d, Settings{"session": "s2", "code": "ignored"}, now) || src.Settings()["session"] != "s2" || src.Settings()["code"] != "" {
		t.Fatalf("update state: %v", src.Settings())
	}
}
