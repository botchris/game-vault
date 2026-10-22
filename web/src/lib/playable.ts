// Which games can be played now: the library's "Playable now" and "Only keys to redeem" filters and
// the home page's chip read it from here, so they never disagree. Structural types only, so Node
// tests import it (tests/playable.test.mjs).
import { isPendingKey } from './deadlines.ts';

// The API's numbers (proto/gamevault/v1/game.proto): CopyKind and CopyStatus.
const KIND_KEY = 1;
const KIND_LIBRARY = 2;
const KIND_PHYSICAL = 3;
const STATUS_REDEEMED = 3;
const STATUS_LENT = 7;
const GONE = new Set([4, 5, 8]); // gifted, expired, sold: the user no longer has the copy

interface CopyLike { details?: { kind: number; status: number } }

/** Whether a copy can be played now: a game in a store library, a redeemed key (it is yours on its
 *  store, whether or not that store is connected) or a disc or cartridge at home (not lent). */
export function playableCopy(c: CopyLike): boolean {
  const d = c.details;
  if (!d || GONE.has(d.status)) return false;
  switch (d.kind) {
    case KIND_KEY: return d.status === STATUS_REDEEMED;
    case KIND_LIBRARY: return true;
    case KIND_PHYSICAL: return d.status !== STATUS_LENT;
    default: return false;
  }
}

/** Whether any copy of the game can be played now. */
export const isPlayable = (g: { copies: CopyLike[] }) => g.copies.some(playableCopy);

/** Whether the game can only be played once a key is redeemed: no copy playable now, at least one
 *  key still to reveal or redeem. */
export const onlyKeysToRedeem = (g: { copies: CopyLike[] }) => !isPlayable(g) && g.copies.some(isPendingKey);
