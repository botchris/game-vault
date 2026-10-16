package csvfile

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

// The CSV format is what users keep as backups and edit in spreadsheets, so it must not change by
// accident. The golden files in testdata were written by the codec on 2026-10-13: an export of
// games with every column filled in, and what an import with every column (and every kind of
// mistake) reads. Run `go test ./internal/adapters/outbound/csvfile -update` only when the format
// changes on purpose.

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func goldenGames(t *testing.T) []*game.Game {
	t.Helper()

	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	contents, err := game.ContentsOf(game.ContentBox, game.ContentManual, game.ContentMedia)
	require.NoError(t, err)

	quest := game.Rehydrate(game.Snapshot{
		ID: "g1",
		Info: game.Info{
			Title: "Console Quest",
			Links: game.Links{
				game.LinkSteam: "100",
				"gog":          "200",
			},
		},
		Copies: []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:       game.KindPhysical,
				Platform:   "PS5",
				Status:     game.StatusLent,
				AcquiredOn: "2025-12-24",
				Edition:    "Collector's",
				Grade:      game.GradeVeryGood,
				Region:     game.RegionPAL,
				Contents:   contents,
				Location:   "Shelf B",
				Price: game.Money{
					Amount:   2995,
					Currency: "EUR",
				},
				Notes:   "Lent to a friend, \"until spring\"",
				Barcode: "4006381333931",
			},
			ExternalID: "manual:quest",
		}, {
			ID: "c2",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindKey,
				Platform: "Steam",
				Status:   game.StatusRevealed,
				Key:      "AAAAA-BBBBB-CCCCC",
				RedeemBy: "2026-12-31",
				Origin:   "Humble – Example Bundle",
			},
			SourceID:   "s1",
			ExternalID: "humble:A:1",
		}},
		AutoLinks: []string{game.LinkSteam},
		CreatedAt: at,
		UpdatedAt: at,
	})

	hero := game.Rehydrate(game.Snapshot{
		ID:   "g2",
		Info: game.Info{Title: "Pocket Hero"},
		Copies: []game.Copy{{
			ID: "c3",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Switch",
				Price: game.Money{
					Amount:   1500,
					Currency: "JPY",
				},
			},
		}, {
			ID: "c4",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
				Status:   game.StatusOwned,
				System:   "Linux",
			},
			ExternalID: "steam:300",
		}},
		CreatedAt: at,
		UpdatedAt: at,
	})

	genesis := game.Rehydrate(game.Snapshot{
		ID:   "g3",
		Info: game.Info{Title: "Sonic the Hedgehog"},
		Copies: []game.Copy{{
			ID: "c5",
			CopyDetails: game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Mega Drive / Genesis",
				Status:   game.StatusOwned,
				Region:   game.RegionNTSCJ,
			},
			ExternalID: "manual:sonic",
		}},
		CreatedAt: at,
		UpdatedAt: at,
	})

	return []*game.Game{quest, hero, genesis}
}

func TestGolden_export(t *testing.T) {
	t.Run("GIVEN games with every column filled in", func(t *testing.T) {
		games := goldenGames(t)

		t.Run("WHEN they are exported", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Codec{}.Encode(&b, games))

			t.Run("THEN the file is byte for byte the golden export", func(t *testing.T) {
				golden(t, "export.golden.csv", b.String())
			})
		})
	})
}

func TestGolden_import(t *testing.T) {
	// The export golden is imported back too: its result is export.reimport.json, not a second
	// ".golden" in the name.
	files := []struct {
		in     string
		golden string
	}{
		{
			in:     "import.csv",
			golden: "import.golden.json",
		},
		{
			in:     "import-semicolon.csv",
			golden: "import-semicolon.golden.json",
		},
		{
			in:     "export.golden.csv",
			golden: "export.reimport.json",
		},
	}

	for _, f := range files {
		t.Run("GIVEN the file "+f.in, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join("testdata", f.in))
			require.NoError(t, err)

			t.Run("WHEN it is imported", func(t *testing.T) {
				copies, warnings, err := Codec{}.Decode(strings.NewReader(string(in)))
				require.NoError(t, err)

				t.Run("THEN the copies and warnings are the golden ones", func(t *testing.T) {
					out, err := json.MarshalIndent(struct {
						Copies   []game.ImportedCopy
						Warnings []string
					}{copies, warnings}, "", "  ")
					require.NoError(t, err)

					golden(t, f.golden, string(out)+"\n")
				})
			})
		})
	}
}
