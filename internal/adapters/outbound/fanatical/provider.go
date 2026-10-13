// Package fanatical implements the Fanatical source provider: the keys bought on fanatical.com,
// read the way the Playnite Fanatical plugin does, with the session the site keeps in the
// browser's Local Storage (`bsauth`) and one request to the account's key list.
//
// Fanatical's Terms and Conditions forbid taking data from the website into databases without
// their consent. The source therefore needs the user to accept that risk explicitly (a consent
// setting), scans only when asked unless the user schedules it, makes one request per scan and
// never reveals or redeems a key: it copies the keys the user already revealed on the site.
package fanatical

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Type is the source type id of Fanatical accounts.
const Type source.Type = "fanatical"

// TermsURL is Fanatical's Terms and Conditions, linked from the risk warning.
const TermsURL = "https://support.fanatical.com/hc/en-us/articles/115005056629-Terms-and-Conditions"

const (
	settingConsent = "accept_risk"
	settingSession = "session"
	defaultBaseURL = "https://www.fanatical.com"
	userAgent      = "Mozilla/5.0 (compatible; GameVault/1.0; +https://github.com/botchris/game-vault)"
)

var (
	// ErrNoCredentials means nothing usable was pasted.
	ErrNoCredentials = errkind.New(errkind.Invalid, "fanatical: paste the bsauth value from the site's Local Storage (see the help below the field)")

	// ErrSignedOut means Fanatical rejected the session.
	ErrSignedOut = source.SignedOut("fanatical did not accept the session: sign in again on fanatical.com and paste the new bsauth value")
)

// Provider implements sync.Provider for Fanatical accounts.
type Provider struct {
	// log receives what a scan skips on purpose (WithLogger).
	log *slog.Logger

	// API calls fanatical.com; tests point it at a fake server.
	API *apiclient.Client
}

// NewProvider returns the Fanatical source with its production endpoints.
func NewProvider(opts ...Option) *Provider {
	api := apiclient.New(defaultBaseURL)
	api.UserAgent = userAgent

	p := &Provider{API: api}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Fanatical",
		DescriptionKey: "sources.fanatical.description",
		ManualScans:    true,
		Fields: []schema.Field{
			{
				Key:      settingConsent,
				LabelKey: "sources.fanatical.consent",
				HelpKey:  "sources.fanatical.consentHelp",
				HelpURL:  TermsURL,
				Kind:     schema.FieldConsent,
				Required: true,
			},
			{
				Key:      settingSession,
				LabelKey: "sources.fanatical.session",
				HelpKey:  "sources.fanatical.sessionHelp",
				HelpURL:  defaultBaseURL + "/en/",
				SignIn: &schema.SignInRecipe{
					Version: schema.RecipeVersion,
					Open:    defaultBaseURL + "/en/",
					When: &schema.When{
						Contains: `"authenticated":true`,
					},
					Capture: schema.Capture{
						Storage: &schema.StorageCapture{
							Origin: defaultBaseURL,
							Key:    "bsauth",
						},
					},
				},
				Kind:     schema.FieldSecret,
				Required: true,
			},
		},
	}
}

// token accepts the whole bsauth value (JSON with a token) or just the token.
func token(pasted string) string {
	v := strings.TrimSpace(pasted)

	var auth struct {
		Token string `json:"token"`
	}
	if json.Unmarshal([]byte(v), &auth) == nil && auth.Token != "" {
		v = auth.Token
	}

	return strings.ReplaceAll(strings.Trim(v, `"' `), " ", "")
}

