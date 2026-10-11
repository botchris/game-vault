/** A colour as 0–255 red, green and blue channels. */
export type Rgb = { r: number; g: number; b: number };

/**
 * The lowest alpha (0–255) a pixel needs to count in the average: the transparent corners of a
 * rounded box or the margin of a PNG would otherwise pull the colour towards black.
 */
export const MIN_ALPHA = 128;

/** The lightness range (0–1) of a backdrop: never washed out, never pitch black, badges stay legible. */
export const MIN_LIGHTNESS = 0.2;
export const MAX_LIGHTNESS = 0.42;

/** The highest saturation (0–1) of a backdrop, so a bright cover does not turn its band neon. */
export const MAX_SATURATION = 0.7;

/**
 * The mean colour of RGBA pixel data (4 bytes per pixel, as a canvas's getImageData returns it),
 * skipping pixels more transparent than MIN_ALPHA. Null when no pixel counts.
 */
export function averageColor(data: ArrayLike<number>): Rgb | null {
  let r = 0;
  let g = 0;
  let b = 0;
  let n = 0;
  for (let i = 0; i + 3 < data.length; i += 4) {
    if (data[i + 3]! < MIN_ALPHA) continue;
    r += data[i]!;
    g += data[i + 1]!;
    b += data[i + 2]!;
    n++;
  }
  if (n === 0) return null;
  return { r: Math.round(r / n), g: Math.round(g / n), b: Math.round(b / n) };
}

/**
 * The colour adjusted to sit behind a cover: its hue is kept, its lightness clamped to
 * [MIN_LIGHTNESS, MAX_LIGHTNESS] and its saturation capped at MAX_SATURATION.
 */
export function backdropColor(c: Rgb): Rgb {
  const { h, s, l } = rgbToHsl(c);
  return hslToRgb({
    h,
    s: Math.min(s, MAX_SATURATION),
    l: Math.min(Math.max(l, MIN_LIGHTNESS), MAX_LIGHTNESS),
  });
}

/** The backdrop colour of RGBA pixel data as a CSS colour, or null when no pixel is opaque enough. */
export function dominantColor(data: ArrayLike<number>): string | null {
  const avg = averageColor(data);
  return avg ? toCss(backdropColor(avg)) : null;
}

/** A colour as CSS `rgb(r g b)`. */
export function toCss({ r, g, b }: Rgb): string {
  return `rgb(${r} ${g} ${b})`;
}

/** Hue in degrees (0–360), saturation and lightness 0–1. */
export function rgbToHsl({ r, g, b }: Rgb): { h: number; s: number; l: number } {
  const rn = r / 255;
  const gn = g / 255;
  const bn = b / 255;
  const max = Math.max(rn, gn, bn);
  const min = Math.min(rn, gn, bn);
  const l = (max + min) / 2;
  const d = max - min;
  if (d === 0) return { h: 0, s: 0, l };
  const s = d / (1 - Math.abs(2 * l - 1));
  let h: number;
  if (max === rn) h = ((gn - bn) / d) % 6;
  else if (max === gn) h = (bn - rn) / d + 2;
  else h = (rn - gn) / d + 4;
  h *= 60;
  if (h < 0) h += 360;
  return { h, s, l };
}

/** The inverse of rgbToHsl, rounded to whole channels. */
export function hslToRgb({ h, s, l }: { h: number; s: number; l: number }): Rgb {
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  let rgb: [number, number, number];
  if (h < 60) rgb = [c, x, 0];
  else if (h < 120) rgb = [x, c, 0];
  else if (h < 180) rgb = [0, c, x];
  else if (h < 240) rgb = [0, x, c];
  else if (h < 300) rgb = [x, 0, c];
  else rgb = [c, 0, x];
  const [r, g, b] = rgb.map((v) => Math.round((v + m) * 255)) as [number, number, number];
  return { r, g, b };
}

/**
 * Backdrop colours by image address, at most `max` of them: adding one more forgets the one added
 * (or refreshed) longest ago. Serializable as [address, colour] pairs, oldest first, to persist it.
 */
export class ColorCache {
  private readonly entries = new Map<string, string>();
  private readonly max: number;

  constructor(max: number, initial: Iterable<[string, string]> = []) {
    this.max = max;
    for (const [url, color] of initial) {
      if (typeof url === 'string' && typeof color === 'string') this.set(url, color);
    }
  }

  /** The colour remembered for an address, if any. */
  get(url: string): string | undefined {
    return this.entries.get(url);
  }

  /** Remembers a colour, making it the newest entry and dropping the oldest beyond the cap. */
  set(url: string, color: string): void {
    this.entries.delete(url);
    this.entries.set(url, color);
    while (this.entries.size > this.max) {
      const oldest = this.entries.keys().next().value as string;
      this.entries.delete(oldest);
    }
  }

  /** How many colours are remembered. */
  get size(): number {
    return this.entries.size;
  }

  /** The entries, oldest first, for JSON. */
  toJSON(): [string, string][] {
    return [...this.entries];
  }

  /** A cache from what toJSON stored; anything unreadable gives an empty cache. */
  static parse(max: number, json: string | null): ColorCache {
    try {
      const data: unknown = json ? JSON.parse(json) : [];
      if (!Array.isArray(data)) return new ColorCache(max);
      return new ColorCache(max, data.filter((e): e is [string, string] => Array.isArray(e) && e.length === 2));
    } catch {
      return new ColorCache(max);
    }
  }
}
