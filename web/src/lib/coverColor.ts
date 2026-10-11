import { ColorCache, dominantColor } from './dominantColor';

// The colours are remembered in this browser so a revisit paints every band at once. Covers'
// addresses carry the game's version, so an edited game gets a new entry and old ones age out.
const STORAGE_KEY = 'gamevault.coverColors';
const MAX_COLORS = 1500;
const SAMPLE = 16;

// Saved a moment after the last new colour, so a page of covers loading costs one write.
const SAVE_DELAY_MS = 1000;

let cache: ColorCache | null = null;
let saveTimer: ReturnType<typeof setTimeout> | undefined;

function colors(): ColorCache {
  if (!cache) {
    let stored: string | null = null;
    try { stored = localStorage.getItem(STORAGE_KEY); } catch { /* storage blocked: memory only */ }
    cache = ColorCache.parse(MAX_COLORS, stored);
  }
  return cache;
}

/** The backdrop colour already known for an image address, if any. */
export function knownCoverColor(url: string): string | undefined {
  return colors().get(url);
}

/**
 * The backdrop colour of a loaded image (see dominantColor), remembered by its address. Null when it
 * cannot be read: no canvas, a cross-origin image (getImageData throws) or a fully transparent one.
 */
export function coverColor(url: string, img: HTMLImageElement): string | null {
  const known = colors().get(url);
  if (known) return known;
  let color: string | null = null;
  try {
    const canvas = document.createElement('canvas');
    canvas.width = SAMPLE;
    canvas.height = SAMPLE;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) return null;
    ctx.drawImage(img, 0, 0, SAMPLE, SAMPLE);
    color = dominantColor(ctx.getImageData(0, 0, SAMPLE, SAMPLE).data);
  } catch {
    return null;
  }
  if (!color) return null;
  colors().set(url, color);
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(colors())); } catch { /* full or blocked */ }
  }, SAVE_DELAY_MS);
  return color;
}
