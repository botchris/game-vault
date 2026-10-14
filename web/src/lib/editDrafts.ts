// The game sheet's unsaved Edit-tab forms. The tab unmounts when another tab is shown, so what the
// user typed is kept by the sheet, one draft per game: switching tabs and coming back shows it, and
// moving to another game with ← → keeps each game's own draft while the sheet stays open. Closing
// the sheet drops them all (after asking), and a form that saves or discards has no draft.

/** An Edit tab's unsaved form: what the user typed (`info`) and the game as the form last took it
 *  (`base`), so that the tab, shown again, rebases the typing on the game as it is by then. */
export interface EditDraft<I, G> {
  info: I;
  base: G;
}

/** A sheet's drafts by game id. Never changed in place. */
export type EditDrafts<D> = ReadonlyMap<string, D>;

/** The game's draft, if its form has unsaved changes. */
export function draftFor<D>(drafts: EditDrafts<D>, gameId: string): D | undefined {
  return drafts.get(gameId);
}

/** The drafts with the game's form as it is now: `draft` when it has unsaved changes, undefined when
 *  it has none (saved, discarded, or typed back to the stored values), which drops the game's draft. */
export function keepDraft<D>(drafts: EditDrafts<D>, gameId: string, draft: D | undefined): EditDrafts<D> {
  if (draft === undefined && !drafts.has(gameId)) return drafts;
  const next = new Map(drafts);
  if (draft === undefined) next.delete(gameId);
  else next.set(gameId, draft);
  return next;
}

/** What closing the sheet must do: ask first (`confirm`) when `count` games still in the catalog
 *  have unsaved changes; a draft of a game that is gone (merged into another, deleted) has nothing
 *  left to save. */
export function closeRequest<D>(drafts: EditDrafts<D>, inCatalog: (gameId: string) => boolean): { confirm: boolean; count: number } {
  const count = [...drafts.keys()].filter(inCatalog).length;
  return { confirm: count > 0, count };
}
