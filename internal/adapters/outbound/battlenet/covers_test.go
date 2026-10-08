package battlenet

import (
	"context"
	"testing"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

type fakeSteam struct{ asked []string }

func (f *fakeSteam) SearchApps(_ context.Context, q string) ([]media.AppMatch, error) {
	f.asked = append(f.asked, q)
	return []media.AppMatch{{AppID: 1, Name: "Call of Duty®: Modern Warfare® 2 Campaign Remastered"}, {AppID: 393080, Name: "Call of Duty®: Modern Warfare® Remastered"}}, nil
}
func (f *fakeSteam) Descriptor() provider.Descriptor { return provider.Descriptor{ID: "steam"} }
func (f *fakeSteam) Applies(q media.CoverQuery) bool { return q.SteamAppID != 0 }
func (f *fakeSteam) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{{URL: "https://steam/" + q.Title, Label: "Library art", Provider: "steam"}}, nil
}

func TestCovers(t *testing.T) {
	steam := &fakeSteam{}
	c := NewCovers(steam, steam)

	q := media.CoverQuery{Title: "Call of Duty: Modern Warfare Remastered (2017)", ExternalIDs: []string{"battlenet:1329875278"}}
	if !c.Applies(q) || c.Applies(media.CoverQuery{Title: "x", SteamAppID: 1}) {
		t.Fatal("applies only to games imported from Battle.net")
	}

	got, err := c.Covers(context.Background(), q, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Provider != CoverProviderID || got[0].URL != "https://steam/Call of Duty®: Modern Warfare® Remastered" {
		t.Fatalf("candidates: %+v", got)
	}

	if steam.asked[0] != "Call of Duty: Modern Warfare Remastered" {
		t.Fatalf("the year should be dropped from the search: %q", steam.asked[0])
	}
	// No exact title on Steam: nothing (the next providers try).
	if got, _ := c.Covers(context.Background(), media.CoverQuery{Title: "World of Warcraft®", ExternalIDs: []string{"battlenet:5730135"}}, nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
