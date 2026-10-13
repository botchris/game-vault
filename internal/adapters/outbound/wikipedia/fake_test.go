package wikipedia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The fake reproduces the shapes the real APIs answered on 2026-10-11 (wbsearchentities,
// wbgetentities, action=query&prop=extracts), with invented items. Labels may be in "mul" (the
// language-neutral label Wikidata uses for names) rather than in a language.

// entity is an invented Wikidata item.
type entity struct {
	labels    map[string]string
	claims    map[string][]string // property → item ids ("P31" → ["Q7889"])
	dates     []string            // P577 values ("+2011-04-19T00:00:00Z", day precision)
	sitelinks map[string]string   // "enwiki" → "Example Quest"
}

// article is an invented Wikipedia article.
type article struct {
	extract        string
	disambiguation bool
}

type fakeWiki struct {
	srv *httptest.Server

	mu sync.Mutex

	// entities are the Wikidata items, by id.
	entities map[string]entity

	// searches maps "<language>:<text>" to the item ids wbsearchentities finds.
	searches map[string][]string

	// articles are the Wikipedia pages, by "<lang>:<title>".
	articles map[string]article

	// agents records the User-Agent of every request.
	agents []string

	// requests records every request's action.
	requests []string

	// labelLanguages records the languages parameter of every wbgetentities request.
	labelLanguages []string

	// searched records every search as "<language>:<text>".
	searched []string

	// fail answers every Wikidata request with this API error (HTTP 200), or HTTP 429 when "429".
	fail string
}

func newFakeWiki(t *testing.T) *fakeWiki {
	t.Helper()

	f := &fakeWiki{
		entities: map[string]entity{},
		searches: map[string][]string{},
		articles: map[string]article{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeWiki) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	q := r.URL.Query()
	f.agents = append(f.agents, r.UserAgent())
	f.requests = append(f.requests, q.Get("action"))

	// Real behavior: Wikimedia refuses requests without a User-Agent (and blocks generic library
	// agents) with an HTML page.
	if r.UserAgent() == "" || strings.HasPrefix(r.UserAgent(), "Go-http-client") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<html><body>Please set a user-agent and respect our robot policy https://w.wiki/4wJS.</body></html>"))

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if lang, ok := strings.CutPrefix(r.URL.Path, "/wiki-"); ok {
		f.serveArticle(w, strings.TrimSuffix(lang, "/w/api.php"), q.Get("titles"))
		return
	}

	switch f.fail {
	case "":
	case "502":
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))

		return
	case "hangup":
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}

		return
	case "429":
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":"too-many-requests","info":"Too many requests"}}`))

		return
	default:
		w.Write([]byte(`{"error":{"code":"` + f.fail + `","info":"The API answered ` + f.fail + `."}}`))
		return
	}

	switch q.Get("action") {
	case "wbsearchentities":
		f.searched = append(f.searched, q.Get("language")+":"+q.Get("search"))

		var hits []map[string]any

		for _, id := range f.searches[q.Get("language")+":"+q.Get("search")] {
			label := f.entities[id].labels["mul"]
			if label == "" {
				label = f.entities[id].labels["en"]
			}

			hits = append(hits, map[string]any{
				"id":    id,
				"label": label,
				"match": map[string]any{
					"type":     "label",
					"language": "mul",
					"text":     label,
				},
			})
		}

		json.NewEncoder(w).Encode(map[string]any{
			"searchinfo": map[string]any{"search": q.Get("search")},
			"search":     hits,
			"success":    1,
		})
	case "wbgetentities":
		f.labelLanguages = append(f.labelLanguages, q.Get("languages"))
		f.serveEntities(w, strings.Split(q.Get("ids"), "|"), strings.Split(q.Get("languages"), "|"))
	default:
		w.Write([]byte(`{"error":{"code":"badvalue","info":"Unrecognized value for parameter \"action\"."}}`))
	}
}

func (f *fakeWiki) serveEntities(w http.ResponseWriter, ids, languages []string) {
	out := map[string]any{}

	for _, id := range ids {
		e, ok := f.entities[id]
		if !ok {
			out[id] = map[string]any{
				"id":      id,
				"missing": "",
			}

			continue
		}

		labels := map[string]any{}
		for _, l := range languages {
			if v, ok := e.labels[l]; ok {
				labels[l] = map[string]string{
					"language": l,
					"value":    v,
				}
			}
		}

		claims := map[string][]any{}
		for prop, values := range e.claims {
			for _, v := range values {
				claims[prop] = append(claims[prop], map[string]any{
					"mainsnak": map[string]any{
						"snaktype": "value",
						"property": prop,
						"datavalue": map[string]any{
							"type": "wikibase-entityid",
							"value": map[string]any{
								"entity-type": "item",
								"id":          v,
							},
						},
					},
					"rank": "normal",
				})
			}
		}

		for _, d := range e.dates {
			precision := 11
			if p, rest, ok := strings.Cut(d, ":+"); ok {
				precision, _ = strconv.Atoi(p)
				d = "+" + rest
			}

			claims["P577"] = append(claims["P577"], map[string]any{
				"mainsnak": map[string]any{
					"snaktype": "value",
					"property": "P577",
					"datavalue": map[string]any{
						"type": "time",
						"value": map[string]any{
							"time":      d,
							"precision": precision,
						},
					},
				},
				"rank": "normal",
			})
		}

		sitelinks := map[string]any{}
		for site, title := range e.sitelinks {
			sitelinks[site] = map[string]any{
				"site":  site,
				"title": title,
			}
		}

		out[id] = map[string]any{
			"type":      "item",
			"id":        id,
			"labels":    labels,
			"claims":    claims,
			"sitelinks": sitelinks,
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"entities": out,
		"success":  1,
	})
}

func (f *fakeWiki) serveArticle(w http.ResponseWriter, lang, title string) {
	a, ok := f.articles[lang+":"+title]
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{
			"batchcomplete": true,
			"query": map[string]any{"pages": []any{map[string]any{
				"ns":      0,
				"title":   title,
				"missing": true,
			}}},
		})

		return
	}

	page := map[string]any{
		"pageid":  1550851,
		"ns":      0,
		"title":   title,
		"extract": a.extract,
		"fullurl": "https://" + lang + ".wikipedia.org/wiki/" + strings.ReplaceAll(title, " ", "_"),
	}
	if a.disambiguation {
		page["pageprops"] = map[string]any{"disambiguation": ""}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"batchcomplete": true,
		"query":         map[string]any{"pages": []any{page}},
	})
}

func (f *fakeWiki) userAgents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.agents...)
}

func (f *fakeWiki) actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.requests...)
}

func (f *fakeWiki) searchLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.searched...)
}

func (f *fakeWiki) languagesAsked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.labelLanguages...)
}

func (f *fakeWiki) setFail(code string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fail = code
}

// details returns the provider pointed at the fake, without spacing its requests.
func (f *fakeWiki) details() *Details {
	d := New("1.2.3")
	d.Wikidata.BaseURL = f.srv.URL
	d.Wikidata.HTTP = f.srv.Client()
	d.Wikipedia.BaseURL = f.srv.URL + "/wiki-{lang}"
	d.Wikipedia.HTTP = f.srv.Client()
	d.throttle = newThrottle(0)

	return d
}
