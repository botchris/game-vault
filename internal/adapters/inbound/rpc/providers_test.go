package rpc_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
)

// TestCoverProviderChain covers the providers API: settings, secrets, reordering and the cover
// endpoint. Which provider the chain asks for which edition is the media package's (TestEditionCovers).
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

	// A PS3 disc of a Steam game: its edition is PS3, so only box art proposes; Steam is for its PC edition.
	cands, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Msg.Game.Id}))
	if err != nil || len(cands.Msg.Candidates) != 1 || cands.Msg.Candidates[0].ProviderName != "Box art" {
		t.Fatalf("candidates: %+v %v", cands, err)
	}

	res, err := http.Get(c.baseURL + "/media/covers/" + g.Msg.Game.Id)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("cover via box art provider: %v %v", res.StatusCode, err)
	}

	res.Body.Close()

	// Pinning a candidate = choosing it as the edition's cover.
	if _, err := c.games.SetEditionCover(ctx, connect.NewRequest(&pb.SetEditionCoverRequest{
		GameId: g.Msg.Game.Id,
		System: "PS3",
		Cover:  &pb.SetEditionCoverRequest_Url{Url: cands.Msg.Candidates[0].Url},
	})); err != nil {
		t.Fatal(err)
	}

	if r, err := c.covers.RefreshCovers(ctx, connect.NewRequest(&pb.RefreshCoversRequest{MissingOnly: true})); err != nil || r.Msg.Games != 0 {
		t.Fatalf("refresh missing: %+v %v", r, err)
	}
}

// allowanceUsedUp is a quota error that knows when the allowance renews.
type allowanceUsedUp struct{ until time.Time }

func (e allowanceUsedUp) Error() string { return "the allowance is used up until it renews" }

func (e allowanceUsedUp) Unwrap() error { return provider.ErrQuotaExhausted }

func (e allowanceUsedUp) ResumesAt() time.Time { return e.until }

func TestProviderStatusAndHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a box art service whose allowance is used up, and a PS3 disc", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		_, err := c.providers.UpdateProvider(ctx, connect.NewRequest(&pb.UpdateProviderRequest{
			Id:       "boxart",
			Enabled:  true,
			Settings: map[string]string{"api_key": "k"},
		}))
		require.NoError(t, err)

		g, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Halo 3",
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_PHYSICAL,
				Platform: "PS3",
			}},
		}))
		require.NoError(t, err)

		renews := time.Now().Add(27 * 24 * time.Hour).Truncate(time.Second)
		c.boxart.limited = allowanceUsedUp{until: renews}

		t.Run("WHEN the cover picker lists candidates", func(t *testing.T) {
			res, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Msg.Game.Id}))
			require.NoError(t, err)

			t.Run("THEN it shows no warning about the provider, only that providers are unavailable", func(t *testing.T) {
				assert.Empty(t, res.Msg.Warnings)
				assert.True(t, res.Msg.ProvidersUnavailable)
			})

			t.Run("AND the provider's status in the chain reports the used-up allowance until it renews", func(t *testing.T) {
				list, err := c.providers.ListProviders(ctx, connect.NewRequest(&pb.ListProvidersRequest{Kind: "cover"}))
				require.NoError(t, err)

				var s *pb.ProviderStatus

				for _, p := range list.Msg.Providers {
					if p.Id == "boxart" {
						s = p.Status
					} else {
						assert.Nil(t, p.Status, p.Id)
					}
				}

				require.NotNil(t, s)
				assert.Equal(t, "boxart", s.ProviderId)
				assert.Equal(t, "Box art", s.ProviderName)
				assert.Contains(t, s.ProviderIds, "boxart")
				assert.Equal(t, "quota", s.Kind)
				assert.Equal(t, "the allowance is used up until it renews", s.Message)
				assert.True(t, s.Until.AsTime().Equal(renews))
				assert.NotNil(t, s.Since)
			})

			t.Run("AND the health lists it as a warning pointing to Providers", func(t *testing.T) {
				res, err := c.health.GetHealth(ctx, connect.NewRequest(&pb.GetHealthRequest{}))
				require.NoError(t, err)
				require.Len(t, res.Msg.Issues, 1)

				i := res.Msg.Issues[0]
				assert.Equal(t, "provider:boxart", i.Key)
				assert.Equal(t, "warning", i.Level)
				assert.Equal(t, "provider", i.Kind)
				assert.Equal(t, "Box art", i.Source)
				assert.Equal(t, "the allowance is used up until it renews", i.Message)
				assert.Equal(t, "providers", i.Target)
				assert.Equal(t, "provider_quota", i.Reason)
				assert.NotEmpty(t, i.Params["until"])
				assert.NotNil(t, i.Since)
			})
		})

		t.Run("WHEN the service answers again", func(t *testing.T) {
			c.boxart.limited = nil

			_, err := c.covers.ListCoverCandidates(ctx, connect.NewRequest(&pb.ListCoverCandidatesRequest{GameId: g.Msg.Game.Id}))
			require.NoError(t, err)

			t.Run("THEN no status is reported, and the health is clear", func(t *testing.T) {
				list, err := c.providers.ListProviders(ctx, connect.NewRequest(&pb.ListProvidersRequest{Kind: "cover"}))
				require.NoError(t, err)

				for _, p := range list.Msg.Providers {
					assert.Nil(t, p.Status, p.Id)
				}

				res, err := c.health.GetHealth(ctx, connect.NewRequest(&pb.GetHealthRequest{}))
				require.NoError(t, err)
				assert.Empty(t, res.Msg.Issues)
			})
		})
	})
}
