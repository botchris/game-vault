package rpc_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/inbound/rpc"
	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/photostore"
	"gamevault/internal/adapters/outbound/sqlite"
	appauth "gamevault/internal/application/auth"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/logs"
	"gamevault/internal/application/media"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/system"
	"gamevault/internal/application/transfer"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
	"gamevault/internal/domain/source"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// fakeProvider stands in for a real store so the whole stack can be exercised offline.
type fakeProvider struct{ copies []game.ImportedCopy }

func (f *fakeProvider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "fake",
		Name: "Fake store",
		Fields: []source.Field{
			{
				Key:      "token",
				Kind:     source.FieldSecret,
				Required: true,
			},
		},
	}
}

func (f *fakeProvider) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	return f.copies, nil, nil
}

// Test checks the token the way a real source would check its credentials.
func (f *fakeProvider) Test(_ context.Context, s source.Settings) error {
	if s["token"] == "" {
		return errors.New("no token")
	}

	return nil
}

// fakeImages serves a 1×1 PNG for any URL ending in ".png" and fails otherwise.
type fakeImages struct{ fetched int }

var png1x1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *fakeImages) Fetch(_ context.Context, url string) (media.Image, error) {
	f.fetched++

	if strings.HasSuffix(url, ".png") {
		return media.Image{
			Data:        png1x1,
			ContentType: "image/png",
		}, nil
	}

	return media.Image{}, errors.New("404")
}

type fakeSteamStore struct{}

func (fakeSteamStore) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "steam",
		Kind:             provider.KindCover,
		Name:             "Steam",
		EnabledByDefault: true,
	}
}

func (fakeSteamStore) Test(context.Context, schema.Settings) error { return nil }

func (fakeSteamStore) Applies(q media.CoverQuery) bool { return q.Links[game.LinkSteam] != "" }

func (fakeSteamStore) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	return []media.CoverCandidate{
		{
			URL:      "https://steam.test/" + q.Links[game.LinkSteam] + "/library.jpg",
			Provider: "steam",
		},
		{
			URL:      "https://steam.test/" + q.Links[game.LinkSteam] + "/header.png",
			Provider: "steam",
		},
	}, nil
}

// fakeBarcodes knows one PAL code, like UPCitemdb with Assassin's Creed III.
type fakeBarcodes struct{}

func (*fakeBarcodes) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "barcodes",
		Kind:             provider.KindBarcode,
		Name:             "Barcodes",
		EnabledByDefault: true,
	}
}

func (*fakeBarcodes) Test(context.Context, schema.Settings) error { return nil }

func (*fakeBarcodes) Lookup(_ context.Context, code game.Barcode, _ schema.Settings) ([]media.BarcodeMatch, error) {
	if code == "3307215643006" {
		title, platform, edition := media.CleanProductTitle("Assassin's Creed Iii Ed. Special Ps3(sp)")

		return []media.BarcodeMatch{{
			Raw:      "Assassin's Creed Iii Ed. Special Ps3(sp)",
			Title:    title,
			Platform: platform,
			Edition:  edition,
			Provider: "barcodes",
		}}, nil
	}

	return nil, nil
}

// fakeBoxArt is a keyed provider for physical copies; it records how often it is asked.
type fakeBoxArt struct{ asked int }

