# Downloads the Wikipedia "List of <platform> games" pages through the MediaWiki API and keeps the
# game titles (the italic link in each table row) as a JSON catalog per platform.
import json, re, sys, urllib.parse, urllib.request

UA = {"User-Agent": "GameVaultSpike/0.1 (cover OCR feasibility test; https://github.com/botchris/game-vault)"}

def api(**params):
    params.update(format="json", formatversion=2)
    url = "https://en.wikipedia.org/w/api.php?" + urllib.parse.urlencode(params)
    return json.load(urllib.request.urlopen(urllib.request.Request(url, headers=UA)))

def pages(prefix):
    r = api(action="query", list="allpages", apprefix=prefix, aplimit=50)
    return [p["title"] for p in r["query"]["allpages"]]

row_title = re.compile(r"^\|\s*''\[\[([^\]|]+)(?:\|([^\]]+))?\]\]''", re.M)
catalog = {}
for platform, prefix in {"PS3": "List of PlayStation 3 games", "X360": "List of Xbox 360 games"}.items():
    titles = set()
    for page in pages(prefix):
        wt = api(action="parse", page=page, prop="wikitext")["parse"]["wikitext"]
        for m in row_title.finditer(wt):
            titles.add((m.group(2) or m.group(1)).strip())
        print(platform, page, len(titles), file=sys.stderr)
    catalog[platform] = sorted(titles)
json.dump(catalog, open(sys.argv[1], "w"), indent=0)
print({k: len(v) for k, v in catalog.items()})
