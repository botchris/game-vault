package battlenet

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// fakeSteam stands for the Steam store: it finds the titles of apps by their exact title, and
// gives library art for a game linked to an AppID.
type fakeSteam struct {
	// apps are the AppIDs Steam's title rules find, by title.
	apps map[string]int64

	// err is the answer of every title lookup when set.
	err error

	// asked are the titles looked up.
	asked []string
}

func (f *fakeSteam) AppIDByTitle(_ context.Context, title string) (int64, error) {
	f.asked = append(f.asked, title)

	return f.apps[title], f.err
}

func (f *fakeSteam) Descriptor() provider.Descriptor             { return provider.Descriptor{ID: "steam"} }
func (f *fakeSteam) Test(context.Context, schema.Settings) error { return nil }
func (f *fakeSteam) Applies(q media.GameQuery) bool              { return q.Links[game.LinkSteam] != "" }
func (f *fakeSteam) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{{
		URL:      "https://steam/" + q.Links[game.LinkSteam],
		Label:    "Library art",
		Provider: "steam",
	}}, nil
}

func TestCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Steam store that sells the remaster", func(t *testing.T) {
		steam := &fakeSteam{apps: map[string]int64{"Call of Duty: Modern Warfare Remastered": 393080}}
		c := NewCovers(steam)

		q := media.GameQuery{
			Title: "Call of Duty: Modern Warfare Remastered (2017)",
			Links: game.Links{"battlenet": "1329875278"},
		}

		t.Run("THEN it applies only to games imported from Battle.net", func(t *testing.T) {
			assert.True(t, c.Applies(q))
			assert.False(t, c.Applies(media.GameQuery{
				Title: "x",
				Links: game.Links{game.LinkSteam: "1"},
			}))
		})

		t.Run("WHEN the covers of the Battle.net copy are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, q, nil)
			require.NoError(t, err)

			t.Run("THEN Steam's art of the app it found is proposed as Battle.net's", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, "https://steam/393080", got[0].URL)
				assert.Equal(t, CoverProviderID, got[0].Provider)
				assert.Equal(t, "Battle.net (Steam): Library art", got[0].Label)
			})

			t.Run("AND the title was looked up without Battle.net's year", func(t *testing.T) {
				assert.Equal(t, []string{"Call of Duty: Modern Warfare Remastered"}, steam.asked)
			})
		})

		t.Run("WHEN the covers of a game Steam does not sell are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, media.GameQuery{
				Title: "World of Warcraft®",
				Links: game.Links{"battlenet": "5730135"},
			}, nil)

			t.Run("THEN there are none, for the next providers to try", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, got)
			})
		})
	})

	t.Run("GIVEN a Steam store that is rate limited", func(t *testing.T) {
		c := NewCovers(&fakeSteam{err: fmt.Errorf("store search: %w", provider.ErrRateLimited)})

		t.Run("WHEN covers are asked", func(t *testing.T) {
			_, err := c.Covers(ctx, media.GameQuery{
				Title: "Diablo",
				Links: game.Links{"battlenet": "1"},
			}, nil)

			t.Run("THEN the error says it is a passing trouble, not a missing cover", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Steam search that answers", func(t *testing.T) {
		steam := &fakeSteam{}
		c := NewCovers(steam)

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it succeeds after one lookup", func(t *testing.T) {
				require.NoError(t, err)
				assert.Len(t, steam.asked, 1)
			})
		})
	})

	t.Run("GIVEN a Steam search that is rate limited", func(t *testing.T) {
		c := NewCovers(&fakeSteam{err: fmt.Errorf("store search: %w", provider.ErrRateLimited)})

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it fails and says what to do", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Contains(t, err.Error(), "try again later")
			})
		})
	})
}
