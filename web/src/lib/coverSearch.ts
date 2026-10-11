// Links to search for a cover by hand, and reading the image address the user brings back by
// dragging or pasting. The server never queries Google Images or SteamGridDB (their terms forbid
// automated access): these are plain links the user opens in their own browser.

// The store names a key can be for, as game.SearchTitle has them (internal/domain/game/values.go).
// TestSearchTitle_webAgrees checks this port against the Go function on shared cases.
const STORE_NAMES = 'steam|gog(\\.com)?|epic(\\s*games)?(\\s*store)?|ubisoft(\\s*connect)?|uplay|ea(\\s*app)?|origin|' +
  'battle\\.?\\s*net|rockstar(\\s*games)?|microsoft(\\s*store)?|xbox(\\s*(one|series\\s*[xs](\\s*\\|\\s*[xs])?|live))?|' +
  'windows|playstation(\\s*[345])?|psn|ps[345]|nintendo(\\s*(switch|eshop))?|switch|itch(\\.io)?|amazon(\\s*games)?';

const RE_TRADEMARKS = /[™®©]/g;
const RE_BRACKETS = /\(.*?\)|\[.*?\]/g;
const RE_STORE_KEY = new RegExp(`\\b(${STORE_NAMES})\\s*(cd\\s*)?key\\b`, 'gi');
const RE_SUFFIXES = /\b(key|pc|digital|game of the year|goty|definitive|complete|deluxe|ultimate|enhanced|remastered|director'?s cut|anniversary|gold|edition)\b/gi;
const RE_DANGLING = /^[\s:;,\-–—|/]+|[\s:;,\-–—|/]+$/g;
const RE_SEPARATOR_RUN = /\s*[:;,\-–—|/](\s*[:;,\-–—|/])+\s*/g;

/** Collapses runs of whitespace and trims, like strings.Join(strings.Fields(s), " ") in Go. */
const fields = (s: string) => s.split(/\s+/).filter(Boolean).join(' ');

/**
 * The title without the edition and store tags game databases do not name ("Example West GOTY" →
 * "Example West", "Example Game (PC Key)" → "Example Game"); a port of game.SearchTitle. A title made
 * only of tags is returned as it is, without trademark signs and repeated spaces.
 */
export function searchTitle(title: string): string {
  let s = title.replace(RE_TRADEMARKS, '');
  s = s.replace(RE_BRACKETS, ' ');
  s = s.replace(RE_STORE_KEY, ' ');
  s = s.replace(RE_SUFFIXES, ' ');
  s = fields(s);
  s = s.replace(RE_SEPARATOR_RUN, ': ');
  s = s.replace(RE_DANGLING, '');
  return s || fields(title.replace(RE_TRADEMARKS, ''));
}

/**
 * A Google Images search for the box art of the game on that system ('' for none), limited to large,
 * tall images: `"Example Quest" PS4 box art`.
 */
export function googleImagesUrl(title: string, system: string): string {
  const q = [`"${searchTitle(title)}"`, system.trim(), 'box art'].filter(Boolean).join(' ');
  return `https://www.google.com/search?tbm=isch&q=${encodeURIComponent(q)}&tbs=isz:l,iar:t`;
}

/** A SteamGridDB search for the game's grids (its covers). */
export function steamGridDbUrl(title: string): string {
  return `https://www.steamgriddb.com/search/grids?term=${encodeURIComponent(searchTitle(title))}`;
}

/** Whether s is an absolute http(s) address with a host. */
export function isHttpUrl(s: string): boolean {
  try {
    const u = new URL(s.trim());
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '';
  } catch {
    return false;
  }
}

/**
 * Whether the address is only a small preview of an image: a Google Images thumbnail
 * (encrypted-tbn0.gstatic.com…) or an image inlined as a data: URI, which is what dragging a result
 * from Google's grid usually gives.
 */
export function isSmallPreview(s: string): boolean {
  const v = s.trim();
  if (/^data:/i.test(v)) return true;
  try {
    return /^encrypted-tbn\d*\.gstatic\.com$/i.test(new URL(v).hostname);
  } catch {
    return false;
  }
}

const decodeEntities = (s: string) => s
  .replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');

/** The src of the first <img> in an HTML fragment, or ''. */
export function firstImageSrc(html: string): string {
  const m = /<img\b[^>]*?\ssrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/i.exec(html);
  return m ? decodeEntities((m[1] ?? m[2] ?? m[3] ?? '').trim()) : '';
}

/** What a drop or a paste brought: an http(s) image address, and whether it was only a small preview. */
export interface DroppedImage {
  /** The http(s) address found, or '' when there was none. */
  url: string;
  /** The address (or the only thing found, a data: URI) is a small preview, not the full image. */
  small: boolean;
}

/**
 * The image address in dragged or pasted data, read through get (DataTransfer.getData): the first
 * http(s) address of text/uri-list (comment lines skipped), then text/plain, then the src of the first
 * <img> of text/html. Anything else (a data: URI, plain words) is not an address to download; a data:
 * URI is reported as a small preview so the user knows to open the full image.
 */
export function imageFromTransfer(get: (type: string) => string): DroppedImage {
  const candidates = [
    ...(get('text/uri-list') || '').split(/\r?\n/).map((l) => l.trim()).filter((l) => l && !l.startsWith('#')),
    (get('text/plain') || '').trim(),
    firstImageSrc(get('text/html') || ''),
  ].filter(Boolean);
  const url = candidates.find(isHttpUrl);
  if (url) return { url: url.trim(), small: isSmallPreview(url) };
  return { url: '', small: candidates.some(isSmallPreview) };
}
