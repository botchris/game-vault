// Package csvfile implements transfer.Codec for CSV files: one row per copy.
//
// Columns (header names are case-insensitive; Spanish aliases are accepted, unknown columns are
// reported and ignored):
//
//	title, platform, kind, status, key, redeemBy, origin, acquiredOn, edition, condition,
//	location, notes, links, externalId, barcode
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
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

var columns = []string{
	"title", "platform", "kind", "status", "key", "redeemBy", "origin", "acquiredOn",
	"edition", "condition", "location", "notes", "links", "externalId", "barcode",
}

var aliases = map[string]string{
	"titulo": "title", "juego": "title", "nombre": "title", "game": "title",
	"plataforma": "platform", "store": "platform",
	"tipo": "kind", "type": "kind",
	"estado":      "status",
	"clave":       "key",
	"fechalimite": "redeemBy", "caducidad": "redeemBy",
	"origen": "origin", "tienda": "origin", "bundle": "origin",
	"fechacompra": "acquiredOn",
	"edicion":     "edition",
	"condicion":   "condition", "estadofisico": "condition",
	"ubicacion": "location",
	"notas":     "notes",
	"enlaces":   "links", "vinculos": "links",
	"ean": "barcode", "upc": "barcode", "codigobarras": "barcode", "codigo": "barcode",
}

var kindAliases = map[string]game.Kind{
	"key": game.KindKey, "clave": game.KindKey, "cdkey": game.KindKey,
	"library": game.KindLibrary, "biblioteca": game.KindLibrary,
	"physical": game.KindPhysical, "fisico": game.KindPhysical, "físico": game.KindPhysical, "disco": game.KindPhysical,
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
				string(c.AcquiredOn), c.Edition, "", c.Location, c.Notes, links, ext, string(c.Barcode),
			}); err != nil {
				return err
			}
		}
	}

	cw.Flush()

	return cw.Error()
}
