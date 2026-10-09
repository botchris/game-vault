package humble

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

// MapOrders converts raw Humble orders, as returned by /api/v1/orders, into imported key copies.
// now is used for relative expiry dates. Keys that are not games are returned withdrawn, and their
// titles listed in skipped.
func MapOrders(orders []json.RawMessage, now time.Time) (copies []game.ImportedCopy, skipped, warnings []string) {
	for _, raw := range orders {
		var o struct {
			Gamekey string `json:"gamekey"`
			Created string `json:"created"`
			Product struct {
				HumanName string `json:"human_name"`
			} `json:"product"`
			TpkdDict struct {
				AllTpks []map[string]any `json:"all_tpks"`
			} `json:"tpkd_dict"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			warnings = append(warnings, "unreadable order: "+err.Error())
			continue
		}

		acquired := parseDate(o.Created)

		origin := "Humble Bundle"
		if o.Product.HumanName != "" {
			origin = "Humble Bundle – " + o.Product.HumanName
		}

		for _, t := range o.TpkdDict.AllTpks {
			title := strings.TrimSpace(str(t, "human_name"))
			if title == "" {
				continue
			}

			gamekey := str(t, "gamekey")
			if gamekey == "" {
				gamekey = o.Gamekey
			}

			key := str(t, "redeemed_key_val")
			status := game.StatusUnrevealed

			switch {
			case boolean(t, "is_expired"):
				status = game.StatusExpired
			case boolean(t, "is_gift") || strings.HasPrefix(key, "http"):
				status = game.StatusGifted
			case key != "":
				status = game.StatusRevealed
			}

			// keyindex only numbers repeated copies of the same game in an order (almost always
			// 0), so the key is told apart by its machine name. Older versions used
			// gamekey:keyindex alone, which folded every key of an order into one copy.
			previous := fmt.Sprintf("humble:%s:%v", gamekey, t["keyindex"])
			id := previous

			if machine := str(t, "machine_name"); machine != "" {
				id = fmt.Sprintf("humble:%s:%s:%v", gamekey, machine, t["keyindex"])
			}

			platform := platformFor(str(t, "key_type"), str(t, "key_type_human_name"))
			if !isGame(title, platform) {
				// Software, courses, in-game items and store coupons come in bundles too. They are
				// reported as withdrawn so a copy imported before this filter existed is removed.
				copies = append(copies, game.ImportedCopy{ExternalID: id, PreviousExternalID: previous, Title: title, Withdrawn: true})
				skipped = append(skipped, title)

				continue
			}

			copies = append(copies, game.ImportedCopy{
				ExternalID:         id,
				PreviousExternalID: previous,
				Title:              title,
				Links:              steamLink(t),
				Details: game.CopyDetails{
					Kind:       game.KindKey,
					Platform:   platform,
					Status:     status,
					Key:        key,
					RedeemBy:   deadline(t, now),
					Origin:     origin,
					AcquiredOn: acquired,
				},
			})
		}
	}

	return copies, skipped, warnings
}

// couponTitle matches store coupons ("45% off Street Fighter V PS Store Coupon").
var couponTitle = regexp.MustCompile(`(?i)\bcoupon\b|^\d+\s*% off\b`)

// isGame reports whether a key unlocks a game: a key for a store or console Game Vault knows that
// is not a coupon. Humble also sells keys for software (Ashampoo, Corel), courses (Udemy), in-game
// items (Duelyst, Sega) and the like, each with its own key type.
func isGame(title, platform string) bool {
	return !couponTitle.MatchString(title) && game.IsKnownPlatform(platform)
}

func platformFor(keyType, human string) string {
	k := strings.ToLower(keyType)
	switch {
	case strings.Contains(k, "steam"):
		return "Steam"
	case strings.Contains(k, "origin"), k == "ea":
		return "EA App"
	case strings.Contains(k, "uplay"), strings.Contains(k, "ubisoft"):
		return "Ubisoft Connect"
	case strings.Contains(k, "gog"):
		return "GOG"
	case strings.Contains(k, "epic"):
		return "Epic Games"
	case strings.Contains(k, "battle"), strings.Contains(k, "blizzard"):
		return "Battle.net"
	case strings.Contains(k, "rockstar"):
		return "Rockstar"
	case strings.Contains(k, "microsoft"), strings.Contains(k, "xbox"):
		return "Microsoft Store / Xbox"
	case strings.Contains(k, "nintendo"), strings.Contains(k, "switch"):
		return "Nintendo eShop"
	case human != "":
		return human
	}

	return keyType
}

var deadlinePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:redeem|claim|activate)[^.<]{0,40}?(?:by|before)\s+([A-Z][a-z]+\.? \d{1,2},? \d{4})`),
	regexp.MustCompile(`(?i)(?:expires?|expiration)[^.<]{0,20}?(?:on)?\s*([A-Z][a-z]+\.? \d{1,2},? \d{4})`),
	regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`),
}

// deadline finds the redeem-by date. Humble is not consistent about where it stores it.
func deadline(t map[string]any, now time.Time) game.Date {
	for _, f := range []string{"expiry_date", "expiration_date", "expires", "expiry", "redeem_by"} {
		if d := parseDate(t[f]); d != "" {
			return d
		}
	}

	if v, ok := t["num_days_until_expired"].(float64); ok && v >= 0 {
		return game.DateOf(now.AddDate(0, 0, int(v)))
	}

	html := str(t, "custom_instructions_html")
	for _, re := range deadlinePatterns {
		if m := re.FindStringSubmatch(html); m != nil {
			if d := parseDate(strings.ReplaceAll(m[1], ".", "")); d != "" {
				return d
			}
		}
	}

	return ""
}

var dateLayouts = []string{
	"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", time.RFC3339, time.DateOnly,
	"January 2, 2006", "January 2 2006", "Jan 2, 2006", "Jan 2 2006",
}

func parseDate(v any) game.Date {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return game.DateOf(time.Unix(int64(x), 0).UTC())
		}
	case string:
		x = strings.TrimSpace(x)
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, x); err == nil {
				return game.DateOf(t)
			}
		}
	}

	return ""
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func boolean(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func num(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case string:
		var f float64

		_, _ = fmt.Sscan(v, &f) // anything that is not a number counts as zero

		return f
	}

	return 0
}

// steamLink is the Steam AppID Humble gives a Steam key, as a link (nil for other keys).
func steamLink(t map[string]any) game.Links {
	if id := int64(num(t, "steam_app_id")); id > 0 {
		return game.Links{game.LinkSteam: strconv.FormatInt(id, 10)}
	}

	return nil
}
