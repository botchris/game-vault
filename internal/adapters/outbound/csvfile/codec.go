// Package csvfile implements transfer.Codec for CSV files: one row per copy.
//
// The columns and their order are the columns table below; an export writes them in that order
// with English names. An import reads them by name, in any order (header names are
// case-insensitive; unknown columns are reported and ignored).
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
// ("steam:620 gog:1207658924"); every row of a game carries the game's links. autoLinks lists, the
// same way, the links Game Vault found automatically: they come back automatic (labeled, never
// used to recognize a game in imports), not as ordinary links.
//
// Rows without externalId get a stable one derived from kind, platform, title (game.TitleIDKey,
// frozen: never MatchKey, which changes as matching improves) and key, so the same file can be
// imported again to update rows instead of duplicating them.
package csvfile

import (
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/money"
)

// column is one column of the file: its header and what an export writes in it. Imports read
// columns by name (see rowReader), so only exports depend on the order.
type column struct {
	name   string
	export func(r exportRow) string
}

// exportRow is what a column's export reads: a copy, its game, and the game's links already
// written as text (they are the same on every row of the game).
type exportRow struct {
	game      *game.Game
	copy      game.Copy
	links     string
	autoLinks string
}

// columns are the file's columns in the order exports write them. A new column goes here, and the
// golden files in testdata show the change.
var columns = []column{
	{"title", func(r exportRow) string { return r.game.Title() }},
	{"platform", func(r exportRow) string { return r.copy.Platform }},
	{"kind", func(r exportRow) string { return string(r.copy.Kind) }},
	{"status", func(r exportRow) string { return string(r.copy.Status) }},
	{"key", func(r exportRow) string { return r.copy.Key }},
	{"redeemBy", func(r exportRow) string { return string(r.copy.RedeemBy) }},
	{"origin", func(r exportRow) string { return r.copy.Origin }},
	{"acquiredOn", func(r exportRow) string { return string(r.copy.AcquiredOn) }},
	{"edition", func(r exportRow) string { return r.copy.Edition }},
	{"grade", func(r exportRow) string { return string(r.copy.Grade) }},
	{"contents", func(r exportRow) string { return contentsText(r.copy.Contents) }},
	{"location", func(r exportRow) string { return r.copy.Location }},
	{"price", func(r exportRow) string { return formatPrice(r.copy.Price) }},
	{"currency", func(r exportRow) string { return r.copy.Price.Currency }},
	{"notes", func(r exportRow) string { return r.copy.Notes }},
	{"links", func(r exportRow) string { return r.links }},
	{"externalId", func(r exportRow) string { return exportID(r.copy) }},
	{"barcode", func(r exportRow) string { return string(r.copy.Barcode) }},
	{"system", func(r exportRow) string { return r.copy.System() }},
	{"autoLinks", func(r exportRow) string { return r.autoLinks }},
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
		if strings.ToLower(c.name) == k {
			return c.name
		}
	}

	return aliases[k]
}

// Decode reads a CSV export (or a compatible spreadsheet) into imported copies and warnings.
func (Codec) Decode(r io.Reader) ([]game.ImportedCopy, []string, error) {
	records, err := readRecords(r)
	if err != nil {
		return nil, nil, err
	}

	idx, warnings, err := readHeader(records[0])
	if err != nil {
		return nil, nil, err
	}

	var out []game.ImportedCopy

	for n, rec := range records[1:] {
		row := rowReader{
			row: n + 2,
			rec: rec,
			idx: idx,
		}

		c, ok := row.copy()
		warnings = append(warnings, row.warnings...)

		if ok {
			out = append(out, c)
		}
	}

	return out, warnings, nil
}

// readRecords reads every record of the file: the header, then at least one data row.
func readRecords(r io.Reader) ([][]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("the CSV has no data rows")
	}

	return records, nil
}

// readHeader maps each known column to its position in the file and reports the unknown ones.
func readHeader(header []string) (map[string]int, []string, error) {
	var (
		idx      = map[string]int{}
		warnings []string
	)

	for i, h := range header {
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

	return idx, warnings, nil
}

// rowReader reads one data row into a copy. A value it cannot read is left out and reported as a
// warning naming the row, so one bad cell never stops an import.
type rowReader struct {
	// row is the row's line number for warnings (the header is row 1).
	row int

	rec []string
	idx map[string]int

	// warnings are the problems found so far, in column order.
	warnings []string
}

// get returns a column's trimmed value, empty when the file has no such column or the row is short.
func (r *rowReader) get(col string) string {
	if i, ok := r.idx[col]; ok && i < len(r.rec) {
		return strings.TrimSpace(r.rec[i])
	}

	return ""
}

func (r *rowReader) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf("row %d: ", r.row)+fmt.Sprintf(format, args...))
}

// copy reads the row; ok is false for a row without a title, which is skipped.
func (r *rowReader) copy() (_ game.ImportedCopy, ok bool) {
	title := r.get("title")
	if title == "" {
		return game.ImportedCopy{}, false
	}

	d := game.CopyDetails{
		Platform: r.get("platform"),
		Key:      r.get("key"),
		Origin:   r.get("origin"),
		Edition:  r.get("edition"),
		Location: r.get("location"),
		Notes:    r.get("notes"),
	}
	d.System = r.system(d.Platform)
	d.Kind = r.kind(d.Key)
	d.Status = r.status(d.Kind)
	d.RedeemBy = r.date("redeemBy")
	d.AcquiredOn = r.date("acquiredOn")
	d.Barcode = r.barcode()
	links := r.links("links")
	autoLinks := r.links("autoLinks")
	d.Grade = r.grade()
	d.Contents = r.contents()
	d.Price = r.price()

	return game.ImportedCopy{
		ExternalID: r.importID(title, d),
		Title:      title,
		Links:      links,
		AutoLinks:  autoLinks,
		Details:    d,
	}, true
}

