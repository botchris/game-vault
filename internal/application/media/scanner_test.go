package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
)

// recordedRhythm is a scanner rhythm with a fixed random draw that records each wait instead of
// sleeping, and ends the scan after the given number of waits.
func recordedRhythm(random float64, waits int, cancel context.CancelFunc) (unattended.Rhythm, *[]time.Duration) {
	var got []time.Duration

	return unattended.Rhythm{
		Random: func() float64 { return random },
		Sleep: func(_ context.Context, d time.Duration) bool {
			got = append(got, d)
			if len(got) >= waits {
				cancel()
				return false
			}

			return true
		},
	}, &got
}

func TestRunScanner_jitter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Steam game without details and a random source that always draws 0.75", func(t *testing.T) {
		scanCtx, stop := context.WithCancel(ctx)
		defer stop()

		rhythm, waits := recordedRhythm(0.75, 3, stop)
		env := newDetailsEnv(t, ctx, time.Now, &media.GameDetails{Summary: "A PC classic."}, media.WithRhythm(rhythm))
		saveGame(t, ctx, env.games, "PC Classic", game.Links{game.LinkSteam: "10"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN the scanner runs one round with a 2 s pause", func(t *testing.T) {
			env.svc.Details.RunScanner(scanCtx, 2*time.Second)

			t.Run("THEN no wait is the fixed value: each is drawn within its range", func(t *testing.T) {
				assert.Equal(t, []time.Duration{
					52500 * time.Millisecond, // start-up: 30 s to 60 s
					3500 * time.Millisecond,  // between games: pause to twice the pause
					63 * time.Minute,         // between rounds: an hour ±10%
				}, *waits)

				t.Run("AND the game's details were fetched", func(t *testing.T) {
					assert.Equal(t, int32(1), env.store.calls.Load())
				})
			})
		})
	})

	t.Run("GIVEN a linked PS4 disc whose only details provider fails", func(t *testing.T) {
		scanCtx, stop := context.WithCancel(ctx)
		defer stop()

		rhythm, waits := recordedRhythm(0.75, 2, stop)
		env := newDetailsEnv(t, ctx, time.Now, nil, media.WithRhythm(rhythm))
		env.quota.failing.Store(true)
		saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN the scanner reaches it", func(t *testing.T) {
			env.svc.Details.RunScanner(scanCtx, 2*time.Second)

			t.Run("THEN it backs off for a minute ±20%, not exactly a minute", func(t *testing.T) {
				require.Len(t, *waits, 2)
				assert.Equal(t, 66*time.Second, (*waits)[1])
			})
		})
	})
}
