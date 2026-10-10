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
			assert.True(t, g.ReplaceChoice("tags", "a", "b"))
			assert.Equal(t, []string{"b"}, g.Copies()[0].Fields["tags"].Choices)
			assert.True(t, g.ReplaceChoice("tags", "b", ""))
			_, ok := g.Copies()[0].Fields["tags"]
			assert.False(t, ok, "a multilist left empty is removed")
		})

		t.Run("WHEN a field is removed THEN its values go and the counts say where", func(t *testing.T) {
			gameHad, copies := g.RemoveFieldValues("sealed")
			assert.False(t, gameHad)
			assert.Equal(t, 1, copies)

			gameHad, _ = g.RemoveFieldValues("review")
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
