package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
)

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()
	fake := &fakeProvider{copies: []game.ImportedCopy{
		{
			ExternalID: "fake:1",
			Title:      "Hades",
			Links:      game.Links{game.LinkSteam: "1145360"},
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
			},
		},
	}}
	c := newServer(t, fake)

	// A manual game with a key, then a source scan that brings the same game in the library.
	created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Hades",
		Links: map[string]string{"steam": "1145360"},
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_KEY,
			Platform: "Steam",
			Status:   pb.CopyStatus_COPY_STATUS_REVEALED,
			Key:      "AAAA-BBBB",
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	src, err := c.sources.CreateSource(ctx, connect.NewRequest(&pb.CreateSourceRequest{Source: &pb.SourceInput{
		Type:     "fake",
		Enabled:  true,
		Settings: map[string]string{"token": "s3cret"},
	}}))
	if err != nil {
		t.Fatal(err)
	}

	if got := src.Msg.Source.Settings["token"]; got != schema.SecretPlaceholder {
		t.Fatalf("secret leaked to client: %q", got)
	}

	synced, err := c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
	if err != nil {
		t.Fatal(err)
	}

	if r := synced.Msg.Source.LastSync; !r.Success || r.CopiesAdded != 1 || r.GamesCreated != 0 || synced.Msg.Source.CopyCount != 1 {
		t.Fatalf("unexpected sync report %+v", r)
	}

	got, err := c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: created.Msg.Game.Id}))
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Msg.Game.Copies) != 2 || !got.Msg.Game.Copies[0].Redundant {
		t.Fatalf("the key should now be flagged redundant: %+v", got.Msg.Game.Copies)
	}

	marked, err := c.games.MarkRedeemedKeys(ctx, connect.NewRequest(&pb.MarkRedeemedKeysRequest{}))
	if err != nil || marked.Msg.Updated != 1 {
		t.Fatalf("mark redeemed: %v %v", marked, err)
	}

	// Updating with the placeholder keeps the stored secret: the next sync still works.
	if _, err := c.sources.UpdateSource(ctx, connect.NewRequest(&pb.UpdateSourceRequest{
		Id: src.Msg.Source.Id,
		Source: &pb.SourceInput{
			Name:     "Renamed",
			Enabled:  true,
			Settings: map[string]string{"token": schema.SecretPlaceholder},
		},
	})); err != nil {
		t.Fatal(err)
	}

	test, err := c.sources.TestSource(ctx, connect.NewRequest(&pb.TestSourceRequest{Id: src.Msg.Source.Id}))
	if err != nil || !test.Msg.Success {
		t.Fatalf("test source after update: %+v %v", test, err)
	}

	// Validation errors surface as InvalidArgument.
	_, err = c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: " "}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}

	// CSV import + export + backup.
	imp, err := c.system.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("title;platform;kind\nHalo 3;Xbox 360;physical\n")}))
	if err != nil || imp.Msg.Report.GamesCreated != 1 {
		t.Fatalf("csv import: %+v %v", imp, err)
	}

	exp, err := c.system.ExportCsv(ctx, connect.NewRequest(&pb.ExportCsvRequest{}))
	if err != nil || len(exp.Msg.Content) == 0 {
		t.Fatalf("csv export: %v", err)
	}

	if _, err := c.system.CreateBackup(ctx, connect.NewRequest(&pb.CreateBackupRequest{})); err != nil {
		t.Fatal(err)
	}

	st, err := c.system.GetStatus(ctx, connect.NewRequest(&pb.GetStatusRequest{}))
	if err != nil || st.Msg.GameCount != 2 || st.Msg.CopyCount != 3 {
		t.Fatalf("status: %+v %v", st, err)
	}

	// Deleting the source releases its copies as manual ones.
	if _, err := c.sources.DeleteSource(ctx, connect.NewRequest(&pb.DeleteSourceRequest{Id: src.Msg.Source.Id})); err != nil {
		t.Fatal(err)
	}

	got, _ = c.games.GetGame(ctx, connect.NewRequest(&pb.GetGameRequest{Id: created.Msg.Game.Id}))
	if len(got.Msg.Game.Copies) != 2 || got.Msg.Game.Copies[1].SourceId != "" {
		t.Fatalf("copies should be kept as manual: %+v", got.Msg.Game.Copies)
	}
}

func TestCatalogIncludesGenres(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})
	g, _ := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Portal 2",
		Links: map[string]string{"steam": "620"},
	}))
	c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Halo 3"}))

	list, _ := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{Language: "es"}))
	if list.Msg.DetailsCached != 0 {
		t.Fatalf("no details yet: %d", list.Msg.DetailsCached)
	}

	c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
	}))

	list, _ = c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{Language: "es"}))
	if list.Msg.DetailsCached != 1 {
		t.Fatalf("details cached: %d", list.Msg.DetailsCached)
	}

	for _, x := range list.Msg.Games {
		if x.Id == g.Msg.Game.Id && (len(x.Genres) != 1 || x.Genres[0] != "Puzzle") {
			t.Fatalf("genres missing from the catalog: %+v", x)
		}
	}
}

func TestPhysicalCopyDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		t.Run("WHEN a game is created with a graded, priced physical copy", func(t *testing.T) {
			res, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
				Title: "Halo 3",
				Copies: []*pb.CopyDetails{{
					Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
					Platform: "Xbox 360",
					Grade:    pb.CopyGrade_COPY_GRADE_VERY_GOOD,
					Contents: []pb.CopyContent{pb.CopyContent_COPY_CONTENT_MEDIA, pb.CopyContent_COPY_CONTENT_BOX},
					Price: &pb.Money{
						AmountMinor: 2995,
						Currency:    "eur",
					},
				}},
			}))
			require.NoError(t, err)

			t.Run("THEN it comes back normalized", func(t *testing.T) {
				d := res.Msg.Game.Copies[0].Details
				assert.Equal(t, pb.CopyGrade_COPY_GRADE_VERY_GOOD, d.Grade)
				assert.Equal(t, []pb.CopyContent{pb.CopyContent_COPY_CONTENT_BOX, pb.CopyContent_COPY_CONTENT_MEDIA}, d.Contents)
				assert.Equal(t, int64(2995), d.Price.GetAmountMinor())
				assert.Equal(t, "EUR", d.Price.GetCurrency())
			})
		})

		t.Run("WHEN the default currency is set to an invalid code", func(t *testing.T) {
			_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{
				Preferences: &pb.Preferences{Currency: "EURO"},
			}))

			t.Run("THEN it is refused as invalid", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN it is set to gbp", func(t *testing.T) {
			_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{
				Preferences: &pb.Preferences{Currency: "gbp"},
			}))
			require.NoError(t, err)

			t.Run("THEN it is read back upper-cased", func(t *testing.T) {
				got, err := c.system.GetPreferences(ctx, connect.NewRequest(&pb.GetPreferencesRequest{}))
				require.NoError(t, err)
				assert.Equal(t, "GBP", got.Msg.Preferences.GetCurrency())
			})
		})
	})
}
