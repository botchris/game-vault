package media_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/application/media"
)

func TestGameQuery_SearchSystems(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    media.GameQuery
		want []string
	}{
		{
			name: "an edition searches its own system",
			q: media.GameQuery{
				System:            "PS3",
				Platforms:         []string{"Steam"},
				PhysicalPlatforms: []string{"Xbox 360"},
			},
			want: []string{"PS3"},
		},
		{
			name: "without an edition, the physical copies' systems win",
			q: media.GameQuery{
				Platforms:         []string{"Steam", "Xbox 360"},
				PhysicalPlatforms: []string{"Xbox 360"},
			},
			want: []string{"Xbox 360"},
		},
		{
			name: "else every copy's system, once each, stores named by their system",
			q: media.GameQuery{
				Platforms: []string{"Steam", "GOG", "PlayStation Store", "PS4", "", "Amiga"},
			},
			want: []string{"PC", "PS4", "Amiga"},
		},
		{
			name: "no copies, no systems",
			q:    media.GameQuery{},
			want: []string{},
		},
		{
			name: "an edition stored under an alias searches the catalog name",
			q: media.GameQuery{
				System: "Mega Drive",
			},
			want: []string{"Mega Drive / Genesis"},
		},
		{
			name: "copies typed with an alias search the catalog name, once",
			q: media.GameQuery{
				Platforms: []string{"Sega Genesis", "mega-drive", "Mega Drive / Genesis"},
			},
			want: []string{"Mega Drive / Genesis"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.q.SearchSystems())
		})
	}
}
