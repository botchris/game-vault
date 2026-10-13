import { useCallback, useEffect, useRef, useState } from 'react';
import { addressFor, gameHash, gameIdFromHash } from './gameRoute';
import { replaceRoute, useHashRoute } from './useHashRoute';

/** The game sheet the library shows: a game and the edition it was opened on ('' for the main one). */
export interface OpenGame {
  id: string;
  system: string;
}

/**
 * The library's open sheet and its address. "#/game/<id>" (links from the bundle checker and the
 * browser extension) opens that game's sheet, now and whenever the address changes; going back
 * closes it. Moving to another game in a sheet opened that way moves the address with it. `exists`
 * says whether a game is in the catalog (null while it loads): an address naming one that is not
 * (deleted, mistyped) opens nothing and is cleared.
 */
export function useGameAddress(exists: ((id: string) => boolean) | null) {
  const { game: addressed } = useHashRoute();
  const [open, setOpen] = useState<OpenGame | null>(null);
  // True while the open sheet came from an address, so going back closes it.
  const byAddress = useRef(false);
  // True when the app added that history entry itself, so closing goes back to the entry before it
  // instead of adding a second "#/library".
  const pushed = useRef(false);

  useEffect(() => {
    if (addressed) {
      byAddress.current = true;
      // The address the sheet itself just moved to keeps the edition the sheet shows.
      setOpen((o) => (o?.id === addressed ? o : { id: addressed, system: '' }));
    } else if (byAddress.current) {
      byAddress.current = false;
      pushed.current = false;
      setOpen(null);
    }
  }, [addressed]);

  // The address follows the game the sheet shows.
  const openId = open?.id ?? null;
  useEffect(() => {
    const next = addressFor(window.location.hash, byAddress.current, openId);
    if (next) replaceRoute(next);
  }, [openId]);

  /** Every way of closing the sheet: clears a game address so a reload does not reopen it. */
  const close = useCallback(() => {
    setOpen(null);
    const wasPushed = pushed.current;
    byAddress.current = false;
    pushed.current = false;
    if (!gameIdFromHash(window.location.hash)) return;
    if (wasPushed) history.back();
    else replaceRoute('#/library');
  }, []);

  useEffect(() => {
    if (!exists || !open || !byAddress.current || exists(open.id)) return;
    close();
  }, [exists, open, close]);

  /** Opens a game from a link inside the app (the checker): the address names it, and opens it
   *  even when the address is already that one. */
  const openFromLink = useCallback((id: string) => {
    const hash = gameHash(id);
    if (window.location.hash !== hash) {
      pushed.current = true;
      window.location.hash = hash;
    }
    byAddress.current = true;
    setOpen({ id, system: '' });
  }, []);

  return { open, setOpen, close, openFromLink };
}
