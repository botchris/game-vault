package notify

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/money"
)

const (
	// maxLines is how many games or keys a message lists before counting the rest.
	maxLines = 20

	// maxImages is how many covers a message carries.
	maxImages = 2
)

// Render turns e into the message a connector sends, in lang (English when it has no texts).
// address is the Game Vault address links start with; without one, messages link only to public
// pages (a deal's, a release's). Covers come from the local game data through covers, never from
// the network; a game without one is told without it.
func Render(ctx context.Context, e event.Event, lang, address string, catalog Catalog, covers CoverFiles) Message {
	if _, ok := texts[lang]; !ok {
		lang = fallbackLanguage
	}

	var m Message

	switch data := e.Data.(type) {
	case event.CollectionAdded:
		m = renderAdded(ctx, data, lang, address, catalog, covers)
	case event.Release:
		m = renderRelease(e.Kind, data, lang, address)
	case event.HistoricalLow:
		m = renderDeal(data, lang)
	case event.Issue:
		m = renderIssue(e.Kind, data, lang, address)
	case event.KeysExpiring:
		m = renderKeys(data, lang, address)
	case event.FailedLogins:
		m = renderLogins(data, lang)
	default:
		m = Message{Title: string(e.Kind)}
	}

	m.Event = e

	return m
}

func renderAdded(ctx context.Context, a event.CollectionAdded, lang, address string, catalog Catalog, covers CoverFiles) Message {
	n := len(a.Games)
	lines := make([]string, 0, min(n, maxLines)+2)

	for _, g := range a.Games[:min(n, maxLines)] {
		line := g.Title
		if len(g.Systems) > 0 {
			line += " (" + strings.Join(g.Systems, ", ") + ")"
		}

		lines = append(lines, line)
	}

	if n > maxLines {
		lines = append(lines, text(lang, "more", n-maxLines))
	}

	if a.CopiesAdded > 0 {
		lines = append(lines, text(lang, "added.copies", a.CopiesAdded))
	}

	m := Message{
		Title:  addedTitle(a, lang),
		Text:   strings.Join(lines, "\n"),
		Images: localCovers(ctx, a.Games, catalog, covers),
	}

	switch {
	case address == "":
	case n == 1:
		m.Link, m.LinkLabel = address+"/#/game/"+string(a.Games[0].ID), text(lang, "open")
	default:
		m.Link, m.LinkLabel = address+"/#/library/recent", text(lang, "open")
	}

	return m
}

func addedTitle(a event.CollectionAdded, lang string) string {
	n := len(a.Games)

	switch a.Origin {
	case event.OriginManual:
		if n == 1 {
			return text(lang, "added.title.manual.one")
		}

		return text(lang, "added.title.manual", n)
	case event.OriginImport:
		return text(lang, "added.title.import", n)
	default:
		if n == 1 {
			return text(lang, "added.title.source.one", a.OriginName)
		}

		return text(lang, "added.title.source", a.OriginName, n)
	}
}

// localCovers returns the main edition's cover of the first games that have one on disk, up to
// maxImages. A game that cannot be read or has no cover is skipped: a message without a cover is
// still worth sending.
func localCovers(ctx context.Context, refs []event.GameRef, catalog Catalog, covers CoverFiles) []Image {
	var out []Image

	for _, ref := range refs {
		if len(out) == maxImages {
			break
		}

		g, err := catalog.Get(ctx, ref.ID)
		if err != nil {
			continue
		}

		img, ok, err := covers.GetCover(ref.ID, g.MainEdition().System)
		if err != nil || !ok || len(img.Data) == 0 {
			continue
		}

		out = append(out, Image{
			Name:        imageName(ref.ID, img.ContentType),
			Data:        img.Data,
			ContentType: img.ContentType,
		})
	}

	return out
}

// imageName names a cover file after its game, with the extension of its type.
func imageName(id game.ID, contentType string) string {
	ext := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".gif",
	}[contentType]

	return "cover-" + string(id) + ext
}

func renderRelease(kind event.Kind, r event.Release, lang, address string) Message {
	title := "release.announced.title"
	if kind == event.KindReleaseOut {
		title = "release.out.title"
	}

	when := string(r.Date)
	if when == "" {
		when = text(lang, "release.tba")
	}

	if len(r.Platforms) > 0 {
		when += " · " + strings.Join(r.Platforms, ", ")
	}

	m := Message{
		Title: text(lang, title, r.Title),
		Text:  when + "\n" + text(lang, "release.because", r.BecauseTitle),
	}

	if r.CoverURL != "" {
		m.Images = []Image{{
			Name: "cover",
			URL:  r.CoverURL,
		}}
	}

	switch {
	case address != "" && r.Because != "":
		m.Link, m.LinkLabel = address+"/#/game/"+string(r.Because), text(lang, "open")
	case r.URL != "":
		m.Link, m.LinkLabel = r.URL, text(lang, "open.page")
	}

	return m
}

