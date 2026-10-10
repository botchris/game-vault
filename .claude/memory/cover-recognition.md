---
name: cover-recognition
description: Local (no paid LLM) recognition of physical game boxes from a photo — spike results 2026-10-08 and the pipeline they support
metadata:
  type: project
---
The goal: identify physical games from a photo of the box, as an alternative to barcodes
(poor coverage), **without paid LLM services**; a small local model is fine. Training a model per
game is not viable (tens of thousands of classes); the spike tested OCR + catalog matching + a
visual tie-break instead.

Spike 2026-10-08, 4 phone photos of PAL boxes (frontal, good light): two PS3 and two Xbox 360
games, two of them sequels whose number is drawn into the logo.
- **OCR** PP-OCRv4 mobile ONNX (RapidOCR): ~0.35 s/photo on a desktop CPU; reads titles, edition
  and the platform banner ("PS3", "XBOX360") on all 4. **Never reads logo digits** drawn as art
  (a sequel's number in the title art), even at 2560 px or with lower thresholds.
- **Text matching** against per-platform title lists (Wikipedia "List of PlayStation 3 / Xbox
  360 games" via the MediaWiki API, ~1,800 titles each): title must be found inside the OCR text
  (not the reverse), biggest text first, longest title wins ties. 2/4 right at top-1; 4/4 inside
  a group of 1-3 sequels of the same series.
- **Visual tie-break** CLIP ViT-B/32 (laion2b) photo vs each candidate's Wikipedia cover (NA art,
  `pilicense=any` needed for non-free images): **4/4**, margins 0.05-0.17 cosine.
- Not yet tested: in-browser runtime and speed on a phone, other platforms (PS2, Wii, Switch,
  DS), worn/reflective boxes, non-Latin titles, a 30-50 photo set.

**Status: paused on 2026-10-08** until a 30-50 photo test set exists (it takes a while to
collect). Scripts, method and next steps: `.claude/spikes/cover-recognition/` (`run.sh
PHOTO_DIR` with a `labels.json`; photos never go into the repository). Resume from its README's
"Before building it" list.

**How to apply:** pipeline = OCR in the browser (ONNX) → platform from banner text or the batch
preset → fuzzy match in the Go server → visual rerank of the near-ties → user confirms. Keep the
server pure Go (no CGO / ONNX runtime). Related: [[provider-chains]], [[local-images]].
