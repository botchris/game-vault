// Package csvfile implements transfer.Codec for CSV files: one row per copy.
//
// Columns, in English only (header names are case-insensitive; unknown columns are reported and
// ignored):
//
//	title, platform, kind, status, key, redeemBy, origin, acquiredOn, edition, grade, contents,
//	location, price, currency, notes, links, externalId, barcode, system
//
// grade is one of sealed, mint, very_good, good, acceptable, damaged; contents lists box, manual,
// media and extras separated by spaces; price is an amount with a dot or a comma and at most the
// currency's decimals (29.95, 1500 for JPY), and currency its ISO 4217 code. A price without
// currency is left without one: the importer applies the default currency.
//
// system is the system the copy is played on (PC, PS4, Switch…). The export writes each copy's
// effective system; the import keeps a value only when it differs from the one the platform
// implies (it is then the copy's own system), so an export imported again changes nothing.
//
// links lists the stores the game is linked to as space-separated store:id pairs
// ("steam:620 gog:1207658924"); every row of a game carries the game's links.
//
// Rows without externalId get a stable one derived from kind, platform, title and key, so the
// same file can be imported again to update rows instead of duplicating them.
package csvfile

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

var columns = []string{
	"title", "platform", "kind", "status", "key", "redeemBy", "origin", "acquiredOn", "edition",
	"grade", "contents", "location", "price", "currency", "notes", "links", "externalId", "barcode",
	"system",
}

var aliases = map[string]string{
	"game":   "title",
	"store":  "platform",
	"type":   "kind",
	"bundle": "origin",
	"ean":    "barcode",
	"upc":    "barcode",
}

var kindAliases = map[string]game.Kind{
	"key":      game.KindKey,
	"cdkey":    game.KindKey,
	"library":  game.KindLibrary,
	"physical": game.KindPhysical,
}

var dateLayouts = []string{time.DateOnly, "02/01/2006", "2/1/2006", "2006/01/02"}

// Codec implements transfer.Codec.
type Codec struct{}

// Extension returns the file extension of the format, without the dot.
func (Codec) Extension() string { return "csv" }

func normalizeHeader(h string) string {
	k := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))

	k = strings.NewReplacer("_", "", " ", "", "-", "", "í", "i", "á", "a", "ó", "o", "é", "e").Replace(k)
	for _, c := range columns {
		if strings.ToLower(c) == k {
			return c
		}
	}

	return aliases[k]
}

// Decode reads a CSV export (or a compatible spreadsheet) into imported copies and warnings.
func (Codec) Decode(r io.Reader) ([]game.ImportedCopy, []string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}

	text := string(data)
	firstLine, _, _ := strings.Cut(text, "\n")

	cr := csv.NewReader(strings.NewReader(text))
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		cr.Comma = ';' // spreadsheet apps in many locales export with ';'
	}

	cr.FieldsPerRecord = -1

	records, err := cr.ReadAll()
	if err != nil {
		return nil, nil, err
	}

	if len(records) < 2 {
		return nil, nil, fmt.Errorf("the CSV has no data rows")
	}

	var (
		out      []game.ImportedCopy
		warnings []string
		idx      = map[string]int{}
	)

	for i, h := range records[0] {
		k := normalizeHeader(h)
		if k == "" {
			if h = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")); h != "" {
				warnings = append(warnings, fmt.Sprintf("column %q is not a Game Vault column, ignored (download the template for the column names)", h))
			}

			continue
		}

		idx[k] = i
	}

	if _, ok := idx["title"]; !ok {
		return nil, nil, fmt.Errorf("missing required column 'title'")
	}

	for n, rec := range records[1:] {
		row := n + 2
		get := func(col string) string {
			if i, ok := idx[col]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}

			return ""
		}

		title := get("title")
		if title == "" {
			continue
		}

		d := game.CopyDetails{
			Platform: get("platform"),
			Status:   game.Status(strings.ToLower(get("status"))),
			Key:      get("key"),
			Origin:   get("origin"),
			Edition:  get("edition"),
			Location: get("location"),
			Notes:    get("notes"),
		}

		if s := get("system"); s != "" && game.SystemOf(s) != game.SystemOf(d.Platform) {
			d.System = s // the domain checks it and names it like SystemOf when the copy is saved
		}

		var ok bool
		if d.Kind, ok = kindAliases[strings.ToLower(get("kind"))]; !ok {
			if d.Key != "" {
				d.Kind = game.KindKey
			} else {
				d.Kind = game.KindPhysical
			}
		}

		if !d.Kind.Allows(d.Status) {
			if d.Status != "" {
				warnings = append(warnings, fmt.Sprintf("row %d: status %q not valid for %s, using default", row, d.Status, d.Kind))
			}

			d.Status = ""
		}

		for _, f := range []struct {
			col string
			dst *game.Date
		}{{"redeemBy", &d.RedeemBy}, {"acquiredOn", &d.AcquiredOn}} {
			if v := get(f.col); v != "" {
				if parsed, ok := parseDate(v); ok {
					*f.dst = parsed
				} else {
					warnings = append(warnings, fmt.Sprintf("row %d: unrecognized date %q (use YYYY-MM-DD)", row, v))
				}
			}
		}

		if v := get("barcode"); v != "" {
			if b, err := game.ParseBarcode(v); err == nil {
				d.Barcode = b
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: %v", row, err))
			}
		}

		links, bad := parseLinks(get("links"))
		for _, b := range bad {
			warnings = append(warnings, fmt.Sprintf("row %d: link %q is not store:id (e.g. steam:620), ignored", row, b))
		}

		if v := get("grade"); v != "" {
			if g := game.Grade(strings.ToLower(v)); g.Valid() {
				d.Grade = g
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: grade %q is not one of sealed, mint, very_good, good, acceptable, damaged", row, v))
			}
		}

		if v := get("contents"); v != "" {
			var parts []game.Content
			for _, f := range strings.Fields(strings.ToLower(v)) {
				parts = append(parts, game.Content(f))
			}

			if c, err := game.ContentsOf(parts...); err == nil {
				d.Contents = c
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: %v", row, err))
			}
		}

		currency := strings.ToUpper(get("currency"))
		if currency != "" && !game.IsCurrencyCode(currency) {
			warnings = append(warnings, fmt.Sprintf("row %d: currency %q is not a three-letter code", row, currency))
			currency = ""
		}

		if v := get("price"); v != "" {
			if amount, ok := parsePrice(v, currency); ok {
				d.Price = game.Money{
					Amount:   amount,
					Currency: currency,
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("row %d: price %q is not an amount like 29.95", row, v))
			}
		}

		ext := get("externalId")
		if ext == "" {
			ext = fmt.Sprintf("csv:%s|%s|%s|%s", d.Kind, strings.ToLower(d.Platform), game.MatchKey(title), d.Key)
		}

		out = append(out, game.ImportedCopy{
			ExternalID: ext,
			Title:      title,
			Links:      links,
			Details:    d,
		})
	}

	return out, warnings, nil
}

