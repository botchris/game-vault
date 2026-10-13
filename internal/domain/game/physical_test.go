package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContents(t *testing.T) {
	t.Run("GIVEN contents given out of order and repeated", func(t *testing.T) {
		c, err := ContentsOf(ContentMedia, ContentBox, ContentMedia)
		require.NoError(t, err)

		t.Run("THEN they are a set, listed in the fixed order", func(t *testing.T) {
			assert.Equal(t, []Content{ContentBox, ContentMedia}, c.List())
			assert.True(t, c.Has(ContentBox))
			assert.False(t, c.Has(ContentManual))

			same, _ := ContentsOf(ContentBox, ContentMedia)
			assert.Equal(t, same, c)
		})
	})

	t.Run("GIVEN an unknown content", func(t *testing.T) {
		_, err := ContentsOf(ContentBox, "poster")

		t.Run("THEN it is refused", func(t *testing.T) {
			assert.Error(t, err)
		})
	})
}

func TestGrade(t *testing.T) {
	for _, g := range append([]Grade{""}, Grades()...) {
		assert.True(t, g.Valid(), "grade %q", g)
	}

	assert.False(t, Grade("excellent").Valid())
}

func TestCopyDetails_physicalFields(t *testing.T) {
	contents, _ := ContentsOf(ContentBox, ContentMedia)
	physical := CopyDetails{
		Kind:     KindPhysical,
		Platform: "PS4",
		Grade:    GradeVeryGood,
		Contents: contents,
		Location: "Shelf",
		Price: Money{
			Amount:   2995,
			Currency: "eur",
		},
	}

	t.Run("GIVEN a physical copy with grade, contents, location and price", func(t *testing.T) {
		got, err := physical.normalize()
		require.NoError(t, err)

		t.Run("THEN they are kept", func(t *testing.T) {
			assert.Equal(t, GradeVeryGood, got.Grade)
			assert.Equal(t, contents, got.Contents)
			assert.Equal(t, Money{
				Amount:   2995,
				Currency: "EUR",
			}, got.Price)
		})
	})

	t.Run("GIVEN the same copy turned into a key", func(t *testing.T) {
		key := physical
		key.Kind = KindKey
		got, err := key.normalize()
		require.NoError(t, err)

		t.Run("THEN grade, contents and location go, and the price stays", func(t *testing.T) {
			assert.Empty(t, got.Grade)
			assert.Zero(t, got.Contents)
			assert.Empty(t, got.Location)
			assert.Equal(t, int64(2995), got.Price.Amount)
		})
	})

	t.Run("GIVEN an invalid grade or price", func(t *testing.T) {
		bad := physical
		bad.Grade = "excellent"
		_, errGrade := bad.normalize()

		bad = physical
		bad.Price = Money{Amount: 10}
		_, errPrice := bad.normalize()

		t.Run("THEN the copy is refused", func(t *testing.T) {
			assert.Error(t, errGrade)
			assert.Error(t, errPrice)
		})
	})
}

func TestGradesAndContents_areReadOnly(t *testing.T) {
	t.Run("GIVEN a caller that changes the lists it was given", func(t *testing.T) {
		grades := Grades()
		grades[0] = "scratched"

		contents := AllContents()
		contents[0], contents[1] = contents[1], contents[0]

		t.Run("THEN the domain's lists are unchanged", func(t *testing.T) {
			assert.Equal(t, GradeSealed, Grades()[0])
			assert.False(t, Grade("scratched").Valid())

			c, err := ContentsOf(ContentBox)
			require.NoError(t, err)
			assert.Equal(t, Contents(1), c, "a stored bit set keeps its meaning")
		})
	})
}
