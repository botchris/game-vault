package amazon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Answers with the shape of the real services (2026-10-09): register and token reject bad values
// with HTTP 400 "InvalidValue", the entitlements service an expired token with a bare 401.
const (
	registered = `{"response":{"success":{"tokens":{"bearer":{"access_token":"Atna|first","refresh_token":"Atnr|good","expires_in":"3600"},
		"mac_dms":{"adp_token":"x","device_private_key":"y"}},"extensions":{"customer_info":{"given_name":"Test","user_id":"amzn1.account.TEST"},
		"device_info":{"device_serial_number":"SERIAL"}}}},"request_id":"r1"}`
	invalidValue = `{"response":{"error":{"code":"InvalidValue","index":"abc","message":"One or more provided values are invalid."}},"request_id":"r2"}`
	refreshed    = `{"access_token":"Atna|second","expires_in":3600,"token_type":"bearer"}`
	page1        = `{"entitlements":[
		{"id":"e1","product":{"id":"amzn1.adg.product.1","title":"Tunic™","productDetail":{"iconUrl":"https://m.media-amazon.com/1.png",
			"details":{"websites":{"steam":"https://store.steampowered.com/app/553420/TUNIC/","official":null}}}}},
		{"id":"e2","product":{"id":"amzn1.adg.product.2","productDetail":{"details":{}}}}],"nextToken":"page2"}`
	page2 = `{"entitlements":[
		{"id":"e3","product":{"id":"amzn1.adg.product.3","title":"Fallout 76","productDetail":{"details":{"websites":{"steam":null}}}}},
		{"id":"e1b","product":{"id":"amzn1.adg.product.1","title":"Tunic™","productDetail":{"details":{}}}}]}`
)

// fakeAmazon serves the account API and the entitlements service.
type fakeAmazon struct {
	registerBodies []map[string]any
	expireFirst    bool // reject the first access token, as when it expired mid-scan
	revoked        bool // refresh token no longer valid
	refreshes      int
}

