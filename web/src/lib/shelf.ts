/** The aspect (width / height) of a cover cell: every card keeps it, so titles and rows line up. */
export const CELL_ASPECT = 2 / 3;

/**
 * The widest a box with badges over it may stand (width / height). Boxes are at most a little wider
 * than tall (a CD jewel case); a landscape banner is cropped at the sides to this, so the flags at
 * its top and the badges at its bottom still fit on a phone's narrow card.
 */
export const MAX_BOX_ASPECT = 1.25;

/**
 * The share of a cover cell (in percent) an image of that natural size takes when shown whole,
 * standing at the bottom of the cell like a box on a shelf: a wider box (a DS case) keeps the full
 * width and stands shorter, a narrower one (a Switch case) keeps the full height. An image wider than
 * maxAspect takes maxAspect's shape (and is cropped to it). An image without a size fills the cell.
 */
export function boxShape(width: number, height: number, maxAspect = Infinity): { width: number; height: number } {
  if (!(width > 0 && height > 0)) return { width: 100, height: 100 };
  const aspect = Math.min(width / height, maxAspect);
  if (aspect >= CELL_ASPECT) return { width: 100, height: (CELL_ASPECT / aspect) * 100 };
  return { width: (aspect / CELL_ASPECT) * 100, height: 100 };
}
