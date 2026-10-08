# Runs PP-OCR (RapidOCR, ONNX) over every image and prints each line with its height, biggest first.
import glob, json, os, sys, time
from rapidocr_onnxruntime import RapidOCR

ocr = RapidOCR()
out = {}
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*.jpg"))):
    t = time.time()
    res, _ = ocr(path)
    lines = []
    for box, text, score in res or []:
        ys = [p[1] for p in box]; xs = [p[0] for p in box]
        lines.append({"text": text, "score": round(float(score), 2), "h": round(max(ys) - min(ys)), "y": round(min(ys)), "x": round(min(xs))})
    out[os.path.basename(path)] = {"secs": round(time.time() - t, 2), "lines": lines}
    print(f"\n== {os.path.basename(path)} ({out[os.path.basename(path)]['secs']}s)")
    for l in sorted(lines, key=lambda l: -l["h"]):
        print(f"  h={l['h']:4} y={l['y']:4} {l['score']:.2f}  {l['text']}")
json.dump(out, open(sys.argv[2], "w"), indent=1)
