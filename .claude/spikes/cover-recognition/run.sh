#!/bin/sh
# Re-runs the cover recognition spike on a folder of box photos. Everything runs in throwaway
# python containers; photos, catalogs and downloaded covers stay in WORK, outside the repository.
#
#   .claude/spikes/cover-recognition/run.sh ~/Downloads/covers-tests [WORK]
#
# The photo folder needs a labels.json: {"IMG_4500.jpg": "Call of Duty: Modern Warfare 2", …},
# keyed by the JPEG name (HEIC files are converted to JPEG with the same base name).
set -eu

photos=${1:?usage: run.sh PHOTO_DIR [WORK_DIR]}
work=${2:-${TMPDIR:-/tmp}/cover-spike}
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$work/covers" "$work/out"
cp "$photos/labels.json" "$work/covers/labels.json"

# 1280 px on the long side, like a phone upload. sips ships with macOS.
for f in "$photos"/*; do
  case "$f" in
  *.heic | *.HEIC | *.jpg | *.JPG | *.jpeg | *.png) sips -s format jpeg -Z 1280 "$f" --out "$work/covers/$(basename "${f%.*}").jpg" >/dev/null ;;
  esac
done

run() {
  docker run --rm -v "$work/covers:/photos:ro" -v "$work/out:/out" -v "$here:/spike:ro" \
    -v gamevault-spike-pip:/root/.cache/pip -v gamevault-spike-hf:/root/.cache/huggingface \
    python:3.12-slim sh -c "$1"
}

run 'apt-get update -qq >/dev/null && apt-get install -y -qq libgl1 libglib2.0-0 >/dev/null 2>&1
  pip install -q rapidocr-onnxruntime rapidfuzz 2>/dev/null
  python -I /spike/ocr.py /photos /out/ocr.json
  [ -f /out/titles.json ] || python -I /spike/titles.py /out/titles.json
  python -I /spike/match.py /out/ocr.json /out/titles.json /out/groups.json'

run 'pip install -q torch torchvision --index-url https://download.pytorch.org/whl/cpu 2>/dev/null
  pip install -q open_clip_torch 2>/dev/null
  python -I /spike/visual.py /out/groups.json /photos /out/wiki-covers' 2>&1 | grep -v -i -E 'warn|notice'
