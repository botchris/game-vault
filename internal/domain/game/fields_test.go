package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func yes() *bool { b := true; return &b }

func TestFieldValues(t *testing.T) {
	now := time.Now()

	t.Run("GIVEN a game with values on the game and on a copy", func(t *testing.T) {
		g, err := New("Halo 3", now)
		require.NoError(t, err)

		info := g.Info()
		info.Fields = FieldValues{"review": {Text: "Great"}, "empty": {Text: ""}}
		_, err = g.UpdateInfo(info, now)
		require.NoError(t, err)
		c, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, now)
		require.NoError(t, err)
		require.NoError(t, g.SetCopyFields(c.ID, FieldValues{"sealed": {Bool: yes()}, "tags": {Choices: []string{"a", "b"}}}, now))

		t.Run("THEN empty values are not stored and the rest reads back", func(t *testing.T) {
			assert.Equal(t, FieldValues{"review": {Text: "Great"}}, g.Fields())
			assert.True(t, *g.Copies()[0].Fields["sealed"].Bool)
		})

		t.Run("AND reading never exposes the game's own maps", func(t *testing.T) {
			g.Fields()["review"] = FieldValue{Text: "changed"}
			g.Copies()[0].Fields["tags"].Choices[0] = "x"
			assert.Equal(t, "Great", g.Fields()["review"].Text)
			assert.Equal(t, []string{"a", "b"}, g.Copies()[0].Fields["tags"].Choices)
		})

		t.Run("WHEN a scan updates the copy THEN its values stay", func(t *testing.T) {
			changed := g.copies[0].applyImport(CopyDetails{
				Kind:     KindPhysical,
				Platform: "Xbox 360",
			})
			assert.True(t, changed)
			assert.True(t, *g.Copies()[0].Fields["sealed"].Bool)
		})

		t.Run("WHEN a list value is replaced THEN game and copies change, without duplicates", func(t *testing.T) {
			assert.True(t, g.ReplaceChoice("tags", "a", "b", now))
			assert.Equal(t, []string{"b"}, g.Copies()[0].Fields["tags"].Choices)
			assert.True(t, g.ReplaceChoice("tags", "b", "", now))
			_, ok := g.Copies()[0].Fields["tags"]
			assert.False(t, ok, "a multilist left empty is removed")
		})

		t.Run("WHEN a field is removed THEN its values go and the counts say where", func(t *testing.T) {
			gameHad, copies := g.RemoveFieldValues("sealed", now)
			assert.False(t, gameHad)
			assert.Equal(t, 1, copies)

			gameHad, _ = g.RemoveFieldValues("review", now)
			assert.True(t, gameHad)
			assert.Empty(t, g.Fields())
		})
	})

	t.Run("GIVEN two games with values WHEN one absorbs the other", func(t *testing.T) {
		a, _ := New("Halo 3", now)
		b, _ := New("Halo 3", now)
		ia, ib := a.Info(), b.Info()
		ia.Fields = FieldValues{"review": {Text: "Kept"}}
		ib.Fields = FieldValues{"review": {Text: "Lost"}, "by": {Text: "A friend"}}
		_, _ = a.UpdateInfo(ia, now)
		_, _ = b.UpdateInfo(ib, now)
		a.Absorb(b, now)

		t.Run("THEN the kept game's values win and gaps are filled", func(t *testing.T) {
			assert.Equal(t, "Kept", a.Fields()["review"].Text)
			assert.Equal(t, "A friend", a.Fields()["by"].Text)
		})
	})
}

