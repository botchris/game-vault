package battlenet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

type fakeSteam struct{ asked []string }

func (f *fakeSteam) LinkStore() game.Store {
	return game.Store{Key: game.LinkSteam, Name: "Steam"}
}
func (f *fakeSteam) SearchLinks(_ context.Context, q string) ([]media.LinkMatch, error) {
	f.asked = append(f.asked, q)
	return []media.LinkMatch{{ID: "1", Name: "Call of Duty®: Modern Warfare® 2 Campaign Remastered"}, {ID: "393080", Name: "Call of Duty®: Modern Warfare® Remastered"}}, nil
}
func (f *fakeSteam) Descriptor() provider.Descriptor             { return provider.Descriptor{ID: "steam"} }
func (f *fakeSteam) Test(context.Context, schema.Settings) error { return nil }
func (f *fakeSteam) Applies(q media.CoverQuery) bool             { return q.Links[game.LinkSteam] != "" }
func (f *fakeSteam) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{{URL: "https://steam/" + q.Title, Label: "Library art", Provider: "steam"}}, nil
}

func TestCovers(t *testing.T) {
	steam := &fakeSteam{}
	c := NewCovers(steam, steam)

	q := media.CoverQuery{Title: "Call of Duty: Modern Warfare Remastered (2017)", Links: game.Links{"battlenet": "1329875278"}}
	if !c.Applies(q) || c.Applies(media.CoverQuery{Title: "x", Links: game.Links{game.LinkSteam: "1"}}) {
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
	if got, _ := c.Covers(context.Background(), media.CoverQuery{Title: "World of Warcraft®", Links: game.Links{"battlenet": "5730135"}}, nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

type failingSearch struct{}

func (failingSearch) LinkStore() game.Store { return game.Store{Key: game.LinkSteam} }

func (failingSearch) SearchLinks(context.Context, string) ([]media.LinkMatch, error) {
	return nil, errors.New("steam search: HTTP 429")
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Steam search that answers", func(t *testing.T) {
		steam := &fakeSteam{}
		c := NewCovers(steam, steam)

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it succeeds after one search", func(t *testing.T) {
				require.NoError(t, err)
				assert.Len(t, steam.asked, 1)
			})
		})
	})

	t.Run("GIVEN a Steam search that is rate limited", func(t *testing.T) {
		c := NewCovers(failingSearch{}, &fakeSteam{})

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it fails and says what to do", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "try again later")
			})
		})
	})
}
