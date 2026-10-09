// Package fanatical implements the Fanatical source provider: the keys bought on fanatical.com,
// read the way the Playnite Fanatical plugin does, with the session the site keeps in the
// browser's Local Storage (`bsauth`) and one request to the account's key list.
//
// Fanatical's Terms and Conditions forbid taking data from the website into databases without
// their consent. The source therefore needs the user to accept that risk explicitly (a consent
// setting), scans only when asked unless the user schedules it, makes one request per scan and
// never reveals or redeems a key.
package fanatical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	// ErrNoSession means nothing usable was pasted.
	ErrNoSession = errors.New("fanatical: paste the bsauth value from the site's Local Storage (see the help below the field)")

	// ErrSignedOut means Fanatical rejected the session.
	ErrSignedOut = errors.New("fanatical did not accept the session: sign in again on fanatical.com and paste the new bsauth value")
)

// Provider implements sync.Provider for Fanatical accounts.
type Provider struct {
	BaseURL string
	Client  *http.Client
}

// NewProvider returns the Fanatical source with its production endpoints.
func NewProvider() *Provider {
	return &Provider{BaseURL: defaultBaseURL, Client: &http.Client{Timeout: 60 * time.Second}}
}

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type:           Type,
		Name:           "Fanatical",
		DescriptionKey: "sources.fanatical.description",
		ManualScans:    true,
		Fields: []source.Field{
			{
				Key: settingConsent, LabelKey: "sources.fanatical.consent", HelpKey: "sources.fanatical.consentHelp",
				HelpURL: TermsURL, Kind: schema.FieldConsent, Required: true,
			},
			{
				Key: settingSession, LabelKey: "sources.fanatical.session", HelpKey: "sources.fanatical.sessionHelp",
				HelpURL: defaultBaseURL + "/en/", Kind: schema.FieldSecret, Required: true,
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
	ID           string          `json:"_id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	DRM          map[string]bool `json:"drm"`
	SerialID     string          `json:"serialId"`
	SerialExpiry string          `json:"serialExpiry"`
	Purchased    string          `json:"purchased"`
	BundleName   string          `json:"bundleName"`
}

// isBundle reports whether the entry is a bundle purchase already split into its own keys, not a
// key: it has no serial and is "fulfilled".
func (it item) isBundle() bool {
	return it.SerialID == "" && strings.EqualFold(it.Status, "fulfilled")
}

// origin says where the key came from: the bundle it was part of, when it was.
func (it item) origin() string {
	if b := strings.TrimSpace(it.BundleName); b != "" {
		return "Fanatical – " + b
	}

	return "Fanatical"
}

// keys reads the account's key list.
func (p *Provider) keys(ctx context.Context, settings source.Settings) ([]item, error) {
	tok := token(settings[settingSession])
	if tok == "" {
		return nil, ErrNoSession
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api/user/keys", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", tok)
	req.Header.Set("User-Agent", userAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, ErrSignedOut
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fanatical keys: HTTP %d", res.StatusCode)
	}

	var items []item
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, errors.New("fanatical keys: unexpected answer (the site may have changed)")
	}

	return items, nil
}

// drmPlatforms maps Fanatical's DRM flags to Game Vault's platform names, most specific first.
var drmPlatforms = []struct{ flag, platform string }{
	{"steam", "Steam"}, {"epicgames", "Epic Games"}, {"gog", "GOG"}, {"uplay", "Ubisoft Connect"},
	{"origin", "EA App"}, {"rockstar", "Rockstar"}, {"xbox", "Microsoft Store / Xbox"},
	{"playstation", "PlayStation Store"}, {"switch", "Nintendo eShop"}, {"drm_free", "PC"},
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

func date(v string) game.Date {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", time.DateOnly} {
		if t, err := time.Parse(layout, strings.TrimSpace(v)); err == nil {
			return game.DateOf(t)
		}
	}

	return ""
}

// mapItems turns keys into copies: games only (DLC, software, books, audio and vouchers are
// left out), on the store the key is for. Skipped items are counted in a warning. Bundle purchases
// are not keys: they are returned withdrawn, so a copy imported for one before is removed.
func mapItems(items []item) ([]game.ImportedCopy, []string) {
	var (
		copies  []game.ImportedCopy
		skipped int
	)

	for _, it := range items {
		if it.isBundle() {
			if it.ID != "" {
				copies = append(copies, game.ImportedCopy{ExternalID: "fanatical:" + it.ID, Title: strings.TrimSpace(it.Name), Withdrawn: true})
			}

			continue
		}

		platform := platformOf(it.DRM)
		if it.ID == "" || strings.TrimSpace(it.Name) == "" || !strings.EqualFold(it.Type, "game") || platform == "" {
			skipped++
			continue
		}

		copies = append(copies, game.ImportedCopy{
			ExternalID: "fanatical:" + it.ID,
			Title:      strings.TrimSpace(it.Name),
			Details: game.CopyDetails{
				Kind: game.KindKey, Platform: platform, Status: statusOf(it.Status), Origin: it.origin(),
				RedeemBy: date(it.SerialExpiry), AcquiredOn: date(it.Purchased),
			},
		})
	}

	var warnings []string
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d Fanatical items were not imported: DLC, software, books, audio or keys for an unknown store", skipped))
	}

	return copies, warnings
}

// Test implements sync.Provider: it reads the key list once.
func (p *Provider) Test(ctx context.Context, settings source.Settings) error {
	_, err := p.keys(ctx, settings)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, settings source.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.keys(ctx, settings)
	if err != nil {
		return nil, nil, err
	}

	copies, warnings := mapItems(items)

	return copies, warnings, nil
}
