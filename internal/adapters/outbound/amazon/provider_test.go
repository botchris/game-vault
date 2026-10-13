package amazon

import (
	"context"
	"encoding/json"
	"fmt"
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
	"gamevault/internal/domain/provider"
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
	accountStatus  int // when set, the account API (register, token) answers only this status
	libraryStatus  int // when set, the entitlements service answers only this status
}

func (f *fakeAmazon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)

	var body map[string]any

	_ = json.Unmarshal(raw, &body)

	if f.accountStatus != 0 && strings.HasPrefix(r.URL.Path, "/auth/") {
		w.WriteHeader(f.accountStatus)
		return
	}

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
	case "/api/distribution/entitlements":
		if f.libraryStatus != 0 {
			w.WriteHeader(f.libraryStatus)
			return
		}

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

	p := NewProvider()
	p.SignInURL = "https://amazon.test/ap/signin"
	p.API.BaseURL, p.Gaming.BaseURL = srv.URL, srv.URL
	p.Now = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }

	return p
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

	t.Run("GIVEN a session just signed in whose access token the library rejects before its time", func(t *testing.T) {
		fake := &fakeAmazon{expireFirst: true}
		p := newTestProvider(t, fake)

		// The registration's access token ("Atna|first") is cached for an hour.
		settings, err := p.Prepare(ctx, source.Settings{settingCode: "GOODCODE"})
		require.NoError(t, err)

		t.Run("WHEN the library is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, settings)
			require.NoError(t, err)

			t.Run("THEN the token is renewed once and every page is read", func(t *testing.T) {
				assert.Equal(t, 1, fake.refreshes)
				require.Len(t, copies, 2)
			})

			t.Run("AND each product is one owned library copy, linked to Amazon, and to Steam when Amazon says so", func(t *testing.T) {
				assert.Equal(t, "amazon:amzn1.adg.product.1", copies[0].ExternalID)
				assert.Equal(t, "Tunic™", copies[0].Title)
				assert.Equal(t, game.Links{"amazon": "amzn1.adg.product.1", game.LinkSteam: "553420"}, copies[0].Links)
				assert.Equal(t, game.KindLibrary, copies[0].Details.Kind)
				assert.Equal(t, Platform, copies[0].Details.Platform)
				assert.Equal(t, game.StatusOwned, copies[0].Details.Status)
				assert.Equal(t, "Amazon Games", copies[0].Details.Origin, "the store, like the other library sources")
				assert.Equal(t, game.Links{"amazon": "amzn1.adg.product.3"}, copies[1].Links)
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

// TestAccountAPI_passingTroubles checks that a rate limit or an outage of the account API is not
// taken for a rejected code or session: signing in again would not help.
func TestAccountAPI_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for status, want := range map[int]error{
		http.StatusTooManyRequests:    provider.ErrRateLimited,
		http.StatusServiceUnavailable: provider.ErrUnavailable,
	} {
		t.Run(fmt.Sprintf("GIVEN the account API answers HTTP %d", status), func(t *testing.T) {
			p := newTestProvider(t, &fakeAmazon{accountStatus: status})

			t.Run("WHEN a code is pasted", func(t *testing.T) {
				_, err := p.Prepare(ctx, source.Settings{settingCode: "GOODCODE"})

				t.Run("THEN it is a passing trouble, not a rejected code", func(t *testing.T) {
					require.ErrorIs(t, err, want)
					assert.NotErrorIs(t, err, ErrBadCode)
				})
			})

			t.Run("WHEN the library is scanned with a stored session", func(t *testing.T) {
				_, _, err := p.Fetch(ctx, source.Settings{settingSession: `{"refresh_token":"Atnr|good","serial":"SERIAL"}`})

				t.Run("THEN it is a passing trouble, not a signed-out session", func(t *testing.T) {
					require.ErrorIs(t, err, want)
					assert.NotErrorIs(t, err, source.ErrSignedOut)
				})
			})
		})
	}
}

// TestLibrary_passingTroubles checks that a rate limit, an outage or no answer from the
// entitlements service is reported as a passing trouble, not as a bare HTTP error.
func TestLibrary_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for status, want := range map[int]error{
		http.StatusTooManyRequests:    provider.ErrRateLimited,
		http.StatusServiceUnavailable: provider.ErrUnavailable,
	} {
		t.Run(fmt.Sprintf("GIVEN the entitlements service answers HTTP %d", status), func(t *testing.T) {
			p := newTestProvider(t, &fakeAmazon{libraryStatus: status})

			t.Run("WHEN the library is scanned with a stored session", func(t *testing.T) {
				_, _, err := p.Fetch(ctx, source.Settings{settingSession: `{"refresh_token":"Atnr|good","serial":"SERIAL"}`})

				t.Run("THEN it is a passing trouble, not a signed-out session", func(t *testing.T) {
					require.ErrorIs(t, err, want)
					assert.NotErrorIs(t, err, source.ErrSignedOut)
				})
			})
		})
	}
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
