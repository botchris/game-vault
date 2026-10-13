package thegamesdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/schema"
)

// platformsServer is a fake TheGamesDB whose platform list can fail or hang, and whose searches
// record their platform filter.
type platformsServer struct {
	*httptest.Server

	mu sync.Mutex

	// fail makes the platform list answer HTTP 500.
	fail bool

	// gate, when set, holds the platform list until it is closed.
	gate chan struct{}

	// platformCalls counts the platform list requests.
	platformCalls int

	// filters are the filter[platform] values of the searches, in order.
	filters []string
}

func newPlatformsServer(t *testing.T) *platformsServer {
	t.Helper()

	s := &platformsServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/Platforms":
			s.mu.Lock()
			s.platformCalls++
			fail, gate := s.fail, s.gate
			s.mu.Unlock()

			if gate != nil {
				<-gate
			}

			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.Write([]byte(`{"data":{"platforms":{"12":{"id":12,"name":"Sony Playstation 3","alias":"sony-playstation-3"}}}}`))
		case "/v1.1/Games/ByGameName":
			s.mu.Lock()
			s.filters = append(s.filters, r.URL.Query().Get("filter[platform]"))
			s.mu.Unlock()

			w.Write([]byte(`{"data":{"games":[]}}`))
		}
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *platformsServer) set(fail bool, gate chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fail, s.gate = fail, gate
}

func (s *platformsServer) calls() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.platformCalls, append([]string(nil), s.filters...)
}

// TestPlatforms_failedList checks that a platform list TheGamesDB failed to send is not asked for
// again on every lookup: lookups search every platform for a while, then try the list again.
func TestPlatforms_failedList(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"PS3"},
	}
	settings := schema.Settings{settingAPIKey: "good"}

	t.Run("GIVEN TheGamesDB failing to send its platform list", func(t *testing.T) {
		srv := newPlatformsServer(t)
		srv.set(true, nil)

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := testProvider(srv.Server)
		p.now = clock.Now

		t.Run("WHEN two covers are searched", func(t *testing.T) {
			_, err := p.Covers(ctx, q, settings)
			require.NoError(t, err)

			_, err = p.Covers(ctx, q, settings)
			require.NoError(t, err)

			t.Run("THEN the list was asked for once, and both searched every platform", func(t *testing.T) {
				platformCalls, filters := srv.calls()
				assert.Equal(t, 1, platformCalls)
				assert.Equal(t, []string{"", ""}, filters)
			})
		})

		t.Run("WHEN TheGamesDB recovers and a cover is searched after five minutes", func(t *testing.T) {
			srv.set(false, nil)
			clock.now = clock.now.Add(platformsRetry)

			_, err := p.Covers(ctx, q, settings)
			require.NoError(t, err)

			t.Run("THEN the list is asked for again and the search is filtered", func(t *testing.T) {
				platformCalls, filters := srv.calls()
				assert.Equal(t, 2, platformCalls)
				assert.Equal(t, "12", filters[len(filters)-1])
			})
		})
	})
}

// TestPlatforms_waitingLookups checks that a lookup waiting for the platform list another lookup
// asked for gives up when its own context ends, instead of blocking until the list arrives.
func TestPlatforms_waitingLookups(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"PS3"},
	}
	settings := schema.Settings{settingAPIKey: "good"}

	t.Run("GIVEN a platform list request that hangs", func(t *testing.T) {
		srv := newPlatformsServer(t)
		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		srv.set(false, gate)

		// Released anyway after a while, so a lookup that blocks fails the test instead of hanging.
		time.AfterFunc(3*time.Second, release)

		p := testProvider(srv.Server)

		first := make(chan error, 1)

		go func() {
			_, err := p.Covers(ctx, q, settings)
			first <- err
		}()

		require.Eventually(t, func() bool {
			n, _ := srv.calls()
			return n == 1
		}, 5*time.Second, 10*time.Millisecond)

		t.Run("WHEN another lookup's context ends while it waits", func(t *testing.T) {
			short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
			defer stop()

			start := time.Now()
			_, err := p.Covers(short, q, settings)
			elapsed := time.Since(start)

			t.Run("THEN it returns at once with its context's error", func(t *testing.T) {
				require.Error(t, err)
				assert.True(t, strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "context"), err.Error())
				assert.Less(t, elapsed, 2*time.Second)
			})
		})

		t.Run("WHEN the list arrives", func(t *testing.T) {
			release()
			require.NoError(t, <-first)

			t.Run("THEN the first lookup's search is filtered, and the list was asked for once", func(t *testing.T) {
				platformCalls, filters := srv.calls()
				assert.Equal(t, 1, platformCalls)
				assert.Contains(t, filters, "12")
			})
		})
	})
}
