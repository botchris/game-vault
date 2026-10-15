package notify_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

const address = "https://vault.example"

var at = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

func ref(id, title string, systems ...string) event.GameRef {
	return event.GameRef{
		ID:      game.ID(id),
		Title:   title,
		Systems: systems,
	}
}

func TestRender_texts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	w := newWorld(t)
	release := event.Release{
		Title:        "Example Sequel",
		Date:         "2026-11-20",
		Platforms:    []string{"PC", "PS5"},
		CoverURL:     "https://img.example/sequel.jpg",
		URL:          "https://db.example/sequel",
		Because:      "g-1",
		BecauseTitle: "Example Game",
	}
	tba := release
	tba.Date = ""
	quota := event.Issue{
		Key:     "provider:exampledb",
		Level:   "warning",
		Kind:    "provider",
		Source:  "Example DB",
		Reason:  "provider_quota",
		Params:  map[string]string{"until": "2026-11-01"},
		Message: "monthly allowance used up",
	}
	stale := event.Issue{
		Key:    "backups",
		Kind:   "backups",
		Reason: "backup_stale",
		Params: map[string]string{"days": "9"},
	}

	tests := []struct {
		name  string
		kind  event.Kind
		data  any
		title string
		text  string
	}{
		{
			"one game from a source", event.KindCollectionAdded,
			event.CollectionAdded{
				Origin:     event.OriginSource,
				OriginName: "Example Store",
				Games:      []event.GameRef{ref("g-1", "Example Game", "PC")},
			},
			"Example Store: a new game", "Example Game (PC)",
		},
		{
			"games and copies from a source", event.KindCollectionAdded,
			event.CollectionAdded{
				Origin:     event.OriginSource,
				OriginName: "Example Store",
				Games: []event.GameRef{
					ref("g-1", "Example Game", "PC"),
					ref("g-2", "Another Game", "PC", "Switch"),
					ref("g-3", "Third Game"),
				},
				CopiesAdded: 2,
			},
			"Example Store: 3 new games",
			"Example Game (PC)\nAnother Game (PC, Switch)\nThird Game\nand 2 new copies of games you had",
		},
		{
			"one game by hand", event.KindCollectionAdded,
			event.CollectionAdded{
				Origin: event.OriginManual,
				Games:  []event.GameRef{ref("g-1", "Example Game", "PS5")},
			},
			"A game added by hand", "Example Game (PS5)",
		},
		{
			"games by hand", event.KindCollectionAdded,
			event.CollectionAdded{
				Origin: event.OriginManual,
				Games:  []event.GameRef{ref("g-1", "Example Game", "PS5"), ref("g-2", "Another Game", "PC")},
			},
			"2 games added by hand", "Example Game (PS5)\nAnother Game (PC)",
		},
		{
			"a CSV import", event.KindCollectionAdded,
			event.CollectionAdded{
				Origin: event.OriginImport,
				Games:  []event.GameRef{ref("g-1", "Example Game", "PC"), ref("g-2", "Another Game", "PC")},
			},
			"CSV import: 2 new games", "Example Game (PC)\nAnother Game (PC)",
		},
		{
			"an announced release", event.KindReleaseAnnounced, release,
			"New in your series: Example Sequel", "2026-11-20 · PC, PS5\nBecause you have Example Game",
		},
		{
			"a release without a date", event.KindReleaseAnnounced, tba,
			"New in your series: Example Sequel", "date to be announced · PC, PS5\nBecause you have Example Game",
		},
		{
			"a release out today", event.KindReleaseOut, release,
			"Out today: Example Sequel", "2026-11-20 · PC, PS5\nBecause you have Example Game",
		},
		{
			"a historical low", event.KindHistoricalLow,
			event.HistoricalLow{
				Title: "Example Game",
				Shop:  "Example Shop",
				Price: event.Money{
					Amount:   999,
					Currency: "EUR",
				},
				Regular: event.Money{
					Amount:   1999,
					Currency: "EUR",
				},
				Cut:      50,
				URL:      "https://deals.example/g",
				ImageURL: "https://img.example/g.jpg",
			},
			"Example Game at its historical low", "€9.99 (−50%, was €19.99) at Example Shop",
		},
		{
			"a provider's issue", event.KindIssueOpened, quota,
			"⚠ Example DB: Monthly allowance used up; renews 2026-11-01", "monthly allowance used up",
		},
		{
			"a backups issue", event.KindIssueOpened, stale,
			"⚠ Backups: No backup in 9 days", "",
		},
		{
			"a resolved issue", event.KindIssueResolved, quota,
			"✓ Example DB works again", "monthly allowance used up",
		},
		{
			"keys expiring", event.KindKeysExpiring,
			event.KeysExpiring{Keys: []event.ExpiringKey{
				{
					Game:     ref("g-1", "Example Game"),
					Store:    "Steam",
					RedeemBy: "2026-10-15",
					DaysLeft: 0,
				},
				{
					Game:     ref("g-2", "Another Game"),
					Store:    "GOG",
					RedeemBy: "2026-10-22",
					DaysLeft: 7,
				},
			}},
			"2 keys expire within 30 days", "Example Game (Steam): today\nAnother Game (GOG): in 7 days",
		},
		{
			"one key expiring", event.KindKeysExpiring,
			event.KeysExpiring{Keys: []event.ExpiringKey{
				{
					Game:     ref("g-1", "Example Game"),
					Store:    "Steam",
					RedeemBy: "2026-10-16",
					DaysLeft: 1,
				},
			}},
			"A key expires within 30 days", "Example Game (Steam): in 1 day",
		},
		{
			"failed sign-ins", event.KindFailedLogins,
			event.FailedLogins{
				Address: "192.0.2.7",
				Count:   3,
				First:   time.Date(2026, 10, 15, 10, 0, 0, 0, time.Local),
				Last:    time.Date(2026, 10, 15, 10, 20, 0, 0, time.Local),
			},
			"3 failed sign-ins from 192.0.2.7",
			"Between 10:00 and 10:20. If it was not you, check who can reach Game Vault.",
		},
	}

	for _, tt := range tests {
		t.Run("GIVEN "+tt.name, func(t *testing.T) {
			e := event.New(tt.kind, at, tt.data)

			t.Run("WHEN it is rendered in English THEN it reads as expected", func(t *testing.T) {
				m := notify.Render(ctx, e, "en", address, w.catalog, w.covers)
				assert.Equal(t, tt.title, m.Title)
				assert.Equal(t, tt.text, m.Text)
				assert.Equal(t, e, m.Event)
			})

			t.Run("WHEN it is rendered in Spanish THEN it is translated", func(t *testing.T) {
				en := notify.Render(ctx, e, "en", address, w.catalog, w.covers)
				es := notify.Render(ctx, e, "es", address, w.catalog, w.covers)
				assert.NotEmpty(t, es.Title)
				assert.NotEqual(t, en.Title, es.Title)

				if en.Link != "" {
					assert.NotEqual(t, en.LinkLabel, es.LinkLabel)
				}

				assert.Equal(t, en.Link, es.Link)
			})

			t.Run("WHEN the language is unknown THEN it is English", func(t *testing.T) {
				m := notify.Render(ctx, e, "fr", address, w.catalog, w.covers)
				assert.Equal(t, tt.title, m.Title)
			})
		})
	}
}

