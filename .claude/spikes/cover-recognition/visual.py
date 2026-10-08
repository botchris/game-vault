# Visual tie-break: embeds each photo and the Wikipedia cover of each candidate in its ambiguous
# group with CLIP, and ranks the group by cosine similarity.
import json, os, sys, urllib.parse, urllib.request
import open_clip, torch
from PIL import Image

UA = {"User-Agent": "GameVaultSpike/0.1 (cover OCR feasibility test; https://github.com/botchris/game-vault)"}
groups = json.load(open(sys.argv[1]))
covers_dir, cache = sys.argv[2], sys.argv[3]
os.makedirs(cache, exist_ok=True)
# Expected answers: {"IMG_4500.jpg": "Call of Duty: Modern Warfare 2", ...} (see README).
truth = json.load(open(os.environ.get("LABELS", "/photos/labels.json")))
# Display titles in the catalog are not always article titles.
article = {"Dead Space": "Dead Space (2008 video game)", "Assassin's Creed": "Assassin's Creed (video game)"}

def cover(title):
    path = os.path.join(cache, title.replace("/", "_").replace(":", "") + ".img")
    if os.path.exists(path):
        return path
    q = urllib.parse.urlencode({"action": "query", "titles": article.get(title, title), "prop": "pageimages",
                                "piprop": "original", "pilicense": "any", "redirects": 1, "format": "json", "formatversion": 2})
    r = json.load(urllib.request.urlopen(urllib.request.Request("https://en.wikipedia.org/w/api.php?" + q, headers=UA)))
    src = r["query"]["pages"][0].get("original", {}).get("source")
    if not src:
        return None
    open(path, "wb").write(urllib.request.urlopen(urllib.request.Request(src, headers=UA)).read())
    return path

model, _, preprocess = open_clip.create_model_and_transforms("ViT-B-32", pretrained="laion2b_s34b_b79k")
model.eval()

def embed(path):
    with torch.no_grad():
        e = model.encode_image(preprocess(Image.open(path).convert("RGB")).unsqueeze(0))
    return e / e.norm(dim=-1, keepdim=True)

for img, g in groups.items():
    if len(g["group"]) < 2:
        print(f"\n== {img}: already decided by text ({g['group'][0]})")
        continue
    photo = embed(os.path.join(covers_dir, img))
    ranked = []
    for title in g["group"]:
        p = cover(title)
        if p:
            ranked.append((round(float(photo @ embed(p).T), 3), title))
    ranked.sort(reverse=True)
    print(f"\n== {img}: {'OK' if ranked and ranked[0][1] == truth.get(img) else 'MISS'}")
    for s, t in ranked:
        print(f"   {s:.3f}  {t}")
