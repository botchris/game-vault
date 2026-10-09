package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func humbleRecipe() SignInRecipe {
	return SignInRecipe{
		Version: 1,
		Open:    "https://www.humblebundle.com/home/keys",
		When:    &When{URLPrefix: "https://www.humblebundle.com/home/keys"},
		Capture: Capture{Cookie: &CookieCapture{
			URL:  "https://www.humblebundle.com",
			Name: "_simpleauth_sess",
		}},
	}
}

func TestSignInRecipe_validate(t *testing.T) {
	t.Run("GIVEN well-formed recipes of every kind", func(t *testing.T) {
		ok := []SignInRecipe{
			humbleRecipe(),
			{
				Version: 1,
				Open:    "https://account.battle.net/overview",
				When:    &When{URLPrefix: "https://account.battle.net/overview"},
				Capture: Capture{Cookies: &CookiesCapture{URL: "https://account.battle.net/overview"}},
			},
			{
				Version: 1,
				Open:    "https://www.ea.com/",
				Hosts:   []string{"accounts.ea.com"},
				When: &When{Fetch: &FetchCapture{
					URL:   "https://accounts.ea.com/connect/auth?x=1",
					Field: "access_token",
				}},
				Capture: Capture{Cookies: &CookiesCapture{URL: "https://accounts.ea.com/connect/auth?x=1"}},
			},
			{
				Version: 1,
				Open:    "https://connect.ubisoft.com/login?appId=x",
				Private: true,
				Capture: Capture{Storage: &StorageCapture{
					Origin: "https://connect.ubisoft.com",
					Key:    "PRODrememberMe",
					Path:   "/ready",
				}},
			},
			{
				Version: 1,
				Open:    "https://auth.gog.com/auth?x=1",
				Hosts:   []string{"embed.gog.com"},
				Capture: Capture{Redirect: &RedirectCapture{
					Prefix: "https://embed.gog.com/on_login_success",
					Param:  "code",
				}},
			},
			{
				Version: 1,
				Open:    "https://www.epicgames.com/id/login?x=1",
				Capture: Capture{Fetch: &FetchCapture{
					URL:   "https://www.epicgames.com/id/api/redirect?x=1",
					Field: "authorizationCode",
				}},
			},
		}

		t.Run("THEN each is valid", func(t *testing.T) {
			for _, r := range ok {
				assert.NoError(t, r.Validate(""), r.Open)
			}
		})
	})

	t.Run("GIVEN a recipe whose address comes from the field's help link (Amazon)", func(t *testing.T) {
		r := SignInRecipe{
			Version: 1,
			Capture: Capture{Redirect: &RedirectCapture{
				Prefix: "https://www.amazon.com/",
				Param:  "openid.oa2.authorization_code",
			}},
		}

		t.Run("THEN it is valid with an https help link, and WithOpen fills it", func(t *testing.T) {
			assert.NoError(t, r.Validate("https://www.amazon.com/ap/signin?x=1"))
			assert.Error(t, r.Validate(""))
			assert.Equal(t, "https://www.amazon.com/ap/signin?x=1", r.WithOpen("https://www.amazon.com/ap/signin?x=1").Open)
		})
	})

	t.Run("GIVEN broken recipes", func(t *testing.T) {
		bad := map[string]func(*SignInRecipe){
			"a newer version": func(r *SignInRecipe) { r.Version = 2 },
			"no version":      func(r *SignInRecipe) { r.Version = 0 },
			"an http address": func(r *SignInRecipe) { r.Open = "http://www.humblebundle.com/" },
			"no capture":      func(r *SignInRecipe) { r.Capture = Capture{} },
			"two captures": func(r *SignInRecipe) {
				r.Capture.Fetch = &FetchCapture{
					URL:   "https://www.humblebundle.com/x",
					Field: "a",
				}
			},
			"a capture on another host":   func(r *SignInRecipe) { r.Capture.Cookie.URL = "https://evil.example" },
			"a condition on another host": func(r *SignInRecipe) { r.When.URLPrefix = "https://evil.example/" },
			"a cookie without a name":     func(r *SignInRecipe) { r.Capture.Cookie.Name = "" },
			"a malformed host":            func(r *SignInRecipe) { r.Hosts = []string{"https://x.com/"} },
			"a timeout over ten minutes":  func(r *SignInRecipe) { r.TimeoutSeconds = 601 },
			"a negative timeout":          func(r *SignInRecipe) { r.TimeoutSeconds = -1 },
			// A condition on a redirect would never be checked: refused rather than ignored.
			"a condition on a redirect": func(r *SignInRecipe) {
				r.Capture = Capture{Redirect: &RedirectCapture{
					Prefix: "https://www.humblebundle.com/done",
					Param:  "code",
				}}
			},
			// The extension fetches with the normal window's session, not the private one.
			"a private fetch condition": func(r *SignInRecipe) {
				r.Private = true
				r.When = &When{Fetch: &FetchCapture{
					URL:   "https://www.humblebundle.com/api",
					Field: "a",
				}}
			},
			"a private fetch capture": func(r *SignInRecipe) {
				r.Private = true
				r.When = nil
				r.Capture = Capture{Fetch: &FetchCapture{
					URL:   "https://www.humblebundle.com/api",
					Field: "a",
				}}
			},
		}

		t.Run("THEN each is refused", func(t *testing.T) {
			for name, change := range bad {
				r := humbleRecipe()
				change(&r)
				assert.Error(t, r.Validate(""), name)
			}
		})
	})
}
