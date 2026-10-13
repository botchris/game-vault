package rpc_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	pb "gamevault/internal/gen/gamevault/v1"
)

func TestBarcodeScanFlow(t *testing.T) {
	ctx := context.Background()

	c := newServer(t, &fakeProvider{})
	if _, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
		Id:       "boxart",
		Enabled:  true,
		Settings: map[string]string{"api_key": "k"},
	})); err != nil {
		t.Fatal(err)
	}

	// A known PAL code: cleaned title, platform, and a cover suggestion from the box art provider.
	res, err := c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "3 307215 643006"}))
	if err != nil {
		t.Fatal(err)
	}

	m := res.Msg.Match
	if m == nil || m.Title != "Assassin's Creed III" || m.Platform != "PS3" || len(res.Msg.Suggestions) != 1 || res.Msg.Suggestions[0].Platform != "PS3" {
		t.Fatalf("identify: %+v", res.Msg)
	}

	// Save it with GameService, barcode included, pinning the suggested cover.
	s := res.Msg.Suggestions[0]

	created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: s.Title,
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
			Platform: s.Platform,
			Edition:  m.Edition,
			Barcode:  res.Msg.Barcode,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
		GameId: created.Msg.Game.Id,
		System: s.Platform,
		Cover:  &pb.SetEditionCoverRequest_Url{Url: s.CoverUrl},
	})); err != nil {
		t.Fatal(err)
	}

	// Scanning it again says "you already have it" without asking any provider.
	again, err := c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "3307215643006"}))
	if err != nil || len(again.Msg.Owned) != 1 || again.Msg.Owned[0].Game.Id != created.Msg.Game.Id || again.Msg.Match != nil {
		t.Fatalf("second scan: %+v %v", again.Msg, err)
	}

	// An unknown code (an invented Xbox 360 disc): no match; the user types the title.
	unknown, err := c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "2001234567893"}))
	if err != nil || unknown.Msg.Match != nil || len(unknown.Msg.Owned) != 0 {
		t.Fatalf("unknown code: %+v %v", unknown.Msg, err)
	}

	sugg, err := c.lookup.SuggestGames(ctx, connect.NewRequest(&pb.SuggestGamesRequest{
		Title:    "Example West",
		Platform: "xbox 360",
	}))
	if err != nil || len(sugg.Msg.Suggestions) != 1 || sugg.Msg.Suggestions[0].Platform != "Xbox 360" {
		t.Fatalf("suggest: %+v %v", sugg.Msg, err)
	}

	// The game exists already (e.g. on Steam): it is offered so the disc becomes one more copy.
	c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Example West"}))

	sugg, _ = c.lookup.SuggestGames(ctx, connect.NewRequest(&pb.SuggestGamesRequest{
		Title:    "example west",
		Platform: "Xbox 360",
	}))
	if len(sugg.Msg.Existing) != 1 {
		t.Fatalf("existing game not offered: %+v", sugg.Msg)
	}

	// A misread code is rejected.
	_, err = c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "2001234567894"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad check digit: want InvalidArgument, got %v", err)
	}
}