func renderDeal(d event.HistoricalLow, lang string) Message {
	m := Message{
		Title: text(lang, "deal.title", d.Title),
		Text:  text(lang, "deal.text", formatMoney(d.Price, lang), d.Cut, formatMoney(d.Regular, lang), d.Shop),
	}

	if d.ImageURL != "" {
		m.Images = []Image{{
			Name: "art",
			URL:  d.ImageURL,
		}}
	}

	// The deal's own link, always: it is the service's link, which the terms ask to keep.
	if d.URL != "" {
		m.Link, m.LinkLabel = d.URL, text(lang, "open.page")
	}

	return m
}

func renderIssue(kind event.Kind, is event.Issue, lang, address string) Message {
	name := is.Source
	if _, named := texts[fallbackLanguage]["kind."+is.Kind]; name == "" && named {
		name = text(lang, "kind."+is.Kind)
	}

	name = cmp.Or(name, is.Kind)

	m := Message{
		Title:     text(lang, "issue.resolved.title", name),
		Important: true,
	}

	// A resolved problem is told by its title alone: repeating what was wrong would read as if it
	// still were.
	if kind == event.KindIssueOpened {
		m.Title = text(lang, "issue.opened.title", name+": "+issueReason(is, lang))

		// Without a text for the reason, the title already carries the English message.
		if _, known := texts[fallbackLanguage]["reason."+is.Reason]; known {
			m.Text = is.Message
		}
	}

	if address != "" {
		m.Link, m.LinkLabel = address+"/#/system/health", text(lang, "open")
	}

	return m
}

// issueReason is the issue's reason in lang, its params filled for a reader (a moment as a date);
// for a reason this version has no text for (one added to Health later), Health's English message.
func issueReason(is event.Issue, lang string) string {
	if _, known := texts[fallbackLanguage]["reason."+is.Reason]; !known {
		return is.Message
	}

	params := is.Params
	if until, ok := params["until"]; ok {
		params = maps.Clone(params)
		params["until"] = formatMoment(until, lang)
	}

	return fillParams(text(lang, "reason."+is.Reason), params)
}

// formatMoment writes an RFC 3339 moment as the server's local date for a reader of lang ("on
// November 1", "el 1 de noviembre"); a value that does not parse is shown as it came.
func formatMoment(raw, lang string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}

	t = t.Local()

	return text(lang, "date.on", text(lang, fmt.Sprintf("month.%d", t.Month())), t.Day())
}

func renderKeys(k event.KeysExpiring, lang, address string) Message {
	n := len(k.Keys)

	title := text(lang, "keys.title", n)
	if n == 1 {
		title = text(lang, "keys.title.one")
	}

	lines := make([]string, 0, min(n, maxLines)+1)
	for _, key := range k.Keys[:min(n, maxLines)] {
		lines = append(lines, text(lang, "keys.line", key.Game.Title, key.Store, daysText(key.DaysLeft, lang)))
	}

	if n > maxLines {
		lines = append(lines, text(lang, "more", n-maxLines))
	}

	m := Message{
		Title:     title,
		Text:      strings.Join(lines, "\n"),
		Important: true,
	}

	if address != "" {
		m.Link, m.LinkLabel = address+"/#/library/expiring", text(lang, "open")
	}

	return m
}

func daysText(days int, lang string) string {
	switch {
	case days <= 0:
		return text(lang, "days.today")
	case days == 1:
		return text(lang, "days.one")
	default:
		return text(lang, "days.other", days)
	}
}

func renderLogins(f event.FailedLogins, lang string) Message {
	const clock = "15:04"

	return Message{
		Title:     text(lang, "logins.title", f.Count, f.Address),
		Text:      text(lang, "logins.text", f.First.Local().Format(clock), f.Last.Local().Format(clock)),
		Important: true,
	}
}

// formatMoney writes m for a reader of lang: "€9.99" or "$9.99" in English ("9.99 GBP" for other
// currencies), "9,99 €" in Spanish. The amount is in the currency's minor unit.
func formatMoney(m event.Money, lang string) string {
	digits := money.CurrencyDigits(m.Currency)

	unit := int64(1)
	for range digits {
		unit *= 10
	}

	sep := "."
	if lang == "es" {
		sep = ","
	}

	number := fmt.Sprint(m.Amount / unit)
	if digits > 0 {
		number += sep + fmt.Sprintf("%0*d", digits, m.Amount%unit)
	}

	symbol := map[string]string{
		"EUR": "€",
		"USD": "$",
	}[strings.ToUpper(m.Currency)]

	switch {
	case m.Currency == "":
		return number
	case lang == "es" && symbol != "":
		return number + " " + symbol
	case symbol != "":
		return symbol + number
	default:
		return number + " " + strings.ToUpper(m.Currency)
	}
}
