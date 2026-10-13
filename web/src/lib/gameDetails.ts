// The answer a game sheet shows. The sheet is not remounted per game, so the stored answer may
// still be the previous game's while the next one loads: it is shown only for the game it is for.

/** A game's details answer (or the error asking for it), with the game it belongs to. */
export interface DetailsAnswer<D> {
  gameId: string;
  details: D | null;
  warnings: string[];
  error: string;
}

/** The stored answer when it is `gameId`'s, else null (nothing to show until its own arrives). */
export function answerFor<D>(answer: DetailsAnswer<D> | null, gameId: string): DetailsAnswer<D> | null {
  return answer?.gameId === gameId ? answer : null;
}