func TestRender_collection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN three new games, the last two with a cover on disk", func(t *testing.T) {
		w := newWorld(t)
		a := w.addGame(t, "Example Game", "Steam")
		b := w.addGame(t, "Another Game", "Steam")
		c := w.addGame(t, "Third Game", "Steam")
		w.covers.images[b.ID()] = media.Image{
			Data:        []byte("jpeg-b"),
			ContentType: "image/jpeg",
		}
		w.covers.images[c.ID()] = media.Image{
			Data:        []byte("png-c"),
			ContentType: "image/png",
		}
		e := event.New(event.KindCollectionAdded, at, event.CollectionAdded{
			Origin: event.OriginManual,
			Games:  []event.GameRef{ref(string(a.ID()), a.Title()), ref(string(b.ID()), b.Title()), ref(string(c.ID()), c.Title())},
		})

		t.Run("WHEN it is rendered", func(t *testing.T) {
			m := notify.Render(ctx, e, "en", "", w.catalog, w.covers)

			t.Run("THEN the two covers are attached, as bytes", func(t *testing.T) {
				require.Len(t, m.Images, 2)
				assert.Equal(t, []byte("jpeg-b"), m.Images[0].Data)
				assert.Equal(t, "image/jpeg", m.Images[0].ContentType)
				assert.Equal(t, []byte("png-c"), m.Images[1].Data)
				assert.NotEmpty(t, m.Images[0].Name)
				assert.Empty(t, m.Images[0].URL)
			})

			t.Run("AND each cover asked is the main edition's", func(t *testing.T) {
				assert.Equal(t, a.MainEdition().System, w.covers.asked[a.ID()])
			})

			t.Run("AND without an address there is no link", func(t *testing.T) {
				assert.Empty(t, m.Link)
			})
		})

		t.Run("WHEN it is rendered with an address THEN it links to the recent games", func(t *testing.T) {
			m := notify.Render(ctx, e, "en", address, w.catalog, w.covers)
			assert.Equal(t, address+"/#/library/recent", m.Link)
			assert.Equal(t, "Open in Game Vault", m.LinkLabel)
		})
	})

	t.Run("GIVEN three games with covers WHEN rendered THEN only two are attached", func(t *testing.T) {
		w := newWorld(t)

		games := make([]event.GameRef, 0, 3)

		for i := range 3 {
			g := w.addGame(t, fmt.Sprintf("Game %d", i), "Steam")
			w.covers.images[g.ID()] = media.Image{
				Data:        []byte{byte(i)},
				ContentType: "image/jpeg",
			}
			games = append(games, ref(string(g.ID()), g.Title()))
		}

		m := notify.Render(ctx, event.New(event.KindCollectionAdded, at, event.CollectionAdded{
			Origin: event.OriginManual,
			Games:  games,
		}), "en", "", w.catalog, w.covers)
		assert.Len(t, m.Images, 2)
	})

	t.Run("GIVEN one game WHEN rendered with an address THEN it links to the game", func(t *testing.T) {
		w := newWorld(t)
		m := notify.Render(ctx, event.New(event.KindCollectionAdded, at, event.CollectionAdded{
			Origin: event.OriginManual,
			Games:  []event.GameRef{ref("g-1", "Example Game")},
		}), "en", address, w.catalog, w.covers)
		assert.Equal(t, address+"/#/game/g-1", m.Link)
		assert.Empty(t, m.Images, "a game missing from the catalog has no cover")
	})

	t.Run("GIVEN 30 new games WHEN rendered THEN the first 20 are listed and the rest counted", func(t *testing.T) {
		w := newWorld(t)
		games := make([]event.GameRef, 0, 30)

		for i := range 30 {
			games = append(games, ref(fmt.Sprintf("g-%d", i), fmt.Sprintf("Game %d", i)))
		}

		m := notify.Render(ctx, event.New(event.KindCollectionAdded, at, event.CollectionAdded{
			Origin:     event.OriginSource,
			OriginName: "Example Store",
			Games:      games,
		}), "en", "", w.catalog, w.covers)

		lines := strings.Split(m.Text, "\n")
		require.Len(t, lines, 21)
		assert.Equal(t, "Game 0", lines[0])
		assert.Equal(t, "Game 19", lines[19])
		assert.Equal(t, "… and 10 more", lines[20])
		assert.Equal(t, "Example Store: 30 new games", m.Title)
	})
}