func (*fakeBoxArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:            "boxart",
		Kind:          provider.KindCover,
		Name:          "Box art",
		SettingsGroup: "boxart",
		Fields: schema.Fields{{
			Key:      "api_key",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

func (*fakeBoxArt) Test(context.Context, schema.Settings) error { return nil }

// fakeStoreDetails is a localized store sheet for Steam games (like the Steam store).
type fakeStoreDetails struct{}

func (fakeStoreDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "store-details",
		Kind:             provider.KindMetadata,
		Name:             "Store",
		EnabledByDefault: true,
	}
}

func (fakeStoreDetails) Test(context.Context, schema.Settings) error { return nil }

func (fakeStoreDetails) Applies(q media.CoverQuery) bool { return q.Links[game.LinkSteam] != "" }

func (fakeStoreDetails) Details(_ context.Context, q media.CoverQuery, lang string, _ schema.Settings) (*media.GameDetails, error) {
	summary := "A puzzle game."
	if lang == "es" {
		summary = "Un juego de puzles."
	}

	return &media.GameDetails{
		Summary: summary,
		Genres:  []string{"Puzzle"},
		Videos: []media.Video{{
			HLSURL:    "https://video.test/trailer.m3u8",
			Thumbnail: "https://img.test/poster.png",
		}},
		Screenshots: []media.Screenshot{{
			ThumbURL: "https://img.test/shot-thumb.png",
			FullURL:  "https://img.test/shot.png",
		}},
	}, nil
}

// fakeBoxDetails shares the box art key (same settings group) and adds publisher + YouTube trailer.
type fakeBoxDetails struct{ calls int }

func (*fakeBoxDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:            "boxart-details",
		Kind:          provider.KindMetadata,
		Name:          "Box art details",
		SettingsGroup: "boxart",
		Fields: schema.Fields{{
			Key:      "api_key",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

func (*fakeBoxDetails) Test(context.Context, schema.Settings) error { return nil }

func (*fakeBoxDetails) Applies(q media.CoverQuery) bool { return q.HasPhysical() }

func (f *fakeBoxDetails) Details(_ context.Context, q media.CoverQuery, _ string, s schema.Settings) (*media.GameDetails, error) {
	f.calls++

	if s["api_key"] != "k" {
		return nil, errors.New("bad key")
	}

	return &media.GameDetails{
		Summary:    "English overview.",
		Publishers: []string{"Valve"},
		Videos:     []media.Video{{YouTubeID: "abc123"}},
	}, nil
}

func (*fakeBoxArt) Applies(q media.CoverQuery) bool { return q.HasPhysical() }

func (*fakeBoxArt) ImageHosts() []string { return []string{"boxart.test"} }

func (f *fakeBoxArt) Covers(_ context.Context, q media.CoverQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	f.asked++

	if s["api_key"] != "k" {
		return nil, errors.New("bad key")
	}

	return []media.CoverCandidate{{
		URL:      "https://boxart.test/" + q.PhysicalPlatforms[0] + ".png",
		Label:    q.Title,
		Provider: "boxart",
	}}, nil
}

func (fakeSteamStore) LinkStore() game.Store {
	return game.Store{
		Key:     game.LinkSteam,
		Name:    "Steam",
		PageURL: "https://steam.test/app/{id}",
	}
}

func (fakeSteamStore) SearchLinks(context.Context, string) ([]media.LinkMatch, error) {
	return []media.LinkMatch{{
		ID:   "1064271",
		Name: "Halo 3",
	}}, nil
}

type clients struct {
	games     gamevaultv1connect.GameServiceClient
	providers gamevaultv1connect.ProviderServiceClient
	covers    gamevaultv1connect.CoverServiceClient
	lookup    gamevaultv1connect.LookupServiceClient
	boxart    *fakeBoxArt
	boxDet    *fakeBoxDetails
	metadata  gamevaultv1connect.MetadataServiceClient
	sources   gamevaultv1connect.SourceServiceClient
	system    gamevaultv1connect.SystemServiceClient
	logs      gamevaultv1connect.LogServiceClient
	baseURL   string
	images    *fakeImages
	dataDir   string
	photosDir string
}

func newServer(t *testing.T, p sync.Provider) clients {
	t.Helper()

	ctx := context.Background()
	dir := t.TempDir()

	db, err := sqlite.Open(ctx, filepath.Join(dir, "gamevault.db"), "")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })

	level := new(slog.LevelVar)

	logFiles, err := logfile.Open(filepath.Join(dir, "logs"), level, settings.DefaultLogging())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { logFiles.Close() })

	log := slog.New(slog.NewTextHandler(logFiles, &slog.HandlerOptions{Level: level}))
	games, sources := sqlite.NewGameRepository(db), sqlite.NewSourceRepository(db)

	covers, err := gamedata.Open(filepath.Join(dir, "game-data"))
	if err != nil {
		t.Fatal(err)
	}

	photos, err := photostore.Open(filepath.Join(dir, "photos"))
	if err != nil {
		t.Fatal(err)
	}

	images := &fakeImages{}
	boxart := &fakeBoxArt{}
	boxDet := &fakeBoxDetails{}
	mediaSvc := media.NewService(games, sqlite.NewProviderRepository(db), covers, photos, sqlite.NewDetailsStore(db), images, time.Now, log,
		media.Providers{
			Covers:   []media.CoverProvider{fakeSteamStore{}, boxart},
			Barcodes: []media.BarcodeProvider{&fakeBarcodes{}},
			Metadata: []media.MetadataProvider{fakeStoreDetails{}, boxDet},
		})
	logsSvc := logs.NewService(sqlite.NewSettingsRepository(db), logFiles, logFiles)
	authSvc := appauth.NewService(sqlite.NewAuthRepository(db), sqlite.NewSettingsRepository(db), passwordhash.Bcrypt{Cost: 4}, noCerts{}, time.Now, log)
	syncSvc := sync.NewService(sources, games, db, time.Now, log, p)
	h := rpc.NewHTTPHandler(rpc.Handlers{
		Auth:        rpc.NewAuthHandler(authSvc),
		AuthService: authSvc,
		Logs:        rpc.NewLogHandler(logsSvc),
		Media:       mediaSvc,
		MediaRPC:    rpc.NewMediaHandler(mediaSvc),
		Games:       rpc.NewGameHandler(catalog.NewService(games, db, time.Now, mediaSvc, photos), mediaSvc, syncSvc),
		Sources:     rpc.NewSourceHandler(syncSvc),
		System: rpc.NewSystemHandler(
			system.NewService(games, db, sqlite.NewSettingsRepository(db), time.Now, log, system.Status{Version: "test"}, filepath.Join(dir, "backups"), 3),
			transfer.NewService(games, db, sqlite.NewSettingsRepository(db), time.Now, csvfile.Codec{})),
	}, rpc.Options{Log: log})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return clients{
		games:     gamevaultv1connect.NewGameServiceClient(http.DefaultClient, srv.URL),
		sources:   gamevaultv1connect.NewSourceServiceClient(http.DefaultClient, srv.URL),
		system:    gamevaultv1connect.NewSystemServiceClient(http.DefaultClient, srv.URL),
		logs:      gamevaultv1connect.NewLogServiceClient(http.DefaultClient, srv.URL),
		baseURL:   srv.URL,
		providers: gamevaultv1connect.NewProviderServiceClient(http.DefaultClient, srv.URL),
		covers:    gamevaultv1connect.NewCoverServiceClient(http.DefaultClient, srv.URL),
		lookup:    gamevaultv1connect.NewLookupServiceClient(http.DefaultClient, srv.URL),
		metadata:  gamevaultv1connect.NewMetadataServiceClient(http.DefaultClient, srv.URL),
		boxart:    boxart,
		boxDet:    boxDet,
		images:    images,
		dataDir:   filepath.Join(dir, "game-data"),
		photosDir: filepath.Join(dir, "photos"),
	}
}

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

	if got := src.Msg.Source.Settings["token"]; got != source.SecretPlaceholder {
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
			Settings: map[string]string{"token": source.SecretPlaceholder},
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

	links := map[string]string{game.LinkSteam: found.Msg.Matches[0].Id}
	if _, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
		Id:    id,
		Title: "Halo 3",
		Links: links,
	})); err != nil {
		t.Fatal(err)
	}

	if code := get(); code != http.StatusOK {
		t.Fatalf("want cover, got %d", code)
	}

	before := c.images.fetched
	if code := get(); code != http.StatusOK || c.images.fetched != before {
		t.Fatalf("second request must come from the cache (fetched %d → %d)", before, c.images.fetched)
	}

	// Invalid cover URLs are rejected by the domain.
	_, err = c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
		Id:       id,
		Title:    "Halo 3",
		CoverUrl: "file:///etc/passwd",
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

