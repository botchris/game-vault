package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/logfile"
	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/app"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/config"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
	"gamevault/internal/domain/source"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// fakeProvider stands in for a real store so the whole stack can be exercised offline.
type fakeProvider struct{ copies []game.ImportedCopy }

func (f *fakeProvider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "fake",
		Name: "Fake store",
		Fields: []schema.Field{
			{
				Key:      "token",
				Kind:     schema.FieldSecret,
				Required: true,
			},
		},
	}
}

func (f *fakeProvider) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	return f.copies, nil, nil
}

// Test checks the token the way a real source would check its credentials.
func (f *fakeProvider) Test(_ context.Context, s schema.Settings) error {
	if s["token"] == "" {
		return errors.New("no token")
	}

	return nil
}

// fakeImages serves a 1×1 PNG for any URL ending in ".png" and fails otherwise. Its count is
// atomic: the background prefetch of sheet images fetches while the test reads it.
type fakeImages struct{ fetched atomic.Int32 }

var png1x1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *fakeImages) Fetch(_ context.Context, url string) (media.Image, error) {
	f.fetched.Add(1)

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

func (fakeSteamStore) Applies(q media.GameQuery) bool {
	return q.ForPC() && q.Links[game.LinkSteam] != ""
}

func (fakeSteamStore) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
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
		title, platform, edition := game.ParseProductName("Assassin's Creed Iii Ed. Special Ps3(sp)")

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

// fakeBoxArt is a keyed provider for physical copies; it records how often it is asked, and while
// limited is set it answers that error (a rate limit or a used-up allowance) instead.
type fakeBoxArt struct {
	asked   int
	limited error
}

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

func (fakeStoreDetails) Applies(q media.GameQuery) bool { return q.Links[game.LinkSteam] != "" }

func (fakeStoreDetails) Details(_ context.Context, q media.GameQuery, lang string, _ schema.Settings) (*media.GameDetails, error) {
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

func (*fakeBoxDetails) Applies(q media.GameQuery) bool { return q.HasPhysical() }

func (f *fakeBoxDetails) Details(_ context.Context, q media.GameQuery, _ string, s schema.Settings) (*media.GameDetails, error) {
	f.calls++

	if s["api_key"] != "k" {
		return nil, errors.New("bad key")
	}

	return &media.GameDetails{
		Summary: "English overview.",
		SummarySource: media.Attribution{
			Name:       "Encyclopedia",
			URL:        "https://encyclopedia.test/portal-2",
			License:    "CC BY-SA 4.0",
			LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		},
		Publishers: []string{"Valve"},
		Videos:     []media.Video{{YouTubeID: "abc123"}},
	}, nil
}

func (*fakeBoxArt) Applies(q media.GameQuery) bool { return q.HasPhysical() }

func (*fakeBoxArt) ImageHosts() []string { return []string{"boxart.test"} }

func (f *fakeBoxArt) Covers(_ context.Context, q media.GameQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	f.asked++

	if f.limited != nil {
		return nil, f.limited
	}

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

func (fakeSteamStore) SearchLinks(_ context.Context, query string) ([]media.LinkMatch, error) {
	if query == "store offline" {
		return nil, errors.New("connection refused")
	}

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
	valuation gamevaultv1connect.ValuationServiceClient
	fields    gamevaultv1connect.FieldServiceClient
	sources   gamevaultv1connect.SourceServiceClient
	system    gamevaultv1connect.SystemServiceClient
	logs      gamevaultv1connect.LogServiceClient
	health    gamevaultv1connect.HealthServiceClient
	ownership gamevaultv1connect.OwnershipServiceClient
	baseURL   string
	images    *fakeImages
	dataDir   string
	photosDir string
}

// testPlugins are the fakes the end-to-end tests run instead of real integrations.
type testPlugins struct {
	sources    []sync.Provider
	providers  []providers.Impl
	valuations []valuation.Provider
}

func (p testPlugins) Sources() []sync.Provider { return p.sources }

func (p testPlugins) Providers() []providers.Impl { return p.providers }

func (p testPlugins) Valuations() []valuation.Provider { return p.valuations }

// newServer builds Game Vault the way the server does (app.Build), on a database and folders in a
// temporary directory, with p as its only source and fakes for every provider, and serves it.
func newServer(t *testing.T, p sync.Provider) clients {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	dir := t.TempDir()

	db, err := sqlite.Open(ctx, filepath.Join(dir, "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	level := new(slog.LevelVar)

	logFiles, err := logfile.Open(filepath.Join(dir, "logs"), level, settings.DefaultLogging())
	require.NoError(t, err)
	t.Cleanup(func() { logFiles.Close() })

	log := slog.New(slog.NewTextHandler(logFiles, &slog.HandlerOptions{Level: level}))
	images := &fakeImages{}
	boxart := &fakeBoxArt{}
	boxDet := &fakeBoxDetails{}

	vault, err := app.Build(ctx, config.Config{
		ConfigDir:  dir,
		BackupKeep: 3,
	}, db, logFiles, log,
		app.WithPlugins(testPlugins{
			sources:    []sync.Provider{p},
			providers:  []providers.Impl{fakeSteamStore{}, boxart, &fakeBarcodes{}, fakeStoreDetails{}, boxDet, fakePrices{}},
			valuations: []valuation.Provider{fakePrices{}},
		}),
		app.WithImageFetcher(images),
		app.WithHasher(passwordhash.Bcrypt{Cost: 4}), // the lowest cost: tests sign in often
		app.WithDiskSpace(nil),                       // the test machine's free space is not under test
		app.WithVersion("test"),
	)
	require.NoError(t, err)

	srv := httptest.NewServer(vault.Handler)
	t.Cleanup(srv.Close)

	return clients{
		games:     gamevaultv1connect.NewGameServiceClient(http.DefaultClient, srv.URL),
		sources:   gamevaultv1connect.NewSourceServiceClient(http.DefaultClient, srv.URL),
		system:    gamevaultv1connect.NewSystemServiceClient(http.DefaultClient, srv.URL),
		logs:      gamevaultv1connect.NewLogServiceClient(http.DefaultClient, srv.URL),
		health:    gamevaultv1connect.NewHealthServiceClient(http.DefaultClient, srv.URL),
		ownership: gamevaultv1connect.NewOwnershipServiceClient(http.DefaultClient, srv.URL),
		baseURL:   srv.URL,
		providers: gamevaultv1connect.NewProviderServiceClient(http.DefaultClient, srv.URL),
		covers:    gamevaultv1connect.NewCoverServiceClient(http.DefaultClient, srv.URL),
		lookup:    gamevaultv1connect.NewLookupServiceClient(http.DefaultClient, srv.URL),
		metadata:  gamevaultv1connect.NewMetadataServiceClient(http.DefaultClient, srv.URL),
		valuation: gamevaultv1connect.NewValuationServiceClient(http.DefaultClient, srv.URL),
		fields:    gamevaultv1connect.NewFieldServiceClient(http.DefaultClient, srv.URL),
		boxart:    boxart,
		boxDet:    boxDet,
		images:    images,
		dataDir:   filepath.Join(dir, "game-data"),
		photosDir: filepath.Join(dir, "photos"),
	}
}

// fakePrices is a price provider that knows one barcode.
// blockedPrices is a barcode fakePrices answers with a bot check, a provider-level problem.
const blockedPrices = game.Barcode("4006381333931")

type fakePrices struct{}

func (fakePrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "fake-prices",
		Kind:             provider.KindValuation,
		Name:             "Fake prices",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (fakePrices) Test(context.Context, schema.Settings) error { return nil }

func (fakePrices) Estimate(_ context.Context, _ schema.Settings, code game.Barcode) (game.Estimate, error) {
	if code == blockedPrices {
		return game.Estimate{}, fmt.Errorf("asking for a browser check: %w", provider.ErrUnavailable)
	}

	if code != "5030934110075" {
		return game.Estimate{}, valuation.ErrNotListed
	}

	return game.Estimate{
		Sell: game.Money{
			Amount:   2000,
			Currency: "EUR",
		},
		BuyCash: game.Money{
			Amount:   600,
			Currency: "EUR",
		},
	}, nil
}
