type Translate = (key: string, options?: Record<string, string>) => string;

/**
 * The parts of a game sheet's footer that have something to say, in order: where the details came
 * from, which providers found no clear match (no result, or several they would not guess between),
 * and when the sheet was fetched. The caller joins them with " · ", so an empty part never leaves a
 * dangling separator.
 */
export function sheetFooterParts(
  details: { sources: string[]; notFoundBy: string[] },
  updated: string | undefined,
  t: Translate,
): string[] {
  const parts: string[] = [];
  if (details.sources.length > 0) parts.push(t('details.sources', { sources: details.sources.join(' + ') }));
  if (details.notFoundBy.length > 0) parts.push(t('details.notFoundBy', { providers: details.notFoundBy.join(', ') }));
  if (updated) parts.push(t('details.updated', { date: updated }));
  return parts;
}