func TestReplaceChoice(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := created.Add(time.Hour)

	// newGame returns a game whose "shelf" list value is "top" on the game and whose copy holds
	// the multilist "tags" with "a" and "b".
	newGame := func(t *testing.T) *Game {
		t.Helper()

		g, err := New("Halo 3", created)
		require.NoError(t, err)

		info := g.Info()
		info.Fields = FieldValues{"shelf": {Choice: "top"}}
		_, err = g.UpdateInfo(info, created)
		require.NoError(t, err)

		c, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, created)
		require.NoError(t, err)
		require.NoError(t, g.SetCopyFields(c.ID, FieldValues{"tags": {Choices: []string{"a", "b"}}}, created))

		return g
	}

	t.Run("GIVEN a game-level list value", func(t *testing.T) {
		t.Run("WHEN it is replaced with another value", func(t *testing.T) {
			g := newGame(t)
			changed := g.ReplaceChoice("shelf", "top", "bottom", later)

			t.Run("THEN the game holds the new value and its update time moves", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, "bottom", g.Fields()["shelf"].Choice)
				assert.Equal(t, later, g.UpdatedAt())
			})
		})

		t.Run("WHEN it is cleared", func(t *testing.T) {
			g := newGame(t)
			changed := g.ReplaceChoice("shelf", "top", "", later)

			t.Run("THEN the value is gone", func(t *testing.T) {
				assert.True(t, changed)
				assert.NotContains(t, g.Fields(), "shelf")
			})
		})

		t.Run("WHEN another value is replaced", func(t *testing.T) {
			g := newGame(t)
			changed := g.ReplaceChoice("shelf", "middle", "bottom", later)

			t.Run("THEN nothing changes, the update time included", func(t *testing.T) {
				assert.False(t, changed)
				assert.Equal(t, "top", g.Fields()["shelf"].Choice)
				assert.Equal(t, created, g.UpdatedAt())
			})
		})
	})

	t.Run("GIVEN a copy's multilist value", func(t *testing.T) {
		t.Run("WHEN one of its values is replaced", func(t *testing.T) {
			g := newGame(t)
			changed := g.ReplaceChoice("tags", "a", "c", later)

			t.Run("THEN the copy holds the new value and both update times move", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, []string{"c", "b"}, g.Copies()[0].Fields["tags"].Choices)
				assert.Equal(t, later, g.Copies()[0].UpdatedAt)
				assert.Equal(t, later, g.UpdatedAt())
			})
		})

		t.Run("WHEN an empty value is replaced", func(t *testing.T) {
			g := newGame(t)
			changed := g.ReplaceChoice("tags", "", "c", later)

			t.Run("THEN nothing changes: an empty id never matches", func(t *testing.T) {
				assert.False(t, changed)
				assert.Equal(t, FieldValue{Choices: []string{"a", "b"}}, g.Copies()[0].Fields["tags"])
				assert.Equal(t, created, g.UpdatedAt())
			})
		})
	})
}

func TestRemoveFieldValues(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := created.Add(time.Hour)

	t.Run("GIVEN a game and a copy with a value of the field", func(t *testing.T) {
		g, err := New("Halo 3", created)
		require.NoError(t, err)

		info := g.Info()
		info.Fields = FieldValues{"review": {Text: "Great"}}
		_, err = g.UpdateInfo(info, created)
		require.NoError(t, err)
		c, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, created)
		require.NoError(t, err)
		require.NoError(t, g.SetCopyFields(c.ID, FieldValues{"review": {Text: "Sealed"}}, created))

		t.Run("WHEN another field is removed", func(t *testing.T) {
			gameHad, copies := g.RemoveFieldValues("other", later)

			t.Run("THEN nothing changes, the update time included", func(t *testing.T) {
				assert.False(t, gameHad)
				assert.Zero(t, copies)
				assert.Equal(t, created, g.UpdatedAt())
			})
		})

		t.Run("WHEN the field is removed", func(t *testing.T) {
			gameHad, copies := g.RemoveFieldValues("review", later)

			t.Run("THEN the values are gone and the update times move", func(t *testing.T) {
				assert.True(t, gameHad)
				assert.Equal(t, 1, copies)
				assert.Empty(t, g.Fields())
				assert.Empty(t, g.Copies()[0].Fields)
				assert.Equal(t, later, g.UpdatedAt())
				assert.Equal(t, later, g.Copies()[0].UpdatedAt)
			})
		})
	})
}

func TestRehydrate_compactsCopyFields(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("GIVEN a stored copy with an empty value", func(t *testing.T) {
		g := Rehydrate("g1", Info{Title: "Halo 3"}, []Copy{{
			ID:          "c1",
			CopyDetails: CopyDetails{Kind: KindPhysical},
			Fields:      FieldValues{"review": {Text: ""}},
		}}, now, now)

		t.Run("WHEN the field is removed", func(t *testing.T) {
			_, copies := g.RemoveFieldValues("review", now)

			t.Run("THEN no copy counts as having had a value", func(t *testing.T) {
				assert.Zero(t, copies)
			})
		})
	})
}

func TestAbsorb_keepsCopyFields(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("GIVEN two games whose copies hold values", func(t *testing.T) {
		a, err := New("Halo 3", now)
		require.NoError(t, err)
		b, err := New("Halo 3", now)
		require.NoError(t, err)

		ca, err := a.AddCopy(CopyDetails{Kind: KindPhysical}, now)
		require.NoError(t, err)
		require.NoError(t, a.SetCopyFields(ca.ID, FieldValues{"given": {Text: "Ana"}}, now))
		cb, err := b.AddCopy(CopyDetails{Kind: KindPhysical}, now)
		require.NoError(t, err)
		require.NoError(t, b.SetCopyFields(cb.ID, FieldValues{"given": {Text: "Luis"}}, now))

		t.Run("WHEN one absorbs the other", func(t *testing.T) {
			a.Absorb(b, now)

			t.Run("THEN every copy keeps its own values", func(t *testing.T) {
				copies := a.Copies()
				require.Len(t, copies, 2)
				assert.Equal(t, "Ana", copies[0].Fields["given"].Text)
				assert.Equal(t, "Luis", copies[1].Fields["given"].Text)
			})
		})
	})
}
