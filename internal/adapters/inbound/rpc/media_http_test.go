package rpc_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
)

func TestCoversAndLogs(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})

	created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Halo 3"}))
	if err != nil {
		t.Fatal(err)
	}

	id := created.Msg.Game.Id

	get := func() int {
		res, err := http.Get(c.baseURL + "/media/covers/" + id)
		if err != nil {
			t.Fatal(err)
		}

		res.Body.Close()

		return res.StatusCode
	}
	if code := get(); code != http.StatusNotFound {
		t.Fatalf("no app id and no custom cover: want 404, got %d", code)
	}

	// Link it to Steam via search: the portrait art "404s", the header fallback works and is cached.
	stores, err := c.games.ListLinkStores(ctx, connect.NewRequest(&pb.ListLinkStoresRequest{}))
	if err != nil || len(stores.Msg.Stores) != 1 || stores.Msg.Stores[0].Key != game.LinkSteam {
		t.Fatalf("link stores: %+v %v", stores, err)
	}

	found, err := c.games.SearchLinks(ctx, connect.NewRequest(&pb.SearchLinksRequest{
		Store: game.LinkSteam,
		Query: "halo 3",
	}))
	if err != nil || len(found.Msg.Matches) != 1 {
		t.Fatalf("search: %+v %v", found, err)
	}

	if _, err := c.games.SearchLinks(ctx, connect.NewRequest(&pb.SearchLinksRequest{
		Store: "nowhere",
		Query: "halo 3",
	})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("a store nobody searches: want NotFound, got %v", err)
	}

	if _, err := c.games.SearchLinks(ctx, connect.NewRequest(&pb.SearchLinksRequest{
		Store: game.LinkSteam,
		Query: "store offline",
	})); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("a store that does not answer: want Unavailable, got %v", err)
	}

	links := map[string]string{game.LinkSteam: found.Msg.Matches[0].Id}
	if _, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
		Id:       id,
		Title:    "Halo 3",
		Links:    links,
		LinksSet: true,
	})); err != nil {
		t.Fatal(err)
	}

	if code := get(); code != http.StatusOK {
		t.Fatalf("want cover, got %d", code)
	}

	before := c.images.fetched.Load()
	if code := get(); code != http.StatusOK || c.images.fetched.Load() != before {
		t.Fatalf("second request must come from the cache (fetched %d → %d)", before, c.images.fetched.Load())
	}

	// Invalid cover URLs are rejected by the domain.
	if _, err := c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{
		GameId: id,
		Details: &pb.CopyDetails{
			Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
			Platform: "Steam",
		},
	})); err != nil {
		t.Fatal(err)
	}

	_, err = c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
		GameId: id,
		System: "PC",
		Cover:  &pb.SetEditionCoverRequest_Url{Url: "file:///etc/passwd"},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for a file:// cover, got %v", err)
	}

	// Log settings round-trip, validation, and reading the current file through the API.
	if _, err := c.logs.UpdateLogSettings(ctx, connect.NewRequest(&pb.UpdateLogSettingsRequest{Settings: &pb.LogSettings{
		Level:         "debug",
		MaxFileSizeMb: 2,
		MaxFiles:      3,
	}})); err != nil {
		t.Fatal(err)
	}

	_, err = c.logs.UpdateLogSettings(ctx, connect.NewRequest(&pb.UpdateLogSettingsRequest{Settings: &pb.LogSettings{
		Level:         "loud",
		MaxFileSizeMb: 2,
		MaxFiles:      3,
	}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for a bad level, got %v", err)
	}

	got, err := c.logs.GetLogSettings(ctx, connect.NewRequest(&pb.GetLogSettingsRequest{}))
	if err != nil || got.Msg.Settings.MaxFiles != 3 || got.Msg.Settings.Level != "debug" {
		t.Fatalf("settings not saved: %+v %v", got, err)
	}

	c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{})) // produces a debug "rpc" line

	files, err := c.logs.ListLogFiles(ctx, connect.NewRequest(&pb.ListLogFilesRequest{}))
	if err != nil || len(files.Msg.Files) == 0 || !files.Msg.Files[0].Current {
		t.Fatalf("list logs: %+v %v", files, err)
	}

	file, err := c.logs.GetLogFile(ctx, connect.NewRequest(&pb.GetLogFileRequest{
		Name:      files.Msg.Files[0].Name,
		TailLines: 50,
	}))
	if err != nil || !strings.Contains(file.Msg.Content, "ListGames") {
		t.Fatalf("log should contain the RPC line: %v\n%s", err, file.Msg.Content)
	}

	_, err = c.logs.GetLogFile(ctx, connect.NewRequest(&pb.GetLogFileRequest{Name: "../gamevault.db"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("path traversal must be rejected, got %v", err)
	}
}

func TestImageProxyAllowlist(t *testing.T) {
	c := newServer(t, &fakeProvider{})

	get := func(u string) int {
		res, err := http.Get(c.baseURL + "/media/proxy?url=" + url.QueryEscape(u))
		if err != nil {
			t.Fatal(err)
		}

		res.Body.Close()

		return res.StatusCode
	}
	if code := get("https://boxart.test/PS3.png"); code != http.StatusOK {
		t.Fatalf("declared provider host: want 200, got %d", code)
	}

	for _, u := range []string{"https://evil.test/x.png", "http://boxart.test/PS3.png", "https://127.0.0.1/admin", "file:///etc/passwd"} {
		if code := get(u); code != http.StatusForbidden {
			t.Errorf("%s: want 403, got %d", u, code)
		}
	}
}

func TestSheetImagesAreStoredPerGame(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})
	g, _ := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Portal 2",
		Links: map[string]string{"steam": "620"},
	}))
	id := g.Msg.Game.Id

	res, err := c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   id,
		Language: "en",
	}))
	if err != nil {
		t.Fatal(err)
	}

	shot := res.Msg.Details.Screenshots[0]

	prefix := "/media/games/" + id + "/assets/"
	if !strings.HasPrefix(shot.FullUrl, prefix) || !strings.HasPrefix(shot.ThumbUrl, prefix) || !strings.HasPrefix(res.Msg.Details.Videos[0].ThumbnailUrl, prefix) {
		t.Fatalf("sheet images must point to local assets: %+v", res.Msg.Details)
	}

	get := func(path string) int {
		r, err := http.Get(c.baseURL + path)
		if err != nil {
			t.Fatal(err)
		}

		r.Body.Close()

		return r.StatusCode
	}
	if code := get(shot.FullUrl); code != http.StatusOK {
		t.Fatalf("asset: %d", code)
	}
	// The image now lives in the game's folder, next to assets.json, and is served offline.
	time.Sleep(200 * time.Millisecond) // background prefetch of the other images

	before := c.images.fetched.Load()
	if code := get(shot.FullUrl); code != http.StatusOK || c.images.fetched.Load() != before {
		t.Fatalf("second request must come from disk (fetched %d → %d)", before, c.images.fetched.Load())
	}
	// (Glob would read the "[id]" in the folder name as a character class.)
	files, err := os.ReadDir(filepath.Join(c.dataDir, "Portal 2 ["+id+"]"))
	if err != nil || len(files) < 3 { // assets.json + screenshot + thumb (+ poster)
		t.Fatalf("game folder contents: %v %v", files, err)
	}

	if code := get(prefix + "..%2F..%2Fgamevault.db"); code == http.StatusOK {
		t.Fatal("path traversal must not serve files")
	}

	// Deleting the game deletes its folder.
	c.games.DeleteGame(ctx, connect.NewRequest(&pb.DeleteGameRequest{Id: id}))

	entries, _ := os.ReadDir(c.dataDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), id) {
			t.Fatalf("folder left after delete: %s", e.Name())
		}
	}
}
