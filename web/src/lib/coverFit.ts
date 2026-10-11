/** The aspect (width / height) of a cover cell: every card keeps it, so titles and rows line up. */
export const CELL_ASPECT = 2 / 3;

/**
 * How far (as a ratio) an image's aspect may be from the cell's and still count as filling it: the
 * gaps around it are then a pixel or two, too thin to need a backdrop.
 */
export const FILL_TOLERANCE = 0.02;

/**
 * Whether an image of that natural size, shown whole (contained) in a cover cell, fills it. Most PC
 * covers do; a DS case (wider) or a Switch case (narrower) leaves gaps a backdrop fills. An image
 * without a size counts as filling it, so nothing is drawn behind an image that cannot be measured.
 */
export function fillsCell(width: number, height: number, cellAspect = CELL_ASPECT): boolean {
  if (!(width > 0 && height > 0)) return true;
  return Math.abs(width / height / cellAspect - 1) <= FILL_TOLERANCE;
}