// system keeps the system only when it differs from the one the platform implies: it is then the
// copy's own system. The domain checks it and names it like SystemOf when the copy is saved.
func (r *rowReader) system(platform string) string {
	if s := r.get("system"); s != "" && game.SystemOf(s) != game.SystemOf(platform) {
		return s
	}

	return ""
}

// kind reads the kind or its alias; without one, a row with a key is a key, any other a disc.
func (r *rowReader) kind(key string) game.Kind {
	if k, ok := kindAliases[strings.ToLower(r.get("kind"))]; ok {
		return k
	}

	if key != "" {
		return game.KindKey
	}

	return game.KindPhysical
}

// status reads the status; one the kind does not allow is left empty for the kind's default.
func (r *rowReader) status(kind game.Kind) game.Status {
	s := game.Status(strings.ToLower(r.get("status")))
	if kind.Allows(s) {
		return s
	}

	if s != "" {
		r.warn("status %q not valid for %s, using default", s, kind)
	}

	return ""
}

func (r *rowReader) date(col string) game.Date {
	v := r.get(col)
	if v == "" {
		return ""
	}

	d, ok := parseDate(v)
	if !ok {
		r.warn("unrecognized date %q (use YYYY-MM-DD)", v)
	}

	return d
}

func (r *rowReader) barcode() game.Barcode {
	v := r.get("barcode")
	if v == "" {
		return ""
	}

	b, err := game.ParseBarcode(v)
	if err != nil {
		r.warn("%v", err)

		return ""
	}

	return b
}

func (r *rowReader) links(col string) game.Links {
	links, bad := parseLinks(r.get(col))
	for _, b := range bad {
		r.warn("link %q is not store:id (e.g. steam:620), ignored", b)
	}

	return links
}

func (r *rowReader) grade() game.Grade {
	v := r.get("grade")
	if v == "" {
		return ""
	}

	g := game.Grade(strings.ToLower(v))
	if !g.Valid() {
		r.warn("grade %q is not one of sealed, mint, very_good, good, acceptable, damaged", v)

		return ""
	}

	return g
}

func (r *rowReader) contents() game.Contents {
	v := r.get("contents")
	if v == "" {
		return 0
	}

	var parts []game.Content
	for _, f := range strings.Fields(strings.ToLower(v)) {
		parts = append(parts, game.Content(f))
	}

	c, err := game.ContentsOf(parts...)
	if err != nil {
		r.warn("%v", err)

		return 0
	}

	return c
}

// price reads the price in the row's currency. A price without currency is left without one: the
// importer applies the default currency.
func (r *rowReader) price() game.Money {
	currency := strings.ToUpper(r.get("currency"))
	if currency != "" && !money.IsCurrencyCode(currency) {
		r.warn("currency %q is not a three-letter code", currency)

		currency = ""
	}

	v := r.get("price")
	if v == "" {
		return game.Money{}
	}

	amount, ok := parsePrice(v, currency)
	if !ok {
		r.warn("price %q is not an amount like 29.95", v)

		return game.Money{}
	}

	return game.Money{
		Amount:   amount,
		Currency: currency,
	}
}

// importID is the row's externalId, or a stable one derived from kind, platform, title and key.
func (r *rowReader) importID(title string, d game.CopyDetails) string {
	if ext := r.get("externalId"); ext != "" {
		return ext
	}

	return fmt.Sprintf("csv:%s|%s|%s|%s", d.Kind, strings.ToLower(d.Platform), game.TitleIDKey(title), d.Key)
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

	header := make([]string, 0, len(columns))
	for _, c := range columns {
		header = append(header, c.name)
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}

	for _, g := range games {
		links, autoLinks := exportLinks(g)

		for _, c := range g.Copies() {
			row := exportRow{
				game:      g,
				copy:      c,
				links:     links,
				autoLinks: autoLinks,
			}

			record := make([]string, 0, len(columns))
			for _, col := range columns {
				record = append(record, col.export(row))
			}

			if err := cw.Write(record); err != nil {
				return err
			}
		}
	}

	cw.Flush()

	return cw.Error()
}

// exportLinks writes the game's links and, apart, the ones found automatically, so an import keeps
// them automatic.
func exportLinks(g *game.Game) (links, autoLinks string) {
	own, auto := game.Links{}, game.Links{}

	for k, v := range g.Links() {
		if slices.Contains(g.AutoLinks(), k) {
			auto[k] = v
		} else {
			own[k] = v
		}
	}

	return formatLinks(own), formatLinks(auto)
}

// exportID is the copy's externalId; a copy entered by hand has none, so its own id stands in.
func exportID(c game.Copy) string {
	if c.ExternalID == "" {
		return "copy:" + string(c.ID)
	}

	return c.ExternalID
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
		digits = money.CurrencyDigits(currency)
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

	digits := money.CurrencyDigits(m.Currency)
	s := strconv.FormatInt(m.Amount, 10)

	if digits == 0 {
		return s
	}

	s = strings.Repeat("0", max(0, digits+1-len(s))) + s

	return s[:len(s)-digits] + "." + s[len(s)-digits:]
}
