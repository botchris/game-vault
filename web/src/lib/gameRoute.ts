// The address of a game's sheet, "#/game/<id>": the browser extension and the home page link to it,
// and the library opens the sheet it names. Plain functions, Node-tested.

const GAME_HASH = /^#\/game\/([A-Za-z0-9-]{1,64})\/?$/;

/** The game id a "#/game/<id>" address names, or '' for any other address. */
export function gameIdFromHash(hash: string): string {
  return GAME_HASH.exec(hash)?.[1] ?? '';
}

/** The address of a game's sheet. */
export function gameHash(id: string): string {
  return `#/game/${id}`;
}

/**
 * The address to put in place of `hash` while a sheet shows game `openId` (null: no sheet), or null
 * to leave it. Only a sheet opened from its address (`byAddress`) owns it: moving to another game in
 * that sheet (← →, moving its last copy to another game) moves the address too, so a reload or a shared link opens the game shown.
 */
export function addressFor(hash: string, byAddress: boolean, openId: string | null): string | null {
  if (!byAddress || !openId || gameIdFromHash(hash) === openId) return null;
  return gameHash(openId);
}
