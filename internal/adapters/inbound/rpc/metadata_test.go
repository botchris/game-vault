package rpc_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
)

// TestGameDetailsChain covers what of the game sheets crosses the API: the shared key, the language,
// the refresh flag, the summary's credit, and a link change invalidating the cached sheet. How
// sheets merge and are cached is the media package's (TestGameDetails_*).
func TestGameDetailsChain(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})

	// Configuring the box art key once also configures (and enables) its details capability.
	if _, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
		Id:       "boxart",
		Enabled:  true,
		Settings: map[string]string{"api_key": "k"},
	})); err != nil {
		t.Fatal(err)
	}

	meta, err := c.providers.ListProviders(ctx, connect.NewRequest(&pb.ListProvidersRequest{Kind: "metadata"}))
	if err != nil || len(meta.Msg.Providers) != 2 || !meta.Msg.Providers[1].Enabled || meta.Msg.Providers[1].Settings["api_key"] != schema.SecretPlaceholder {
		t.Fatalf("details capability must inherit the shared key and be enabled: %+v %v", meta, err)
	}

	// A PS3 disc of a Steam game: the store sheet wins (localized), box art fills the publisher,
	// and both trailers are kept.
	g, _ := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Portal 2",
		Links: map[string]string{"steam": "620"},
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
			Platform: "PS3",
		}},
	}))

	res, err := c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if d := res.Msg.Details; d.Summary != "Un juego de puzles." || len(d.Sources) != 2 {
		t.Fatalf("the sheet must be in the asked language, from both providers: %+v", d)
	}

	// Refresh asks the providers again.
	detailsCalls := func(c clients) int { return c.boxDet.calls }
	before := detailsCalls(c)

	c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
		Refresh:  true,
	}))

	if detailsCalls(c) != before+1 {
		t.Fatal("refresh must ask the providers again")
	}

	// Changing the Steam AppID invalidates the cached sheet.
	c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
		Id:       g.Msg.Game.Id,
		Title:    "Portal 2",
		Links:    nil,
		LinksSet: true,
	}))

	res, _ = c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
	}))
	if res.Msg.Details.Summary != "English overview." {
		t.Fatalf("without AppID only the box art provider applies: %+v", res.Msg.Details)
	}

	if src := res.Msg.Details.SummarySource; src.GetName() != "Encyclopedia" || src.GetUrl() != "https://encyclopedia.test/portal-2" ||
		src.GetLicense() != "CC BY-SA 4.0" || src.GetLicenseUrl() != "https://creativecommons.org/licenses/by-sa/4.0/" {
		t.Fatalf("the summary's credit must reach the UI: %+v", src)
	}
}
