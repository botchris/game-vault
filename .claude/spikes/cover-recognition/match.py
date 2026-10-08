# Matching v3: the catalog title must be found inside the OCR text (never the other way round),
# a match on big text counts more than on fine print, and among equal scores the longest title
# wins. Candidates close to the best form the ambiguous group.
import json, os, re, sys, unicodedata
from rapidfuzz import fuzz

ocr = json.load(open(sys.argv[1]))
catalog = json.load(open(sys.argv[2]))
# Expected answers: {"IMG_4500.jpg": "Call of Duty: Modern Warfare 2", ...} (see README).
truth = json.load(open(os.environ.get("LABELS", "/photos/labels.json")))

def norm(s):
    s = unicodedata.normalize("NFKD", s).encode("ascii", "ignore").decode().lower()
    return re.sub(r"[^a-z0-9]", "", s)

NOISE = re.compile(r"^(\d{1,2}|wwwpegiinfo|pal|b?ps3|xbox360|playstationnetwork|r)$")
PLATFORM = {"PS3": re.compile(r"ps3|playstation3"), "X360": re.compile(r"xbox360")}
out = {}
for img, data in ocr.items():
    joined = norm(" ".join(l["text"] for l in data["lines"]))
    platform = next((p for p, rx in PLATFORM.items() if rx.search(joined)), None)
    lines = sorted((l for l in data["lines"] if not NOISE.match(norm(l["text"]))), key=lambda l: (l["y"], l["x"]))
    hmax = max(l["h"] for l in lines)
    # Try the biggest text first: a title found there is worth more than one found in fine print.
    best_by_title = {}
    for th, weight in ((0.6, 1.0), (0.35, 0.9), (0.0, 0.75)):
        text = "".join(norm(l["text"]) for l in lines if l["h"] >= th * hmax)
        for title in catalog[platform]:
            t = norm(title)
            if len(t) < 4 or len(t) > len(text):
                continue
            s = fuzz.partial_ratio(t, text) / 100 * weight
            if s >= 0.7 and s > best_by_title.get(title, (0,))[0]:
                best_by_title[title] = (round(s, 2), len(t), title, th)
    scored = list(best_by_title.values())
    scored.sort(reverse=True)
    best = scored[0][0] if scored else 0
    group = [s[2] for s in scored if s[0] >= best - 0.08][:5]
    out[img] = {"platform": platform, "group": group}
    top = scored[0][2] if scored else None
    print(f"\n== {img} [{platform}]  top-1: {top!r} {'OK' if top == truth.get(img) else 'MISS'};  truth in group: {truth.get(img) in group}  group={group}")
    for s in scored[:5]:
        print(f"   {s[0]:.2f}  (size threshold {s[3]})  {s[2]}")
json.dump(out, open(sys.argv[3], "w"), indent=1)
