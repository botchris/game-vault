package rpc_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/app"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// fakeChat is a notification connector that accepts one token, so the end-to-end tests can pass and
// fail a test without a network.
type fakeChat struct{}

func (fakeChat) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type: "fakechat",
		Name: "Fake chat",
		Fields: schema.Fields{{
			Key:      "token",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

func (fakeChat) Send(context.Context, schema.Settings, notify.Message) error { return nil }

func (fakeChat) Test(_ context.Context, s schema.Settings, _ notify.Message) error {
	if s["token"] != "good-token" {
		return errors.New("the chat rejected the token")
	}

	return nil
}

func chatInput(token string) *pb.ConnectionInput {
	return &pb.ConnectionInput{
		Type:     "fakechat",
		Name:     "Phone",
		Enabled:  true,
		Events:   []string{"collection.added", "issue.opened"},
		Language: "en",
		Settings: map[string]string{"token": token},
	}
}

func TestNotifications_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	fake := &fakeProvider{copies: []game.ImportedCopy{{
		ExternalID: "fake:1",
		Title:      "Lantern Keep",
		Details: game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		},
	}}}
	c := newServer(t, fake, app.WithSending(true))

	t.Run("GIVEN the connector types", func(t *testing.T) {
		res, err := c.notify.ListConnectorTypes(ctx, connect.NewRequest(&pb.ListConnectorTypesRequest{}))
		require.NoError(t, err)

		t.Run("THEN they list the connectors, the eight events, the languages and the placeholder", func(t *testing.T) {
			ids := make([]string, 0, len(res.Msg.Types))
			for _, typ := range res.Msg.Types {
				ids = append(ids, typ.Id)
			}

			assert.Subset(t, ids, []string{"telegram", "ntfy", "webhook", "fakechat"})
			assert.Len(t, res.Msg.Events, 8)
			assert.Equal(t, &pb.EventKind{
				Kind:  "collection.added",
				Group: "activity",
			}, res.Msg.Events[0])
			assert.Equal(t, []string{"en", "es"}, res.Msg.Languages)
			assert.Equal(t, schema.SecretPlaceholder, res.Msg.SecretPlaceholder)
		})
	})

	t.Run("GIVEN a connection saved with a secret", func(t *testing.T) {
		saved, err := c.notify.SaveConnection(ctx, connect.NewRequest(&pb.SaveConnectionRequest{Connection: chatInput("good-token")}))
		require.NoError(t, err)

		id := saved.Msg.Connection.Id

		t.Run("THEN it comes back masked", func(t *testing.T) {
			assert.NotEmpty(t, id)
			assert.Equal(t, "Phone", saved.Msg.Connection.Name)
			assert.Equal(t, schema.SecretPlaceholder, saved.Msg.Connection.Settings["token"])
		})

		t.Run("AND the list shows it masked too", func(t *testing.T) {
			list, err := c.notify.ListConnections(ctx, connect.NewRequest(&pb.ListConnectionsRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Connections, 1)
			assert.Equal(t, schema.SecretPlaceholder, list.Msg.Connections[0].Settings["token"])
			assert.Equal(t, []string{"collection.added", "issue.opened"}, list.Msg.Connections[0].Events)
			assert.Empty(t, list.Msg.Connections[0].PausedReason)
		})

		t.Run("WHEN it is tested with the placeholder, the stored secret is used", func(t *testing.T) {
			res, err := c.notify.TestConnection(ctx, connect.NewRequest(&pb.TestConnectionRequest{
				Id:         id,
				Connection: chatInput(schema.SecretPlaceholder),
			}))
			require.NoError(t, err)

			t.Run("THEN it succeeds", func(t *testing.T) {
				assert.True(t, res.Msg.Success)
				assert.Empty(t, res.Msg.Message)
			})
		})

		t.Run("WHEN a wrong token is tested", func(t *testing.T) {
			res, err := c.notify.TestConnection(ctx, connect.NewRequest(&pb.TestConnectionRequest{Connection: chatInput("bad-token")}))

			t.Run("THEN the failure is the answer, not an RPC error", func(t *testing.T) {
				require.NoError(t, err)
				assert.False(t, res.Msg.Success)
				assert.Equal(t, "the chat rejected the token", res.Msg.Message)
			})
		})

		t.Run("WHEN a game is added by a scan", func(t *testing.T) {
			src, err := c.sources.CreateSource(ctx, connect.NewRequest(&pb.CreateSourceRequest{Source: &pb.SourceInput{
				Type:     "fake",
				Enabled:  true,
				Settings: map[string]string{"token": "s3cret"},
			}}))
			require.NoError(t, err)

			_, err = c.sources.SyncSource(ctx, connect.NewRequest(&pb.SyncSourceRequest{Id: src.Msg.Source.Id}))
			require.NoError(t, err)

			t.Run("THEN the connection's deliveries show it titled in its language", func(t *testing.T) {
				res, err := c.notify.ListDeliveries(ctx, connect.NewRequest(&pb.ListDeliveriesRequest{ConnectionId: id}))
				require.NoError(t, err)
				require.Len(t, res.Msg.Deliveries, 1)

				d := res.Msg.Deliveries[0]
				assert.Equal(t, "collection.added", d.Event)
				assert.Equal(t, "queued", d.State)
				assert.Contains(t, d.Summary, "a new game")
				assert.NotNil(t, d.CreatedAt)
			})
		})

		t.Run("WHEN it is reconfigured with the placeholder", func(t *testing.T) {
			in := chatInput(schema.SecretPlaceholder)
			in.Name = "Tablet"

			res, err := c.notify.SaveConnection(ctx, connect.NewRequest(&pb.SaveConnectionRequest{
				Id:         id,
				Connection: in,
			}))
			require.NoError(t, err)

			t.Run("THEN the name changes and the secret is kept", func(t *testing.T) {
				assert.Equal(t, "Tablet", res.Msg.Connection.Name)

				test, err := c.notify.TestConnection(ctx, connect.NewRequest(&pb.TestConnectionRequest{
					Id:         id,
					Connection: in,
				}))
				require.NoError(t, err)
				assert.True(t, test.Msg.Success)
			})
		})

		t.Run("WHEN it is saved with no event", func(t *testing.T) {
			in := chatInput("good-token")
			in.Events = nil

			_, err := c.notify.SaveConnection(ctx, connect.NewRequest(&pb.SaveConnectionRequest{Connection: in}))

			t.Run("THEN the request is invalid", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN it is deleted", func(t *testing.T) {
			_, err := c.notify.DeleteConnection(ctx, connect.NewRequest(&pb.DeleteConnectionRequest{Id: id}))
			require.NoError(t, err)

			t.Run("THEN it is gone, and deleting it again is not found", func(t *testing.T) {
				list, err := c.notify.ListConnections(ctx, connect.NewRequest(&pb.ListConnectionsRequest{}))
				require.NoError(t, err)
				assert.Empty(t, list.Msg.Connections)

				_, err = c.notify.DeleteConnection(ctx, connect.NewRequest(&pb.DeleteConnectionRequest{Id: id}))
				assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			})
		})
	})

	t.Run("GIVEN the Game Vault address", func(t *testing.T) {
		t.Run("WHEN a bad one is saved", func(t *testing.T) {
			_, err := c.notify.SaveNotificationSettings(ctx, connect.NewRequest(&pb.SaveNotificationSettingsRequest{Address: "vault.local"}))

			t.Run("THEN it is invalid", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN a good one is saved with a trailing slash", func(t *testing.T) {
			saved, err := c.notify.SaveNotificationSettings(ctx, connect.NewRequest(&pb.SaveNotificationSettingsRequest{Address: "http://vault.local:8080/"}))
			require.NoError(t, err)

			t.Run("THEN it is stored without the slash", func(t *testing.T) {
				assert.Equal(t, "http://vault.local:8080", saved.Msg.Address)

				got, err := c.notify.GetNotificationSettings(ctx, connect.NewRequest(&pb.GetNotificationSettingsRequest{}))
				require.NoError(t, err)
				assert.Equal(t, "http://vault.local:8080", got.Msg.Address)
			})
		})
	})
}

func TestNotifications_accessTokenRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)

	created, err := local.CreateAccessToken(ctx, connect.NewRequest(&pb.CreateAccessTokenRequest{Name: "Extension, laptop"}))
	require.NoError(t, err)

	t.Run("GIVEN an access token", func(t *testing.T) {
		client := gamevaultv1connect.NewNotificationServiceClient(bearer(created.Msg.Secret, "gamevault.example:8080", nil), c.baseURL)

		t.Run("WHEN it asks for the connections or the settings", func(t *testing.T) {
			_, listErr := client.ListConnections(ctx, connect.NewRequest(&pb.ListConnectionsRequest{}))
			_, settingsErr := client.GetNotificationSettings(ctx, connect.NewRequest(&pb.GetNotificationSettingsRequest{}))

			t.Run("THEN both are refused as unauthenticated", func(t *testing.T) {
				assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(listErr))
				assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(settingsErr))
			})
		})
	})
}