func TestCoverProviderChain(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})

	list, err := c.providers.ListProviders(ctx, connect.NewRequest(&pb.ListProvidersRequest{Kind: "cover"}))
	if err != nil || len(list.Msg.Providers) != 2 {
		t.Fatalf("providers: %+v %v", list, err)
	}

	for _, p := range list.Msg.Providers {
		if p.Id == "boxart" && p.Enabled {
			t.Fatal("a provider needing a key must start disabled")
		}
	}
	// Enabling without the key fails; with it works, and the key never comes back.
	_, err = c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
		Id:      "boxart",
		Enabled: true,
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}

	up, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
		Id:       "boxart",
		Enabled:  true,
		Settings: map[string]string{"api_key": "k"},
	}))
	if err != nil || up.Msg.Provider.Settings["api_key"] != schema.SecretPlaceholder {
		t.Fatalf("update: %+v %v", up, err)
	}

	test, err := c.providers.TestProvider(ctx, connect.NewRequest(&pb.TestProviderRequest{Id: "boxart"}))
	if err != nil || !test.Msg.Success {
		t.Fatalf("test: %+v %v", test, err)
	}

	// A PS3 disc of a Steam game: box art first after reordering, Steam as fallback.
	g, _ := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Portal 2",
		Links: map[string]string{"steam": "620"},
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
			Platform: "PS3",
		}},
	}))

	re, err := c.providers.ReorderProviders(ctx, connect.NewRequest(&pb.ReorderProvidersRequest{
		Kind: "cover",
		Ids:  []string{"boxart", "steam"},
	}))
	if err != nil || re.Msg.Providers[0].Id != "boxart" {
		t.Fatalf("reorder: %+v %v", re, err)
	}

	cands, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Msg.Game.Id}))
	if err != nil || len(cands.Msg.Candidates) != 3 || cands.Msg.Candidates[0].ProviderName != "Box art" {
		t.Fatalf("candidates: %+v %v", cands, err)
	}

	res, err := http.Get(c.baseURL + "/media/covers/" + g.Msg.Game.Id)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("cover via box art provider: %v %v", res.StatusCode, err)
	}

	res.Body.Close()

	// A Steam-only game never asks the keyed provider.
	asked := c.boxart.asked
	s, _ := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Hades",
		Links: map[string]string{"steam": "1145360"},
	}))
	http.Get(c.baseURL + "/media/covers/" + s.Msg.Game.Id)

	if c.boxart.asked != asked {
		t.Fatal("the box art provider must not be asked for games without physical copies")
	}

	// Pinning a candidate = setting it as the custom cover.
	if _, err := c.games.UpdateGame(ctx, connect.NewRequest(&pb.UpdateGameRequest{
		Id:       g.Msg.Game.Id,
		Title:    "Portal 2",
		Links:    map[string]string{"steam": "620"},
		CoverUrl: cands.Msg.Candidates[1].Url,
	})); err != nil {
		t.Fatal(err)
	}

	if r, err := c.covers.RefreshCovers(ctx, connect.NewRequest(&pb.RefreshCoversRequest{MissingOnly: true})); err != nil || r.Msg.Games != 0 {
		t.Fatalf("refresh missing: %+v %v", r, err)
	}
}

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
		Title:    s.Title,
		CoverUrl: s.CoverUrl,
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

	// Scanning it again says "you already have it" without asking any provider.
	again, err := c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "3307215643006"}))
	if err != nil || len(again.Msg.Owned) != 1 || again.Msg.Owned[0].Game.Id != created.Msg.Game.Id || again.Msg.Match != nil {
		t.Fatalf("second scan: %+v %v", again.Msg, err)
	}

	// An unknown code (Red Dead Redemption, Xbox 360 PAL): no match; the user types the title.
	unknown, err := c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "5026555255042"}))
	if err != nil || unknown.Msg.Match != nil || len(unknown.Msg.Owned) != 0 {
		t.Fatalf("unknown code: %+v %v", unknown.Msg, err)
	}

	sugg, err := c.lookup.SuggestGames(ctx, connect.NewRequest(&pb.SuggestGamesRequest{
		Title:    "Red Dead Redemption",
		Platform: "xbox 360",
	}))
	if err != nil || len(sugg.Msg.Suggestions) != 1 || sugg.Msg.Suggestions[0].Platform != "Xbox 360" {
		t.Fatalf("suggest: %+v %v", sugg.Msg, err)
	}

	// The game exists already (e.g. on Steam): it is offered so the disc becomes one more copy.
	c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{Title: "Red Dead Redemption"}))

	sugg, _ = c.lookup.SuggestGames(ctx, connect.NewRequest(&pb.SuggestGamesRequest{
		Title:    "red dead redemption",
		Platform: "Xbox 360",
	}))
	if len(sugg.Msg.Existing) != 1 {
		t.Fatalf("existing game not offered: %+v", sugg.Msg)
	}

	// A misread code is rejected.
	_, err = c.lookup.IdentifyBarcode(ctx, connect.NewRequest(&pb.IdentifyBarcodeRequest{Barcode: "5026555255043"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad check digit: want InvalidArgument, got %v", err)
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

	d := res.Msg.Details
	if d.Summary != "Un juego de puzles." || len(d.Publishers) != 1 || len(d.Videos) != 2 || len(d.Sources) != 2 {
		t.Fatalf("merged details: %+v", d)
	}

	// Cached: no second call; refresh asks again.
	detailsCalls := func(c clients) int { return c.boxDet.calls }
	before := detailsCalls(c)
	c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
	}))

	if detailsCalls(c) != before {
		t.Fatal("second view must come from the cache")
	}

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
		Id:    g.Msg.Game.Id,
		Title: "Portal 2",
		Links: nil,
	}))

	res, _ = c.metadata.GetGameDetails(ctx, connect.NewRequest(&pb.GetGameDetailsRequest{
		GameId:   g.Msg.Game.Id,
		Language: "es",
	}))
	if res.Msg.Details.Summary != "English overview." {
		t.Fatalf("without AppID only the box art provider applies: %+v", res.Msg.Details)
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

	before := c.images.fetched
	if code := get(shot.FullUrl); code != http.StatusOK || c.images.fetched != before {
		t.Fatalf("second request must come from disk (fetched %d → %d)", before, c.images.fetched)
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

// noCerts ignores the certificate validation setting in tests.
type noCerts struct{}

func (noCerts) SetCertificateValidation(auth.CertificateValidation) {}

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

func TestImportCsv_defaultCurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{Preferences: &pb.Preferences{Currency: "GBP"}}))
	require.NoError(t, err)

	t.Run("WHEN a CSV row has a price without currency", func(t *testing.T) {
		_, err := c.system.ImportCsv(ctx, connect.NewRequest(&pb.ImportCsvRequest{Content: []byte("title,kind,price\nOkami,physical,12.50\n")}))
		require.NoError(t, err)

		t.Run("THEN it gets the default currency", func(t *testing.T) {
			list, err := c.games.ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Games, 1)
			assert.Equal(t, "GBP", list.Msg.Games[0].Copies[0].Details.Price.GetCurrency())
		})
	})
}
