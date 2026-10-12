// The address of a game's sheet, "#/game/<id>": the bundle checker and the browser extension link
// to it, and the library opens the sheet it names. Plain functions, Node-tested.

const GAME_HASH = /^#\/game\/([A-Za-z0-9-]{1,64})\/?$/;

/** The game id a "#/game/<id>" address names, or '' for any other address. */
export function gameIdFromHash(hash: string): string {
  return GAME_HASH.exec(hash)?.[1] ?? '';
}
