/**
 * Returns the address when it is an absolute https URL, undefined otherwise. Addresses that come
 * from providers (an article's page, a license) only become links when they are https, so a
 * `javascript:` or plain-http value can never end up in an href.
 */
export function httpsUrl(value: string | undefined): string | undefined {
  if (!value) return undefined;
  try {
    return new URL(value).protocol === 'https:' ? value : undefined;
  } catch {
    return undefined;
  }
}

/** Whether the link on store `key` is still the one Game Vault found automatically: the server
 *  marks it, and the user has not changed it in the editor (`edited`), so it gets the "automatic"
 *  badge. */
export function isAutoLink(game: { links: Record<string, string>; autoLinks: string[] }, key: string, edited: string | undefined): boolean {
  return game.autoLinks.includes(key) && (edited ?? '').trim() === game.links[key];
}

/**
 * Rebases a form's edits on the record as it is now: the keys the user changed (`edited` differs
 * from what the form `loaded`, a key removed included) keep the user's value, and every other key
 * comes from `current`. A link found automatically while the form was open, or one added from
 * another page, is kept instead of being silently removed by a save. `same` compares two values
 * (by default strictly).
 */
export function rebaseRecord<V>(
  loaded: Record<string, V>,
  edited: Record<string, V>,
  current: Record<string, V>,
  same: (a: V, b: V) => boolean = (a, b) => a === b,
): Record<string, V> {
  const out = { ...current };
  for (const key of new Set([...Object.keys(loaded), ...Object.keys(edited)])) {
    const was = loaded[key], now = edited[key];
    const changed = was === undefined || now === undefined ? was !== now : !same(was, now);
    if (!changed) continue;
    if (now === undefined) delete out[key];
    else out[key] = now;
  }
  return out;
}

/** What the Edit tab edits of a game; `F` is a custom field's value. */
export interface GameInfo<F> {
  title: string;
  links: Record<string, string>;
  notes: string;
  fields: Record<string, F>;
}

/** The form's info rebased on the game as it is now (see rebaseRecord): what the user changed since
 *  `base` stays, the rest is `current`'s. Links differing only in spaces are the same link;
 *  `sameField` compares two field values. */
export function rebaseInfo<F>(base: GameInfo<F>, edited: GameInfo<F>, current: GameInfo<F>, sameField: (a: F, b: F) => boolean): GameInfo<F> {
  return {
    title: edited.title !== base.title ? edited.title : current.title,
    notes: edited.notes !== base.notes ? edited.notes : current.notes,
    links: rebaseRecord(base.links, edited.links, current.links, (a, b) => a.trim() === b.trim()),
    fields: rebaseRecord(base.fields, edited.fields, current.fields, sameField),
  };
}

/** Links without the cleared stores (an empty id unlinks), trimmed: what a save sends. */
export function cleanLinks(links: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(links).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== ''));
}

/** Whether two link sets save the same (see cleanLinks). */
export function sameLinks(a: Record<string, string>, b: Record<string, string>): boolean {
  const ca = cleanLinks(a), cb = cleanLinks(b);
  const keys = Object.keys(ca);
  return keys.length === Object.keys(cb).length && keys.every((k) => ca[k] === cb[k]);
}
