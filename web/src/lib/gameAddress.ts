// The history rules of the library's game sheet and its "#/game/<id>" address, apart from React so
// they are tested on a real history (happy-dom). useGameAddress.ts runs them for the library.
import { addressFor, gameIdFromHash } from './gameRoute.ts';

/** The parts of the browser window the rules use. */
export interface AddressWindow {
  location: { hash: string };
}

/**
 * The rules, for one window. `replace` changes the address without a history entry (and tells the
 * address's readers). One fact is kept: whether the open sheet came from its address (going back
 * closes it).
 */
export function createGameAddress(win: AddressWindow, replace: (hash: string) => void) {
  let byAddress = false;
  return {
    /** The address now names game `addressed` ('' for none): open it, close the sheet it had
     *  opened, or leave the sheet as it is. */
    follow(addressed: string): 'open' | 'close' | 'keep' {
      if (addressed) {
        byAddress = true;
        return 'open';
      }
      if (!byAddress) return 'keep';
      byAddress = false;
      return 'close';
    },

    /** The sheet shows game `openId` (null: none); one opened from its address moves the address
     *  to it. */
    track(openId: string | null) {
      const next = addressFor(win.location.hash, byAddress, openId);
      if (next) replace(next);
    },

    /** The sheet closes: a game address is cleared, so a reload does not reopen it. */
    close() {
      byAddress = false;
      if (gameIdFromHash(win.location.hash)) replace('#/library');
    },

    /** Whether the sheet shows game `openId` because its address named it, and the catalog has no
     *  such game (deleted, mistyped): then it is closed and the address cleared. */
    missing(openId: string, exists: (id: string) => boolean): boolean {
      return byAddress && !exists(openId);
    },
  };
}
