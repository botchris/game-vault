// Package browsersession reuses a website session copied from the user's browser, for stores
// whose sign-in cannot be automated (captchas) and that have no API of their own: the user pastes
// the site's cookies and providers send them, keeping any cookie the site renews on the way.
package browsersession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// Parse accepts what DevTools shows: a "Cookie:" request header ("a=1; b=2"), rows copied from the
// Application → Cookies table (tab separated) or a single "name=value".
func Parse(raw string) map[string]string {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "Cookie:")
	raw = strings.TrimPrefix(raw, "cookie:")
	if strings.Contains(raw, "\t") {
		for _, line := range strings.Split(raw, "\n") {
			f := strings.Split(line, "\t")
			if len(f) >= 2 && strings.TrimSpace(f[0]) != "" {
				out[strings.TrimSpace(f[0])] = strings.TrimSpace(f[1])
			}
		}
		return out
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '\n' }) {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(name) != "" {
			out[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return out
}

// state holds cookies the site renewed, tied to the pasted cookies they derive from.
type state struct {
	From    string            `json:"from"` // hash of the pasted cookies
	Cookies map[string]string `json:"cookies"`
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Current returns the cookies to send: the pasted ones, overridden by the ones renewed from them
// (renewals of an older paste are ignored).
func Current(pasted, rawState string) map[string]string {
	out := Parse(pasted)
	var s state
	if json.Unmarshal([]byte(rawState), &s) == nil && s.From == hash(pasted) {
		for k, v := range s.Cookies {
			out[k] = v
		}
	}
	return out
}

// Remember returns the new state after the site renewed some cookies ("" changes nothing).
func Remember(pasted, rawState string, renewed map[string]string) string {
	if len(renewed) == 0 {
		return rawState
	}
	var s state
	if json.Unmarshal([]byte(rawState), &s) != nil || s.From != hash(pasted) || s.Cookies == nil {
		s = state{From: hash(pasted), Cookies: map[string]string{}}
	}
	for k, v := range renewed {
		s.Cookies[k] = v
	}
	raw, _ := json.Marshal(s)
	return string(raw)
}

// Session is an HTTP client whose cookie jar starts with the given cookies, valid on every host of
// the site's domain (sign-ins often hop between subdomains).
type Session struct {
	Client *http.Client
	jar    *cookiejar.Jar
	base   *url.URL
	sent   map[string]string
}

// New starts a session on baseURL. domain is the cookie domain ("battle.net"); it is ignored for
// other hosts (tests on 127.0.0.1).
func New(baseURL, domain string, cookies map[string]string, timeout time.Duration) (*Session, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	if !strings.HasSuffix(base.Hostname(), domain) {
		domain = ""
	}
	var list []*http.Cookie
	for k, v := range cookies {
		list = append(list, &http.Cookie{Name: k, Value: v, Path: "/", Domain: domain, Secure: base.Scheme == "https"})
	}
	jar.SetCookies(base, list)
	return &Session{Client: &http.Client{Jar: jar, Timeout: timeout}, jar: jar, base: base, sent: cookies}, nil
}

// Renewed returns the cookies the site set or changed during the session.
func (s *Session) Renewed() map[string]string {
	out := map[string]string{}
	for _, c := range s.jar.Cookies(s.base) {
		if s.sent[c.Name] != c.Value {
			out[c.Name] = c.Value
		}
	}
	return out
}
