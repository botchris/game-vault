import { useCallback, useEffect, useState } from 'react';
import { createGameAddress } from './gameAddress';
import { replaceRoute, useHashRoute } from './useHashRoute';

/** The game sheet the library shows: a game and the edition it was opened on ('' for the main one). */
export interface OpenGame {
  id: string;
  system: string;
}

/**
 * The library's open sheet and its address. "#/game/<id>" (links from the browser extension and the
 * home page) opens that game's sheet, now and whenever the address changes; going back closes it.
 * Moving to another game in a sheet opened that way moves the address with it. `exists` says
 * whether a game is in the catalog (null while it loads): an address naming one that is not
 * (deleted, mistyped) opens nothing and is cleared. The history rules are in gameAddress.ts.
 */
export function useGameAddress(exists: ((id: string) => boolean) | null) {
  const { game: addressed } = useHashRoute();
  const [open, setOpen] = useState<OpenGame | null>(null);
  const [address] = useState(() => createGameAddress(window, replaceRoute));

  useEffect(() => {
    const step = address.follow(addressed);
    // The address the sheet itself just moved to keeps the edition the sheet shows.
    if (step === 'open') setOpen((o) => (o?.id === addressed ? o : { id: addressed, system: '' }));
    else if (step === 'close') setOpen(null);
  }, [address, addressed]);

  // The address follows the game the sheet shows.
  const openId = open?.id ?? null;
  useEffect(() => address.track(openId), [address, openId]);

  /** Every way of closing the sheet: clears a game address so a reload does not reopen it. */
  const close = useCallback(() => {
    setOpen(null);
    address.close();
  }, [address]);

  useEffect(() => {
    if (exists && open && address.missing(open.id, exists)) close();
  }, [address, exists, open, close]);

  return { open, setOpen, close };
}
