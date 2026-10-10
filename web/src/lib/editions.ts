// Editions in the web: a game on each system it is played on. Structural types only, so Node tests
// run it as is (no generated code, no TypeScript enums); imports carry the .ts extension.

export interface EditionLike { system: string; coverUrl: string; coverPhotoId: string; main: boolean }

/** The system of the game's main edition; '' for a game without copies. */
export function mainSystem(g: { editions: EditionLike[] }): string {
  return g.editions.find((e) => e.main)?.system ?? '';
}

/** Path of an edition's cover (URL-escaped: systems may hold spaces or slashes); without a system,
 *  the main edition's. */
export function coverPath(gameId: string, system: string): string {
  const base = `/media/covers/${encodeURIComponent(gameId)}`;
  return system ? `${base}/${encodeURIComponent(system)}` : base;
}
