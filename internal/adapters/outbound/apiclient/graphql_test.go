package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
)

// fakeGraphQL answers each operation the way GraphQL gateways do (shapes from EA's and Ubisoft's
// APIs, with invented values): errors come with HTTP 200, a gateway in front may refuse a request
// with its own JSON object and no "errors", and rate limits are plain statuses.
func fakeGraphQL(t *testing.T) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body GraphQLBody

		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")

		switch body.OperationName {
		case "Owned":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"me": map[string]any{"games": []string{"Mass Effect", body.Variables["next"].(string)}},
			}})
		case "SignedOut":
			w.Write([]byte(`{"data":null,"errors":[{"message":"Token is invalid","extensions":{"code":"UNAUTHENTICATED"}}]}`))
		case "Partial":
			w.Write([]byte(`{"data":{"g0":{"title":"Dead Space"},"g1":null},"errors":[{"message":"Game not found","path":["g1"]}]}`))
		case "Gateway":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","error_description":"token expired"}`))
		case "Invalid":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"message":"Cannot query field \"x\"","extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`))
		case "Busy":
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"errors":[{"message":"slow down"}]}`))
		case "Maintenance":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>Back soon</html>"))
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	c.HTTP = srv.Client()

	return c
}

func TestClient_GraphQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := fakeGraphQL(t)
	query := func(op string) GraphQLBody {
		return GraphQLBody{
			Query:         "query " + op + " { me { games } }",
			OperationName: op,
			Variables:     map[string]any{"next": "Dead Space"},
		}
	}

	t.Run("GIVEN a GraphQL API", func(t *testing.T) {
		t.Run("WHEN a query succeeds", func(t *testing.T) {
			var data struct {
				Me struct {
					Games []string `json:"games"`
				} `json:"me"`
			}

			err := c.GraphQL(ctx, "/graphql", query("Owned"), nil, &data)

			t.Run("THEN its data is decoded, the variables having been sent", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []string{"Mass Effect", "Dead Space"}, data.Me.Games)
			})
		})

		t.Run("WHEN it answers HTTP 200 with an error code", func(t *testing.T) {
			err := c.GraphQL(ctx, "/graphql", query("SignedOut"), nil, &struct{}{})

			t.Run("THEN the code can be matched and the message is kept", func(t *testing.T) {
				assert.True(t, IsGraphQLCode(err, "INVALID_TICKET", "UNAUTHENTICATED"))
				assert.False(t, IsGraphQLCode(err, "FORBIDDEN"))
				assert.Contains(t, err.Error(), "Token is invalid")
			})
		})

		t.Run("WHEN it answers some of the data with errors", func(t *testing.T) {
			var data map[string]*struct {
				Title string `json:"title"`
			}

			err := c.GraphQL(ctx, "/graphql", query("Partial"), nil, &data)

			t.Run("THEN the data found is decoded and the error says it is partial", func(t *testing.T) {
				var ge *GraphQLError
				require.ErrorAs(t, err, &ge)
				assert.True(t, ge.Partial)
				assert.Equal(t, "Dead Space", data["g0"].Title)
				assert.Nil(t, data["g1"])
			})
		})

		t.Run("WHEN a gateway refuses the token with its own JSON object", func(t *testing.T) {
			var data struct {
				Games []string `json:"games"`
			}

			err := c.GraphQL(ctx, "/graphql", query("Gateway"), nil, &data)

			t.Run("THEN it is the HTTP status, not an empty answer", func(t *testing.T) {
				assert.True(t, IsStatus(err, http.StatusUnauthorized))
				assert.Contains(t, err.Error(), "HTTP 401")
			})
		})

		t.Run("WHEN it refuses the query with an error status and GraphQL errors", func(t *testing.T) {
			err := c.GraphQL(ctx, "/graphql", query("Invalid"), nil, &struct{}{})

			t.Run("THEN both the code and the status can be matched", func(t *testing.T) {
				assert.True(t, IsGraphQLCode(err, "GRAPHQL_VALIDATION_FAILED"))
				assert.True(t, IsStatus(err, http.StatusBadRequest))
			})
		})

		t.Run("WHEN it is rate limited", func(t *testing.T) {
			err := c.GraphQL(ctx, "/graphql", query("Busy"), nil, &struct{}{})

			t.Run("THEN it is a passing trouble whatever the body says", func(t *testing.T) {
				var ge *GraphQLError
				assert.False(t, errors.As(err, &ge))
				require.ErrorIs(t, ClassifyError(ctx, err), provider.ErrRateLimited)
			})
		})

		t.Run("WHEN it answers a web page", func(t *testing.T) {
			err := c.GraphQL(ctx, "/graphql", query("Maintenance"), nil, &struct{}{})

			t.Run("THEN it is not decoded as JSON", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrNotJSON)
			})
		})
	})
}
