// The history rules of the library's game sheet and its "#/game/<id>" address, apart from React so
// they are tested on a real history (happy-dom). useGameAddress.ts runs them for the library.
import { addressFor, gameHash, gameIdFromHash } from './gameRoute.ts';

/** The parts of the browser window the rules use. */
export interface AddressWindow {
  location: { hash: string };
  history: { back(): void };
}

/**
 * The rules, for one window. `replace` changes the address without a history entry (and tells the
 * address's readers). Two facts are kept: whether the open sheet came from its address (going back
 * closes it), and whether the app pushed that history entry itself (closing goes back to the entry
 * before it instead of adding a second "#/library").
 */
export function createGameAddress(win: AddressWindow, replace: (hash: string) => void) {
  let byAddress = false;
  let pushed = false;
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
      pushed = false;
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
      const wasPushed = pushed;
      byAddress = false;
      pushed = false;
      if (!gameIdFromHash(win.location.hash)) return;
      if (wasPushed) win.history.back();
      else replace('#/library');
    },

    /** A link inside the app (the checker) opens game `id`: the address names it, with a new
     *  history entry unless it already does. */
    openFromLink(id: string) {
      const hash = gameHash(id);
      if (win.location.hash !== hash) {
        pushed = true;
        win.location.hash = hash;
      }
      byAddress = true;
    },

    /** Whether the sheet shows game `openId` because its address named it, and the catalog has no
     *  such game (deleted, mistyped): then it is closed and the address cleared. */
    missing(openId: string, exists: (id: string) => boolean): boolean {
      return byAddress && !exists(openId);
    },
  };
}