// item is one entry of /api/user/keys (shape confirmed with a real account on 2026-10-09). Most
// entries are keys; a bundle bought as one product is also listed, with status "fulfilled" and no
// serial, next to the keys it was split into (each naming it in bundleName).
type item struct {
	ID       string          `json:"_id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Status   string          `json:"status"`
	DRM      map[string]bool `json:"drm"`
	SerialID string          `json:"serialId"`

	// Key is the key itself, present once it has been revealed on the site.
	Key          string `json:"key"`
	SerialExpiry string `json:"serialExpiry"`
	Purchased    string `json:"purchased"`
	BundleName   string `json:"bundleName"`
}

// isBundle reports whether the entry is a bundle purchase already split into its own keys, not a
// key: it has no serial and is "fulfilled".
func (it item) isBundle() bool {
	return it.SerialID == "" && strings.EqualFold(it.Status, "fulfilled")
}

// status is the key's state. A key the list already shows was revealed, whatever the status says.
func (it item) status() game.Status {
	st := statusOf(it.Status)
	if st == game.StatusUnrevealed && strings.TrimSpace(it.Key) != "" {
		return game.StatusRevealed
	}

	return st
}

// origin says where the key came from: the bundle it was part of, when it was.
func (it item) origin() string {
	if b := strings.TrimSpace(it.BundleName); b != "" {
		return "Fanatical – " + b
	}

	return "Fanatical"
}

// keys reads the account's key list.
func (p *Provider) keys(ctx context.Context, settings schema.Settings) ([]item, error) {
	tok := token(settings[settingSession])
	if tok == "" {
		return nil, ErrNoCredentials
	}

	var items []item

	err := p.API.Do(ctx, apiclient.Request{
		Path:   "/api/user/keys",
		Header: http.Header{"Authorization": {tok}},
	}, &items)
	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return nil, ErrSignedOut
	}

	if err != nil {
		// A 429, an outage, no answer or a web page is a passing trouble, not a refused session.
		return nil, fmt.Errorf("fanatical keys: %w", apiclient.ClassifyError(ctx, err))
	}

	return items, nil
}

// drmPlatforms maps Fanatical's DRM flags to Game Vault's platform names, most specific first.
var drmPlatforms = []struct {
	flag     string
	platform string
}{
	{"steam", game.PlatformSteam},
	{"epicgames", game.PlatformEpic},
	{"gog", game.PlatformGOG},
	{"uplay", game.PlatformUbisoft},
	{"origin", game.PlatformEA},
	{"rockstar", game.PlatformRockstar},
	{"xbox", game.PlatformMicrosoft},
	{"playstation", game.PlatformPlayStation},
	{"switch", game.PlatformNintendo},
	{"drm_free", game.SystemPC},
}

func platformOf(drm map[string]bool) string {
	for _, d := range drmPlatforms {
		if drm[d.flag] {
			return d.platform
		}
	}

	return ""
}

// statusOf reads the key's state; anything not known to be revealed or redeemed is unrevealed.
func statusOf(s string) game.Status {
	switch s := strings.ToLower(s); {
	case strings.Contains(s, "redeem"), strings.Contains(s, "activated"):
		return game.StatusRedeemed
	case strings.Contains(s, "unreveal"):
		return game.StatusUnrevealed
	case strings.Contains(s, "reveal"):
		return game.StatusRevealed
	}

	return game.StatusUnrevealed
}

// mapItems turns keys into copies: games only (DLC, software, books, audio and vouchers are
// left out), on the store the key is for. Skipped items are counted (see Fetch). Bundle purchases
// are not keys: they are returned withdrawn, so a copy imported for one before is removed.
func mapItems(items []item) ([]game.ImportedCopy, source.Skipped) {
	var copies []game.ImportedCopy

	skipped := source.Skipped{Store: "Fanatical"}

	for _, it := range items {
		if it.isBundle() {
			if it.ID != "" {
				copies = append(copies, game.ImportedCopy{
					ExternalID: "fanatical:" + it.ID,
					Title:      strings.TrimSpace(it.Name),
					Withdrawn:  true,
				})
			}

			continue
		}

		platform := platformOf(it.DRM)
		if it.ID == "" || strings.TrimSpace(it.Name) == "" || !strings.EqualFold(it.Type, "game") || platform == "" {
			skipped.Add("DLC, software, books, audio or keys for an unknown store")
			continue
		}

		copies = append(copies, game.ImportedCopy{
			ExternalID: "fanatical:" + it.ID,
			Title:      strings.TrimSpace(it.Name),
			Details: game.CopyDetails{
				Kind:       game.KindKey,
				Platform:   platform,
				Status:     it.status(),
				Key:        strings.TrimSpace(it.Key),
				Origin:     it.origin(),
				RedeemBy:   game.ParseStoreDate(it.SerialExpiry),
				AcquiredOn: game.ParseStoreDate(it.Purchased),
			},
		})
	}

	return copies, skipped
}

// Test implements sync.Provider: it reads the key list once.
func (p *Provider) Test(ctx context.Context, settings schema.Settings) error {
	_, err := p.keys(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings schema.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.keys(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	copies, skipped := mapItems(items)
	skipped.Log(p.log)

	return copies, nil, nil
}

// Option configures a Provider.
type Option func(*Provider)

// WithLogger sends what a scan skips on purpose (DLC, subscription games, what is not a game) to
// log, one line per reason with its count. Without it nothing is logged.
func WithLogger(log *slog.Logger) Option {
	return func(p *Provider) { p.log = log }
}
