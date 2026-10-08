# Cover recognition spike

Feasibility test for adding physical games from a **photo of the box**, as an alternative to
barcodes (many PAL codes are unknown to the free barcode databases). Constraint from the user: no
paid LLM services; small local models are fine. Not product code: nothing here is built or shipped.

## Pipeline tested

1. **OCR** (`ocr.py`): PP-OCRv4 mobile through RapidOCR (ONNX). Every text line with its height.
2. **Title catalog** (`titles.py`): per-platform game lists from Wikipedia ("List of PlayStation 3
   games…", "List of Xbox 360 games…") through the MediaWiki API, about 1,800 titles each.
3. **Matching** (`match.py`): platform from the banner text; each catalog title must be found
   *inside* the OCR text, trying the biggest text first (titles are the largest text on a box);
   the longest title wins ties. Titles close to the best form an ambiguous group.
4. **Visual tie-break** (`visual.py`): CLIP ViT-B/32 (laion2b) embeddings of the photo and of
   each group candidate's cover (Wikipedia page image; non-free images need `pilicense=any`),
   ranked by cosine similarity.

`run.sh PHOTO_DIR` runs it all in throwaway containers. The photo folder needs a `labels.json`
with the right title for each photo. Keep photos out of the repository (it is public).

## Results (2026-10-08, 4 photos)

PAL boxes, frontal, good light: CoD Modern Warfare 2 (PS3), Red Dead Redemption GOTY (X360),
Assassin's Creed III Edición Especial (PS3), Dead Space 3 (X360).

| | Text only | + visual tie-break |
|---|---|---|
| Right game first | 2/4 | **4/4** (margins 0.05-0.17) |
| Right game in the group | 4/4 (groups of 1-3 sequels) | |

- OCR: ~0.35 s per photo on a desktop CPU. Reads title, edition and platform banner on all 4.
- OCR **never reads digits drawn as part of a logo** (MW "2", Dead Space "3"), not at 2560 px nor
  with lower thresholds: text alone cannot tell sequels apart, hence the visual step.

## Before building it

1. **A real test set: 30-50 photos**, deliberately hard: PS2, Wii, Switch and DS boxes, worn or
   reflective cases, rare games, long series, non-Latin titles. Go ahead if it stays above
   ~85-90 % first-choice accuracy.
2. **Generalize** `match.py`: platform patterns beyond PS3/X360 (it only knows those two), and
   catalogs for every platform the Scan page offers.
3. **Measure on a phone**: OCR (~15 MB) and a quantized image encoder (MobileCLIP or CLIP int8,
   ~50-90 MB) with onnxruntime-web, downloaded once and cached.
4. **Decide the sources**: the title catalog (Wikipedia lists, CC BY-SA, refreshed by a task; or
   provider search) and the candidate covers for the tie-break (the existing cover providers, or
   Wikipedia).

## Intended design

OCR and the image encoder run **in the browser** (onnxruntime-web), so the NAS does no heavy work
and the server stays pure Go (`CGO_ENABLED=0`, no ONNX runtime). The server matches the text
against the catalog, the browser re-ranks the near-ties visually, and the user confirms. The
result goes through the same flow as a barcode match (suggestions, cover, physical copy).