func TestRender_linksAndImages(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	w := newWorld(t)
	release := event.New(event.KindReleaseAnnounced, at, event.Release{
		Title:        "Example Sequel",
		CoverURL:     "https://img.example/sequel.jpg",
		URL:          "https://db.example/sequel",
		Because:      "g-1",
		BecauseTitle: "Example Game",
	})
	deal := event.New(event.KindHistoricalLow, at, event.HistoricalLow{
		Title:    "Example Game",
		URL:      "https://deals.example/g",
		ImageURL: "https://img.example/g.jpg",
	})
	issue := event.New(event.KindIssueOpened, at, event.Issue{
		Key:    "disk:config",
		Kind:   "disk",
		Reason: "disk_low",
	})
	keys := event.New(event.KindKeysExpiring, at, event.KeysExpiring{Keys: []event.ExpiringKey{{Game: ref("g-1", "Example Game")}}})
	logins := event.New(event.KindFailedLogins, at, event.FailedLogins{
		Address: "192.0.2.7",
		Count:   3,
	})

	t.Run("GIVEN a release", func(t *testing.T) {
		t.Run("WHEN rendered without an address THEN it links to its public page, with its cover", func(t *testing.T) {
			m := notify.Render(ctx, release, "en", "", w.catalog, w.covers)
			assert.Equal(t, "https://db.example/sequel", m.Link)
			assert.Equal(t, "Open the page", m.LinkLabel)
			require.Len(t, m.Images, 1)
			assert.Equal(t, "https://img.example/sequel.jpg", m.Images[0].URL)
			assert.Empty(t, m.Images[0].Data)
		})

		t.Run("WHEN rendered with an address THEN it links to the owned game", func(t *testing.T) {
			m := notify.Render(ctx, release, "en", address, w.catalog, w.covers)
			assert.Equal(t, address+"/#/game/g-1", m.Link)
			assert.Equal(t, "Open in Game Vault", m.LinkLabel)
		})
	})

	t.Run("GIVEN a deal WHEN rendered with an address THEN it keeps the deal's link and art", func(t *testing.T) {
		m := notify.Render(ctx, deal, "en", address, w.catalog, w.covers)
		assert.Equal(t, "https://deals.example/g", m.Link)
		require.Len(t, m.Images, 1)
		assert.Equal(t, "https://img.example/g.jpg", m.Images[0].URL)
	})

	t.Run("GIVEN attention and security events", func(t *testing.T) {
		t.Run("WHEN rendered with an address THEN they are important and link to their page", func(t *testing.T) {
			is := notify.Render(ctx, issue, "en", address, w.catalog, w.covers)
			assert.True(t, is.Important)
			assert.Equal(t, address+"/#/system/health", is.Link)

			ks := notify.Render(ctx, keys, "en", address, w.catalog, w.covers)
			assert.True(t, ks.Important)
			assert.Equal(t, address+"/#/library/expiring", ks.Link)

			ls := notify.Render(ctx, logins, "en", address, w.catalog, w.covers)
			assert.True(t, ls.Important)
		})

		t.Run("WHEN rendered without an address THEN they have no link", func(t *testing.T) {
			assert.Empty(t, notify.Render(ctx, issue, "en", "", w.catalog, w.covers).Link)
			assert.Empty(t, notify.Render(ctx, keys, "en", "", w.catalog, w.covers).Link)
		})
	})

	t.Run("GIVEN games added WHEN rendered THEN they are not important", func(t *testing.T) {
		m := notify.Render(ctx, event.New(event.KindCollectionAdded, at, event.CollectionAdded{
			Origin: event.OriginManual,
			Games:  []event.GameRef{ref("g-1", "Example Game")},
		}), "en", "", w.catalog, w.covers)
		assert.False(t, m.Important)
	})
}