// parseLinks reads "steam:620 gog:1207658924" and returns the links and the pairs it could not read.
func parseLinks(s string) (game.Links, []string) {
	var (
		links game.Links
		bad   []string
	)

	for _, pair := range strings.Fields(s) {
		store, id, ok := strings.Cut(pair, ":")
		if !ok || store == "" || id == "" {
			bad = append(bad, pair)
			continue
		}

		if links == nil {
			links = game.Links{}
		}

		links[strings.ToLower(store)] = id
	}

	return links, bad
}

// formatLinks writes links as parseLinks reads them, sorted by store.
func formatLinks(l game.Links) string {
	pairs := make([]string, 0, len(l))
	for _, k := range l.Keys() {
		pairs = append(pairs, k+":"+l[k])
	}

	return strings.Join(pairs, " ")
}

func parseDate(s string) (game.Date, bool) {
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return game.DateOf(t), true
		}
	}

	return "", false
}

// Encode writes the games as CSV, one row per copy.
func (Codec) Encode(w io.Writer, games []*game.Game) error {
	if _, err := io.WriteString(w, "\ufeff"); err != nil { // BOM so spreadsheet apps detect UTF-8
		return err
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return err
	}

	for _, g := range games {
		links := formatLinks(g.Links())

		for _, c := range g.Copies() {
			ext := c.ExternalID
			if ext == "" {
				ext = "copy:" + string(c.ID)
			}

			if err := cw.Write([]string{
				g.Title(), c.Platform, string(c.Kind), string(c.Status), c.Key, string(c.RedeemBy), c.Origin,
				string(c.AcquiredOn), c.Edition, string(c.Grade), contentsText(c.Contents), c.Location,
				formatPrice(c.Price), c.Price.Currency, c.Notes, links, ext, string(c.Barcode), c.System(),
			}); err != nil {
				return err
			}
		}
	}

	cw.Flush()

	return cw.Error()
}

// contentsText writes contents as the column reads them: "box manual media".
func contentsText(c game.Contents) string {
	list := c.List()
	parts := make([]string, 0, len(list))

	for _, x := range list {
		parts = append(parts, string(x))
	}

	return strings.Join(parts, " ")
}

// parsePrice reads "29.95" or "29,95" (at most the currency's decimals) into minor units. An
// unknown currency (empty) uses two decimals until the default one is known.
func parsePrice(text, currency string) (int64, bool) {
	digits := 2
	if currency != "" {
		digits = game.CurrencyDigits(currency)
	}

	whole, frac, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), ".")
	if whole == "" || len(frac) > digits || strings.Contains(frac, ".") {
		return 0, false
	}

	n, err := strconv.ParseInt(whole+frac+strings.Repeat("0", digits-len(frac)), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}

	return n, true
}

// formatPrice writes an amount with its currency's decimals and a dot: 2995 EUR is "29.95".
func formatPrice(m game.Money) string {
	if m.IsZero() {
		return ""
	}

	digits := game.CurrencyDigits(m.Currency)
	s := strconv.FormatInt(m.Amount, 10)

	if digits == 0 {
		return s
	}

	s = strings.Repeat("0", max(0, digits+1-len(s))) + s

	return s[:len(s)-digits] + "." + s[len(s)-digits:]
}
