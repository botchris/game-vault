package schema

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// RecipeVersion is the version of the recipe format and of the primitives it uses. The browser
// extension refuses recipes newer than the ones it knows (the user then updates it).
const RecipeVersion = 1

// maxTimeoutSeconds bounds how long the extension waits for the user to sign in.
const maxTimeoutSeconds = 600

var reHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// SignInRecipe tells the Game Vault Connector browser extension how to collect a setting's value
// from the store's own sign-in: what to open, when the user counts as signed in, and what to
// capture then. It is data the extension interprets, never code.
type SignInRecipe struct {
	Version int    `json:"version"`
	Open    string `json:"open,omitempty"` // empty: the field's HelpURL (a per-run link such as Amazon's)

	// Private prefers a private window when the user allowed the extension in one (Ubisoft: its
	// ticket rotates when Game Vault uses it, which would sign the normal browser out).
	Private bool `json:"private,omitempty"`

	// Hosts are other hosts the capture or condition may read (EA's accounts.ea.com); the user
	// confirms each one.
	Hosts          []string `json:"hosts,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"` // 0: five minutes

	// When is the readiness condition; nil: the capture itself is the condition (a redirect, a
	// fetch whose field only exists once signed in).
	When    *When   `json:"when,omitempty"`
	Capture Capture `json:"capture"`
}

// When tells the extension that the user is signed in. Every condition set must hold.
type When struct {
	URLPrefix string        `json:"urlPrefix,omitempty"` // the tab is on an address starting so
	Contains  string        `json:"contains,omitempty"`  // the captured value contains this text
	Fetch     *FetchCapture `json:"fetch,omitempty"`     // this field is present when fetched with the session
}

// Capture is what the extension reads once the user is signed in: exactly one member is set.
type Capture struct {
	Cookie   *CookieCapture   `json:"cookie,omitempty"`
	Cookies  *CookiesCapture  `json:"cookies,omitempty"`
	Storage  *StorageCapture  `json:"storage,omitempty"`
	Redirect *RedirectCapture `json:"redirect,omitempty"`
	Fetch    *FetchCapture    `json:"fetch,omitempty"`
}

// CookieCapture reads one cookie's value (HttpOnly ones included).
type CookieCapture struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// CookiesCapture reads every cookie sent to an address, as a Cookie header ("a=1; b=2").
type CookiesCapture struct {
	URL string `json:"url"`
}

// StorageCapture reads a localStorage value from the tab when it is on Origin (and Path, if set).
type StorageCapture struct {
	Origin string `json:"origin"`
	Key    string `json:"key"`
	Path   string `json:"path,omitempty"`
}

// RedirectCapture reads a query parameter of the address the store redirects to.
type RedirectCapture struct {
	Prefix string `json:"prefix"`
	Param  string `json:"param"`
}

// FetchCapture reads a field of the JSON an address answers when fetched with the user's session.
type FetchCapture struct {
	URL   string `json:"url"`
	Field string `json:"field"`
}

// WithOpen returns the recipe with an empty Open replaced by helpURL.
func (r SignInRecipe) WithOpen(helpURL string) SignInRecipe {
	if r.Open == "" {
		r.Open = helpURL
	}

	return r
}

// Validate checks the recipe the way the extension does: a known version, https addresses only,
// exactly one capture, and every address on the opened host or a listed one.
func (r SignInRecipe) Validate(helpURL string) error {
	r = r.WithOpen(helpURL)
	if r.Version < 1 || r.Version > RecipeVersion {
		return fmt.Errorf("sign-in recipe version %d is not supported", r.Version)
	}

	if r.TimeoutSeconds < 0 || r.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("sign-in recipe timeout must be between 0 and %d seconds", maxTimeoutSeconds)
	}

	open, err := httpsHost(r.Open)
	if err != nil {
		return err
	}

	allowed := map[string]bool{open: true}

	for _, h := range r.Hosts {
		if !reHost.MatchString(h) {
			return fmt.Errorf("sign-in recipe host %q is not a host name", h)
		}

		allowed[h] = true
	}

	addresses, err := r.Capture.addresses()
	if err != nil {
		return err
	}

	// A redirect is its own condition: a When on it would never be checked.
	if r.When != nil && r.Capture.Redirect != nil {
		return errors.New("a redirect sign-in recipe takes no condition")
	}

	// The extension's own requests carry the normal window's session, not the private one's.
	if r.Private && (r.Capture.Fetch != nil || r.When != nil && r.When.Fetch != nil) {
		return errors.New("a private sign-in recipe cannot fetch")
	}

	if r.When != nil {
		if r.When.URLPrefix != "" {
			addresses = append(addresses, r.When.URLPrefix)
		}

		if f := r.When.Fetch; f != nil {
			if f.Field == "" {
				return errors.New("sign-in recipe condition needs a field")
			}

			addresses = append(addresses, f.URL)
		}
	}

	for _, a := range addresses {
		h, err := httpsHost(a)
		if err != nil {
			return err
		}

		if !allowed[h] {
			return fmt.Errorf("sign-in recipe reads %s, which it neither opens nor lists", h)
		}
	}

	return nil
}

// addresses returns the capture's addresses, checking that exactly one capture is set and complete.
func (c Capture) addresses() ([]string, error) {
	var (
		set  int
		out  []string
		miss bool
	)

	if v := c.Cookie; v != nil {
		set++

		out = append(out, v.URL)
		miss = v.Name == ""
	}

	if v := c.Cookies; v != nil {
		set++

		out = append(out, v.URL)
	}

	if v := c.Storage; v != nil {
		set++

		out = append(out, v.Origin)
		miss = miss || v.Key == ""
	}

	if v := c.Redirect; v != nil {
		set++

		out = append(out, v.Prefix)
		miss = miss || v.Param == ""
	}

	if v := c.Fetch; v != nil {
		set++

		out = append(out, v.URL)
		miss = miss || v.Field == ""
	}

	switch {
	case set != 1:
		return nil, errors.New("a sign-in recipe needs exactly one capture")
	case miss:
		return nil, errors.New("the sign-in recipe's capture is incomplete")
	}

	return out, nil
}

func httpsHost(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("sign-in recipe address %q must be an https address", address)
	}

	return strings.ToLower(u.Hostname()), nil
}