func (f *fakeAmazon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)

	var body map[string]any

	_ = json.Unmarshal(raw, &body)

	switch r.URL.Path {
	case "/auth/register":
		f.registerBodies = append(f.registerBodies, body)

		auth, _ := body["auth_data"].(map[string]any)
		if auth["authorization_code"] != "GOODCODE" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(invalidValue))

			return
		}

		w.Write([]byte(registered))
	case "/auth/token":
		f.refreshes++
		if f.revoked || body["source_token"] != "Atnr|good" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"InvalidValue","error_description":"The request has an invalid parameter : source_token"}`))

			return
		}

		w.Write([]byte(refreshed))
	case "/entitlements":
		token := r.Header.Get("x-amzn-token")
		if r.Header.Get("X-Amz-Target") == "" || token == "" || (f.expireFirst && token == "Atna|first") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`<NotAuthorizedException/>`))

			return
		}

		if body["nextToken"] == "page2" {
			w.Write([]byte(page2))
			return
		}

		w.Write([]byte(page1))
	default:
		http.NotFound(w, r)
	}
}

func newTestProvider(t *testing.T, fake *fakeAmazon) *Provider {
	t.Helper()

	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	return &Provider{
		SignInURL: "https://amazon.test/ap/signin", APIURL: srv.URL, EntitlementsURL: srv.URL + "/entitlements",
		Client: srv.Client(), Now: func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
	}
}

func TestSignIn_registersTheLinkDevice(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the sign-in link of a running server", func(t *testing.T) {
		fake := &fakeAmazon{}
		p := newTestProvider(t, fake)

		link, err := url.Parse(p.Descriptor().Fields[0].HelpURL)
		require.NoError(t, err)

		q := link.Query()

		t.Run("THEN it is the launcher's device sign-in with a PKCE challenge", func(t *testing.T) {
			assert.Equal(t, "amzn_sonic_games_launcher", q.Get("openid.assoc_handle"))
			assert.Equal(t, "S256", q.Get("openid.oa2.code_challenge_method"))
			assert.NotEmpty(t, q.Get("openid.oa2.code_challenge"))
			assert.True(t, strings.HasPrefix(q.Get("openid.oa2.client_id"), "device:"))
		})

		t.Run("WHEN the address Amazon lands on is pasted", func(t *testing.T) {
			pasted := "https://www.amazon.com/?openid.assoc_handle=amzn_sonic_games_launcher&openid.oa2.authorization_code=GOODCODE&openid.mode=id_res"
			out, err := p.Prepare(ctx, source.Settings{settingCode: pasted})
			require.NoError(t, err)

			t.Run("THEN the code registers the link's device and the session replaces the code", func(t *testing.T) {
				require.Len(t, fake.registerBodies, 1)

				auth := fake.registerBodies[0]["auth_data"].(map[string]any)
				assert.Equal(t, strings.TrimPrefix(q.Get("openid.oa2.client_id"), "device:"), auth["client_id"])
				assert.NotContains(t, out, settingCode)

				var s session
				require.NoError(t, json.Unmarshal([]byte(out[settingSession]), &s))
				assert.Equal(t, "Atnr|good", s.RefreshToken)
				assert.NotEmpty(t, s.Serial)
			})

			t.Run("AND the same code again (Test, then Save) does not register twice", func(t *testing.T) {
				_, err := p.Prepare(ctx, source.Settings{settingCode: pasted})
				require.NoError(t, err)
				assert.Len(t, fake.registerBodies, 1)
			})

			t.Run("AND the next sign-in link is a new device", func(t *testing.T) {
				assert.NotEqual(t, link.String(), p.Descriptor().Fields[0].HelpURL)
			})
		})
	})

	t.Run("GIVEN a code Amazon does not accept", func(t *testing.T) {
		p := newTestProvider(t, &fakeAmazon{})

		t.Run("WHEN it is pasted", func(t *testing.T) {
			_, err := p.Prepare(ctx, source.Settings{settingCode: "BADCODE"})

			t.Run("THEN it fails with ErrBadCode", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrBadCode)
			})
		})
	})

	t.Run("GIVEN nothing pasted and no session", func(t *testing.T) {
		p := newTestProvider(t, &fakeAmazon{})

		t.Run("WHEN the source is saved", func(t *testing.T) {
			_, err := p.Prepare(ctx, source.Settings{})

			t.Run("THEN it asks for the address", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrNoSession)
			})
		})
	})
}

func TestFetch_readsTheLibrary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	stored := source.Settings{settingSession: `{"refresh_token":"Atnr|good","serial":"SERIAL"}`}

	t.Run("GIVEN a stored session whose access token expired", func(t *testing.T) {
		fake := &fakeAmazon{expireFirst: true}
		p := newTestProvider(t, fake)
		p.tokens = map[string]token{"Atnr|good": {access: "Atna|first", expires: p.Now().Add(time.Hour)}}

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, stored)
			require.NoError(t, err)

			t.Run("THEN the token is renewed once and every page is read", func(t *testing.T) {
				assert.Equal(t, 1, fake.refreshes)
				require.Len(t, copies, 2)
			})

			t.Run("AND each product is one owned library copy, linked to Steam when Amazon says so", func(t *testing.T) {
				assert.Equal(t, "amazon:amzn1.adg.product.1", copies[0].ExternalID)
				assert.Equal(t, "Tunic™", copies[0].Title)
				assert.Equal(t, game.Links{game.LinkSteam: "553420"}, copies[0].Links)
				assert.Equal(t, game.KindLibrary, copies[0].Details.Kind)
				assert.Equal(t, Platform, copies[0].Details.Platform)
				assert.Equal(t, game.StatusOwned, copies[0].Details.Status)
				assert.Empty(t, copies[1].Links)
			})

			t.Run("AND a product without a title is skipped with a warning", func(t *testing.T) {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "amzn1.adg.product.2")
			})
		})
	})

	t.Run("GIVEN a session Amazon revoked (device removed from the account)", func(t *testing.T) {
		p := newTestProvider(t, &fakeAmazon{revoked: true})

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, stored)

			t.Run("THEN it fails with ErrSignedOut", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrSignedOut)
			})
		})
	})
}

func TestCleanCode(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.amazon.com/?openid.oa2.authorization_code=ANabc%2Fdef&openid.mode=id_res": "ANabc/def",
		"  ANxyz  ": "ANxyz",
		`"ANq"`:     "ANq",
	} {
		assert.Equal(t, want, cleanCode(in), in)
	}
}
