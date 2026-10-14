package ubisoft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/schema"
)

// TestNode_platformGroupShapes decodes ownedPlatformGroups in the shapes the library answer has
// been seen in: a list of groups, and (in production, 2026-10) a list of lists of groups.
func TestNode_platformGroupShapes(t *testing.T) {
	cases := []struct {
		name string
		meta string
		onPC bool
	}{
		{"a flat list with PC", `[{"name":"PC","type":"PC"}]`, true},
		{"a flat list with consoles only", `[{"name":"PlayStation 4","type":"PS4"}]`, false},
		{"a list of lists with PC", `[[{"name":"PlayStation 4","type":"PS4"}],[{"name":"PC","type":"PC"}]]`, true},
		{"a list of lists with consoles only", `[[{"name":"Xbox One","type":"XBOXONE"},{"name":"PlayStation 4","type":"PS4"}]]`, false},
		{"an empty list", `[]`, true},
		{"null", `null`, true},
		{"a shape nobody has seen", `{"unexpected":true}`, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var n node

			raw := `{"id":"g1","spaceId":"s1","name":"Example Game","viewer":{"meta":{"ownedPlatformGroups":` + c.meta + `}}}`
			require.NoError(t, json.Unmarshal([]byte(raw), &n))
			assert.Equal(t, c.onPC, onPC(n))
		})
	}
}

// TestFetch_fullQueryUnreadable checks that a full library answer the code cannot read (Ubisoft
// changed a field's shape) is asked again with the minimal query instead of failing the scan.
func TestFetch_fullQueryUnreadable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a library whose full answer has a field in a shape nobody has seen", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /v3/profiles/sessions", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"ticket":"tk","sessionId":"sid","rememberMeTicket":"rm-2"}`)
		})
		mux.HandleFunc("POST /v1/profiles/me/uplay/graphql", func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Query string }
			json.NewDecoder(r.Body).Decode(&body)

			if strings.Contains(body.Query, "ownedPlatformGroups") {
				fmt.Fprint(w, `{"data":{"viewer":{"games":{"totalCount":1,"nodes":[{"id":"g1","spaceId":"s1","name":"Example Game","viewer":"changed"}]}}}}`)
				return
			}

			fmt.Fprint(w, `{"data":{"viewer":{"games":{"totalCount":1,"nodes":[{"id":"g1","spaceId":"s1","name":"Example Game"}]}}}}`)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		p := NewProvider()
		p.API.BaseURL = srv.URL

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, _, err := p.Fetch(ctx, schema.Settings{settingLoginData: pasted})

			t.Run("THEN the minimal query's games are imported", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, copies, 1)
				assert.Equal(t, "ubisoft:s1", copies[0].ExternalID)
			})
		})
	})
}
